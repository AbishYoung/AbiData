package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// File attachments in backups (ADR 0011). A backup of a workspace that has
// files is two siblings next to each other:
//
//	<id>.db.gz          the database, as before
//	<id>.files.tar      one tar entry per distinct file, named by its SHA-256
//	<id>.manifest.json  signed; lists every file (hash, size) plus the tar's own hash
//
// The tar is not compressed: attachments are mostly photos and PDFs that are
// already compressed. The manifest signature covers the file list and the
// tar's hash, so any change to a file inside the archive fails Verify. A
// backup without files has none of these fields and verifies exactly as
// archives made before this feature did.

// ManifestFile is one file in a backup's files archive.
type ManifestFile struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func filesArchivePath(b string) string { return strings.TrimSuffix(b, ".db.gz") + ".files.tar" }

type fileArchive struct {
	list    []ManifestFile
	bytes   int64
	sha     string
	missing int
}

// listFiles reads the distinct (hash, size) pairs a database file's _sys_files
// describes. A database from before file attachments has none.
func listFiles(ctx context.Context, dbPath string) ([]ManifestFile, error) {
	c, err := sqlitex.Open(ctx, dbPath, sqlitex.Options{Kind: sqlitex.Snapshot})
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	var n int
	if err := c.QueryRow(ctx, sqlListFiles.SQL).Scan(&n); err != nil || n == 0 {
		return nil, err
	}
	rows, err := c.Query(ctx, sqlListFiles2.SQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManifestFile
	for rows.Next() {
		var f ManifestFile
		if err := rows.Scan(&f.SHA256, &f.Size); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// archiveFiles writes <dir>/<id>.files.tar with the bucket files the database
// at raw refers to. Files that are missing or damaged on disk are left out (and
// counted): a bad blob must not stop the database from being backed up, and
// the restored row simply shows "file removed". Returns nil when there is
// nothing to archive.
func (s *Service) archiveFiles(ctx context.Context, ws ids.WorkspaceID, dir, id, raw string, at time.Time) (*fileArchive, error) {
	if s.Files == nil {
		return nil, nil
	}
	list, err := listFiles(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("backup: list files: %w", err)
	}
	if len(list) == 0 {
		return nil, nil
	}
	bk, err := s.Files.BucketFor(ws)
	if err != nil {
		return nil, err
	}
	// The janitor may not delete blobs while we copy them.
	release := s.Files.Hold(ws)
	defer release()

	dst := filepath.Join(dir, id+".files.tar")
	partial := dst + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // backup dir from SafeJoin
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*fileArchive, error) {
		_ = out.Close()
		_ = os.Remove(partial)
		return nil, e
	}
	h := sha256.New()
	cw := &countWriter{w: io.MultiWriter(out, h)}
	tw := tar.NewWriter(cw)
	fa := &fileArchive{}
	for _, f := range list {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		path, perr := bk.Path(f.SHA256)
		if perr != nil {
			fa.missing++
			continue
		}
		if cerr := files.CheckBlob(path, f.SHA256, f.Size); cerr != nil {
			s.log().Error("file left out of backup: missing or damaged", "subsystem", "backup", "workspace_id", ws, "sha256", f.SHA256, "err", cerr)
			fa.missing++
			continue
		}
		src, oerr := bk.Open(f.SHA256)
		if oerr != nil {
			fa.missing++
			continue
		}
		hdr := &tar.Header{Name: f.SHA256, Mode: 0o600, Size: f.Size, ModTime: at, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = src.Close()
			return fail(err)
		}
		_, err := io.CopyN(tw, src, f.Size)
		_ = src.Close()
		if err != nil {
			return fail(err)
		}
		fa.list = append(fa.list, f)
	}
	if err := tw.Close(); err != nil {
		return fail(err)
	}
	if err := out.Sync(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(partial)
		return nil, err
	}
	if len(fa.list) == 0 {
		_ = os.Remove(partial)
		return &fileArchive{missing: fa.missing}, nil
	}
	if err := os.Rename(partial, dst); err != nil {
		_ = os.Remove(partial)
		return nil, err
	}
	if err := syncDir(dir); err != nil {
		return nil, err
	}
	fa.bytes, fa.sha = cw.n, hex.EncodeToString(h.Sum(nil))
	return fa, nil
}

// verifyFiles checks the files archive against the (already authenticated) manifest.
func verifyFiles(ctx context.Context, dbPath string, m Manifest) error {
	if m.SHA256Files == "" {
		return nil
	}
	f, err := os.Open(filesArchivePath(dbPath)) //nolint:gosec // path recorded by us
	if err != nil {
		return fault.New(fault.BackupInvalid, "The backup's files archive is missing.")
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, f})
	if err != nil {
		return err
	}
	if n != m.FilesBytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256Files {
		return fault.New(fault.BackupInvalid, "The backup's files archive does not match its manifest; it is damaged or was modified.")
	}
	return nil
}

// extractFiles unpacks the verified files archive of a backup into the bucket
// dest, checking every entry against the manifest: only listed names, the
// listed size, and a SHA-256 that matches the name. Anything else fails the
// whole extraction.
func extractFiles(ctx context.Context, dbPath string, m Manifest, dest files.Bucket) error {
	if m.SHA256Files == "" {
		return nil
	}
	want := make(map[string]int64, len(m.Files))
	for _, f := range m.Files {
		want[f.SHA256] = f.Size
	}
	in, err := os.Open(filesArchivePath(dbPath)) //nolint:gosec // path recorded by us
	if err != nil {
		return fault.New(fault.BackupInvalid, "The backup's files archive is missing.")
	}
	defer func() { _ = in.Close() }()
	tr := tar.NewReader(contextReader{ctx, in})
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fault.New(fault.BackupInvalid, "The backup's files archive is not readable.")
		}
		size, listed := want[hdr.Name]
		if hdr.Typeflag != tar.TypeReg || !listed || seen[hdr.Name] || hdr.Size != size || !files.ValidSHA(hdr.Name) {
			return fault.Newf(fault.BackupInvalid, "The backup's files archive holds an entry the manifest does not list (%q).", hdr.Name)
		}
		seen[hdr.Name] = true
		tmp, err := dest.NewTemp()
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(tr, size+1))
		if err == nil {
			err = tmp.Sync()
		}
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil && (n != size || hex.EncodeToString(h.Sum(nil)) != hdr.Name) {
			err = fault.Newf(fault.BackupInvalid, "File %s in the backup does not match its hash.", hdr.Name)
		}
		if err != nil {
			_ = os.Remove(tmp.Name())
			return err
		}
		if _, err := dest.Place(tmp.Name(), hdr.Name, size); err != nil {
			_ = os.Remove(tmp.Name())
			return err
		}
	}
	if len(seen) != len(want) {
		return fault.New(fault.BackupInvalid, "The backup's files archive is missing files its manifest lists.")
	}
	return nil
}

// stageFiles extracts a backup's files into a staging bucket under the restore
// directory (same filesystem as the live buckets, so merging is a rename).
func (s *Service) stageFiles(ctx context.Context, b string, m Manifest, stage string) error {
	if m.SHA256Files == "" {
		return nil
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	if err := extractFiles(ctx, b, m, files.Bucket{Dir: stage}); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	return nil
}

// mergeFiles moves staged blobs that the live bucket lacks into it. Blobs the
// live bucket already has are identical (same hash) and stay. This runs before
// the database is switched, so a restored row never points at a file that is
// not there; if the restore then fails, the extra blobs are harmless orphans
// that the janitor removes.
func (s *Service) mergeFiles(ws ids.WorkspaceID, stage string) error {
	if s.Files == nil {
		return nil
	}
	if _, err := os.Stat(stage); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	live, err := s.Files.BucketFor(ws)
	if err != nil {
		return err
	}
	st := files.Bucket{Dir: stage}
	return st.Walk(func(b files.BlobInfo) error {
		if live.Exists(b.SHA) {
			return nil
		}
		from, err := st.Path(b.SHA)
		if err != nil {
			return err
		}
		to, err := live.Path(b.SHA)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		return syncDir(filepath.Dir(to))
	})
}

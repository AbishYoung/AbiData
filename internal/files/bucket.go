package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidSHA reports whether s is a lower-case hex SHA-256.
func ValidSHA(s string) bool { return shaRe.MatchString(s) }

// Bucket is a workspace's content-addressed blob directory:
//
//	<dir>/aa/bb/<sha256>      the blobs (aa, bb are the first two byte pairs)
//	<dir>/.tmp/               uploads in flight
//	<dir>/.thumb/<sha256>.jpg cached thumbnails
type Bucket struct{ Dir string }

// Path returns the blob path for sha.
func (b Bucket) Path(sha string) (string, error) {
	if !ValidSHA(sha) {
		return "", fmt.Errorf("files: invalid sha256 %q", sha)
	}
	return filepath.Join(b.Dir, sha[0:2], sha[2:4], sha), nil
}

func (b Bucket) tmpDir() string   { return filepath.Join(b.Dir, ".tmp") }
func (b Bucket) thumbDir() string { return filepath.Join(b.Dir, ".thumb") }

// ThumbPath returns the cached thumbnail path for sha.
func (b Bucket) ThumbPath(sha string) (string, error) {
	if !ValidSHA(sha) {
		return "", fmt.Errorf("files: invalid sha256 %q", sha)
	}
	return filepath.Join(b.thumbDir(), sha+".jpg"), nil
}

// NewTemp creates a private temporary file for an upload in the bucket.
func (b Bucket) NewTemp() (*os.File, error) {
	if err := os.MkdirAll(b.tmpDir(), 0o750); err != nil {
		return nil, err
	}
	return os.CreateTemp(b.tmpDir(), "up-*")
}

// Place moves the fsynced temporary file tmp to its content address. When the
// blob already exists with the right size the temporary file is discarded
// (deduplication) and created is false. The containing directories are synced
// so the rename survives a crash.
func (b Bucket) Place(tmp, sha string, size int64) (created bool, err error) {
	final, err := b.Path(sha)
	if err != nil {
		return false, err
	}
	if st, err := os.Stat(final); err == nil && st.Mode().IsRegular() && st.Size() == size {
		_ = os.Remove(tmp)
		return false, nil
	}
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return false, err
	}
	if err := syncDir(dir); err != nil {
		return true, err
	}
	// Make the new aa/ and aa/bb entries durable as well.
	_ = syncDir(filepath.Dir(dir))
	_ = syncDir(b.Dir)
	return true, nil
}

// Open opens the blob for sha.
func (b Bucket) Open(sha string) (*os.File, error) {
	p, err := b.Path(sha)
	if err != nil {
		return nil, err
	}
	return os.Open(p) //nolint:gosec // Path validates sha as 64 hex characters
}

// Remove deletes the blob and its cached thumbnail, then prunes the two
// directory levels if they became empty. A missing blob is not an error.
func (b Bucket) Remove(sha string) error {
	p, err := b.Path(sha)
	if err != nil {
		return err
	}
	if tp, err := b.ThumbPath(sha); err == nil {
		_ = os.Remove(tp)
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// os.Remove on a non-empty directory fails, which is exactly "keep it".
	bb := filepath.Dir(p)
	if os.Remove(bb) == nil {
		_ = os.Remove(filepath.Dir(bb))
	}
	return nil
}

// Exists reports whether the blob for sha is present.
func (b Bucket) Exists(sha string) bool {
	p, err := b.Path(sha)
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// ErrCorrupt reports a blob whose bytes no longer match its address.
var ErrCorrupt = errors.New("files: blob does not match its hash")

// CheckBlob re-hashes the file at path and compares it with sha (and size when
// size >= 0). It is the integrity check used by backups and the verifier.
func CheckBlob(path, sha string, size int64) error {
	f, err := os.Open(path) //nolint:gosec // callers pass bucket or archive paths they built
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if (size >= 0 && n != size) || hex.EncodeToString(h.Sum(nil)) != sha {
		return fmt.Errorf("%w: %s", ErrCorrupt, sha)
	}
	return nil
}

// BlobInfo describes one blob found by Walk.
type BlobInfo struct {
	SHA     string
	Size    int64
	ModTime time.Time
}

// Walk calls fn for every blob in the bucket (dot directories are skipped).
func (b Bucket) Walk(fn func(BlobInfo) error) error {
	top, err := os.ReadDir(b.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, aa := range top {
		if !aa.IsDir() || len(aa.Name()) != 2 {
			continue
		}
		l2, err := os.ReadDir(filepath.Join(b.Dir, aa.Name()))
		if err != nil {
			return err
		}
		for _, bb := range l2 {
			if !bb.IsDir() || len(bb.Name()) != 2 {
				continue
			}
			l3, err := os.ReadDir(filepath.Join(b.Dir, aa.Name(), bb.Name()))
			if err != nil {
				return err
			}
			for _, e := range l3 {
				if !ValidSHA(e.Name()) || e.Name()[0:2] != aa.Name() || e.Name()[2:4] != bb.Name() {
					continue
				}
				st, err := e.Info()
				if err != nil || !st.Mode().IsRegular() {
					continue
				}
				if err := fn(BlobInfo{SHA: e.Name(), Size: st.Size(), ModTime: st.ModTime()}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// SweepTemp removes temporary upload files older than maxAge.
func (b Bucket) SweepTemp(now time.Time, maxAge time.Duration) {
	entries, err := os.ReadDir(b.tmpDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if st, err := e.Info(); err == nil && now.Sub(st.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(b.tmpDir(), e.Name()))
		}
	}
}

func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // bucket directories we created
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

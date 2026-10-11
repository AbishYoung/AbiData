package backup

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/records"
)

// filesEnv is a workspace with an attachments table, three uploaded files (two
// attached, one pending) and a files service wired into the backup service.
type filesEnv struct {
	*env
	fs     *files.Service
	cache  *catalog.Cache
	rec    *records.Service
	table  string
	col    string
	a, b   files.Meta
	pend   files.Meta
	rowID  int64
	rowVer int64
}

func newFilesEnv(t *testing.T) *filesEnv {
	t.Helper()
	e := newEnv(t)
	fe := &filesEnv{env: e, fs: &files.Service{DataDir: e.dir}, cache: catalog.NewCache()}
	e.svc.Files = fe.fs
	fe.rec = &records.Service{Schemas: fe.cache}
	l := e.lease(e.ws)
	defer l.Release()
	if _, err := (&catalog.Engine{}).Run(e.ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sightings",
		Columns: []catalog.ColumnSpec{{DisplayName: "Photos", Type: catalog.Attachments}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, err := fe.cache.Get(e.ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	tbl, _ := sch.TableByPhysical("Sightings")
	col, _ := tbl.ColumnByPhysical("Photos")
	fe.table, fe.col = tbl.ID, col.ID
	up := func(name, content string) files.Meta {
		m, err := fe.fs.Upload(e.ctx, l, e.owner, name, strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	fe.a, fe.b, fe.pend = up("a.txt", "alpha contents"), up("b.txt", "bravo contents"), up("p.txt", "pending contents")
	row, err := fe.rec.Insert(e.ctx, l, fe.table, map[string]any{fe.col: []any{fe.a.ID, fe.b.ID}}, records.Actor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fe.rowID, fe.rowVer = row.ID, row.Version
	return fe
}

func (fe *filesEnv) bucket(ws ids.WorkspaceID) files.Bucket {
	bk, err := fe.fs.BucketFor(ws)
	if err != nil {
		fe.t.Fatal(err)
	}
	return bk
}

func (fe *filesEnv) read(ws ids.WorkspaceID, m files.Meta) string {
	fe.t.Helper()
	p, _ := fe.bucket(ws).Path(m.SHA256)
	b, err := os.ReadFile(p)
	if err != nil {
		fe.t.Fatalf("%s: %v", m.Name, err)
	}
	return string(b)
}

func TestBackupIncludesFilesAndManifestListsThem(t *testing.T) {
	fe := newFilesEnv(t)
	l := fe.lease(fe.ws)
	b, err := fe.svc.Snapshot(fe.ctx, l, admin.BackupManual, fe.owner)
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	m, err := fe.svc.Verify(fe.ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	// All three files are in the database, so all three are archived.
	if len(m.Files) != 3 || m.SHA256Files == "" || m.FilesBytes == 0 || m.FilesMissing != 0 {
		t.Fatalf("manifest %+v", m)
	}
	seen := map[string]int64{}
	for _, f := range m.Files {
		seen[f.SHA256] = f.Size
	}
	if seen[fe.a.SHA256] != fe.a.Size || seen[fe.pend.SHA256] != fe.pend.Size {
		t.Fatalf("manifest files %v", m.Files)
	}
	if _, err := os.Stat(filesArchivePath(b.Path)); err != nil {
		t.Fatal(err)
	}
	// Deleting the backup removes the archive too.
	if err := fe.svc.Delete(fe.ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filesArchivePath(b.Path)); !os.IsNotExist(err) {
		t.Fatal("files archive left behind")
	}
}

func TestTamperedFilesArchiveFailsVerifyAndRestore(t *testing.T) {
	fe := newFilesEnv(t)
	l := fe.lease(fe.ws)
	b, err := fe.svc.Snapshot(fe.ctx, l, admin.BackupManual, fe.owner)
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	tarPath := filesArchivePath(b.Path)
	orig, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	// Flip one byte inside a file's content.
	tampered := append([]byte(nil), orig...)
	i := bytes.Index(tampered, []byte("alpha contents"))
	if i < 0 {
		t.Fatal("content not found in archive")
	}
	tampered[i] ^= 0x01
	if err := os.WriteFile(tarPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fe.svc.Verify(fe.ctx, b); fault.Classify(err).Code != fault.BackupInvalid {
		t.Fatalf("verify accepted a tampered file: %v", err)
	}
	if err := fe.svc.Restore(fe.ctx, fe.ws, b.ID, fe.owner); fault.Classify(err).Code != fault.BackupInvalid {
		t.Fatalf("restore: %v", err)
	}
	if _, err := fe.svc.RestoreAsNew(fe.ctx, b.ID, func(ids.WorkspaceID) error { return nil }); fault.Classify(err).Code != fault.BackupInvalid {
		t.Fatalf("restore as new: %v", err)
	}
	// A missing archive fails too.
	if err := os.Remove(tarPath); err != nil {
		t.Fatal(err)
	}
	if _, err := fe.svc.Verify(fe.ctx, b); fault.Classify(err).Code != fault.BackupInvalid {
		t.Fatalf("verify without the archive: %v", err)
	}
	// Rewriting the tar and the manifest's hash without the key is caught by the signature.
	if err := os.WriteFile(tarPath, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	mp := strings.TrimSuffix(b.Path, ".db.gz") + ".manifest.json"
	raw, _ := os.ReadFile(mp)
	if err := os.WriteFile(mp, bytes.Replace(raw, []byte(`"size": 14`), []byte(`"size": 15`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fe.svc.Verify(fe.ctx, b); fault.Classify(err).Code != fault.BackupInvalid {
		t.Fatalf("verify accepted an edited file list: %v", err)
	}
}

func TestRestoreBringsFilesBackBeforeTheDatabase(t *testing.T) {
	fe := newFilesEnv(t)
	l := fe.lease(fe.ws)
	b, err := fe.svc.Snapshot(fe.ctx, l, admin.BackupManual, fe.owner)
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	// Lose the file bytes and the row (a disaster), and add a newer file that the backup never had.
	l = fe.lease(fe.ws)
	if err := fe.rec.Delete(fe.ctx, l, fe.table, fe.rowID, fe.rowVer, records.Actor{}, nil); err != nil {
		t.Fatal(err)
	}
	newer, err := fe.fs.Upload(fe.ctx, l, fe.owner, "newer.txt", strings.NewReader("written after the backup"))
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []files.Meta{fe.a, fe.b} {
		p, _ := fe.bucket(fe.ws).Path(m.SHA256)
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := fe.svc.Restore(fe.ctx, fe.ws, b.ID, fe.owner); err != nil {
		t.Fatal(err)
	}
	fe.svc.Wait()
	if got := fe.read(fe.ws, fe.a); got != "alpha contents" {
		t.Fatalf("a = %q", got)
	}
	if got := fe.read(fe.ws, fe.b); got != "bravo contents" {
		t.Fatalf("b = %q", got)
	}
	// The restored database knows the files and the row again; the newer file's row is gone.
	l = fe.lease(fe.ws)
	defer l.Release()
	if _, err := fe.fs.Get(fe.ctx, l, fe.a.ID); err != nil {
		t.Fatalf("restored file row: %v", err)
	}
	if _, err := fe.fs.Get(fe.ctx, l, newer.ID); fault.Classify(err).Code != fault.NotFound {
		t.Fatalf("file newer than the backup survived the restore: %v", err)
	}
	row, err := fe.rec.Get(fe.ctx, l, fe.table, fe.rowID)
	if err != nil || len(records.AttachmentIDs(`["`+strings.Join(toStrings(row.Fields[fe.col]), `","`)+`"]`)) != 2 {
		t.Fatalf("restored row %+v %v", row, err)
	}
	// The pre-restore backup (made in the background) also carries the files.
	if entries, _ := os.ReadDir(fe.svc.restoreDir()); len(entries) != 0 {
		t.Fatalf("temporary restore files left: %v", entries)
	}
}

func toStrings(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			out = append(out, x.(string))
		}
	}
	return out
}

func TestRestoreAsNewCopiesFiles(t *testing.T) {
	fe := newFilesEnv(t)
	l := fe.lease(fe.ws)
	b, err := fe.svc.Snapshot(fe.ctx, l, admin.BackupManual, fe.owner)
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	id, err := fe.svc.RestoreAsNew(fe.ctx, b.ID, func(id ids.WorkspaceID) error {
		return fe.ad.CreateWorkspace(fe.ctx, admin.Workspace{ID: id, Name: "Copy", CreatedAt: time.Now().UTC(), CreatedBy: fe.owner}, fe.owner)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []files.Meta{fe.a, fe.b, fe.pend} {
		if got := fe.read(id, m); !strings.HasSuffix(got, "contents") {
			t.Fatalf("copy of %s: %q", m.Name, got)
		}
	}
	// The copy is independent: removing the original bucket leaves it intact.
	if err := fe.fs.RemoveBucket(fe.ws); err != nil {
		t.Fatal(err)
	}
	if got := fe.read(id, fe.a); got != "alpha contents" {
		t.Fatalf("copy shares storage with the original: %q", got)
	}
	l = fe.lease(id)
	defer l.Release()
	f, _, err := fe.fs.Open(fe.ctx, l, fe.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if b, _ := io.ReadAll(f); string(b) != "alpha contents" {
		t.Fatalf("served %q", b)
	}
}

func TestBackupWithoutFilesIsUnchangedAndOldArchivesStillRestore(t *testing.T) {
	e := newEnv(t)
	e.svc.Files = &files.Service{DataDir: e.dir}
	e.seed(e.ws)
	l := e.lease(e.ws)
	b, err := e.svc.Snapshot(e.ctx, l, admin.BackupManual, e.owner)
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.svc.Verify(e.ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Files != nil || m.SHA256Files != "" || m.FilesBytes != 0 {
		t.Fatalf("files section for a workspace without files: %+v", m)
	}
	// The manifest on disk has none of the new keys: an archive made before this
	// feature has exactly that shape and keeps verifying and restoring.
	raw, _ := os.ReadFile(strings.TrimSuffix(b.Path, ".db.gz") + ".manifest.json")
	for _, key := range []string{"files", "sha256_files", "files_bytes", "files_missing"} {
		if bytes.Contains(raw, []byte(`"`+key+`"`)) {
			t.Fatalf("manifest has %s: %s", key, raw)
		}
	}
	if _, err := os.Stat(filesArchivePath(b.Path)); !os.IsNotExist(err) {
		t.Fatal("empty files archive written")
	}
	before := e.fingerprint(e.ws)
	e.exec(e.ws, `INSERT INTO ledger (amount) VALUES (1000)`)
	if err := e.svc.Restore(e.ctx, e.ws, b.ID, e.owner); err != nil {
		t.Fatal(err)
	}
	e.svc.Wait()
	if d := diff(before, e.fingerprint(e.ws)); d != "" {
		t.Fatalf("restore differs:\n%s", d)
	}
}

func TestBackupSkipsMissingBlobsAndRecordsIt(t *testing.T) {
	fe := newFilesEnv(t)
	p, _ := fe.bucket(fe.ws).Path(fe.b.SHA256)
	if err := os.WriteFile(p, []byte("rotted on disk!"), 0o600); err != nil { // wrong content: damaged
		t.Fatal(err)
	}
	l := fe.lease(fe.ws)
	b, err := fe.svc.Snapshot(fe.ctx, l, admin.BackupManual, fe.owner)
	l.Release()
	if err != nil {
		t.Fatalf("a damaged blob must not stop the database backup: %v", err)
	}
	m, err := fe.svc.Verify(fe.ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if m.FilesMissing != 1 || len(m.Files) != 2 {
		t.Fatalf("manifest %+v", m)
	}
	// And the archive holds only intact files.
	f, _ := os.Open(filesArchivePath(b.Path))
	defer func() { _ = f.Close() }()
	tr := tar.NewReader(f)
	n := 0
	for {
		if _, err := tr.Next(); err != nil {
			break
		}
		n++
	}
	if n != 2 {
		t.Fatalf("%d entries", n)
	}
}

func TestExtractRejectsUnlistedEntries(t *testing.T) {
	fe := newFilesEnv(t)
	l := fe.lease(fe.ws)
	b, err := fe.svc.Snapshot(fe.ctx, l, admin.BackupManual, fe.owner)
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	m, _ := fe.svc.Verify(fe.ctx, b)
	// Build a tar with an extra entry (as if the signed tar had been swapped, which
	// Verify prevents; extractFiles must still refuse it on its own).
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: strings.Repeat("ab", 32), Size: 1, Mode: 0o600, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	if err := os.WriteFile(filesArchivePath(b.Path), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := files.Bucket{Dir: filepath.Join(t.TempDir(), "bucket")}
	if err := extractFiles(fe.ctx, b.Path, m, dest); fault.Classify(err).Code != fault.BackupInvalid {
		t.Fatalf("extract: %v", err)
	}
}

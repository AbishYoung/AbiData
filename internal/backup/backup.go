// Package backup takes online snapshots of workspaces and restores them with a
// crash-safe journal (brief <backup_restore>).
//
// Snapshot: VACUUM INTO a staging file, verify it, gzip it while hashing raw and
// compressed bytes, write an HMAC-signed manifest, record it, apply retention.
//
// Restore: verify, decompress and check a copy, then under the workspace's
// Exclusive drain move the live files aside and swap the copy in, recording each
// step in system/restore-journal/<ws>.json so Recover can finish or undo a
// restore interrupted at any point: the workspace is always fully old or fully new.
package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// ManifestFormat is the manifest schema version.
const ManifestFormat = 1

// integrityLimit: snapshots smaller than this get integrity_check, larger ones quick_check.
const integrityLimit = 1 << 30

// TableCount is a table's row count in a snapshot.
type TableCount struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// Manifest describes a backup file. Field order is the canonical JSON order the
// HMAC is computed over (with HMAC empty).
type Manifest struct {
	Format         int          `json:"format"`
	BackupID       string       `json:"backup_id"`
	WorkspaceID    string       `json:"workspace_id"`
	Kind           string       `json:"kind"`
	CreatedAt      time.Time    `json:"created_at"`
	CreatedBy      string       `json:"created_by"`
	AppVersion     string       `json:"app_version"`
	CatalogVersion int64        `json:"catalog_version"`
	SQLiteVersion  string       `json:"sqlite_version"`
	RawBytes       int64        `json:"raw_bytes"`
	GzBytes        int64        `json:"gz_bytes"`
	SHA256Raw      string       `json:"sha256_raw"`
	SHA256Gz       string       `json:"sha256_gz"`
	Tables         []TableCount `json:"tables"`
	// File attachments (ADR 0011); all omitted for a workspace without files,
	// which keeps older manifests and their signatures valid unchanged.
	Files        []ManifestFile `json:"files,omitempty"`
	FilesBytes   int64          `json:"files_bytes,omitempty"`
	SHA256Files  string         `json:"sha256_files,omitempty"`
	FilesMissing int            `json:"files_missing,omitempty"`
	HMAC         string         `json:"hmac,omitempty"`
}

// Retention is the number of backups kept per workspace.
type Retention struct {
	Daily         int           // newest scheduled backup of each of the last N days
	Weekly        int           // newest scheduled backup of each of the last N ISO weeks
	RestorePoints int           // newest N restore points
	PreRestore    time.Duration // pre_restore backups kept this long
}

// DefaultRetention is the brief's default.
var DefaultRetention = Retention{Daily: 7, Weekly: 4, RestorePoints: 5, PreRestore: 7 * 24 * time.Hour}

// Service takes and restores backups.
type Service struct {
	DataDir    string // <data>
	BackupDir  string // <backups>
	Admin      *admin.Store
	Workspaces *workspace.Manager
	Key        []byte // manifest HMAC key
	AppVersion string
	Retention  Retention
	Log        *slog.Logger
	// Files, when set, adds each workspace's file bucket to its backups and
	// restores it with them (nil: databases only).
	Files *files.Service
	// AfterRestore runs once a restored workspace is reopened (rebuild the job
	// index, drop caches). Errors are logged; the restore stands.
	AfterRestore func(ctx context.Context, ws ids.WorkspaceID)
	// Observe, when set, is told how every snapshot ended (metrics, failure list).
	Observe func(ws ids.WorkspaceID, kind string, took time.Duration, gzBytes int64, err error)

	// hook, when set (tests), is called at each step boundary; an error aborts
	// the operation right there without any cleanup, simulating a crash.
	hook func(step string) error
	now  func() time.Time

	mu   sync.Mutex
	busy map[string]bool // in-flight restores and manual backups, by claim key
	bg   sync.WaitGroup  // background pre_restore archiving
}

// Wait blocks until background archiving has finished (shutdown and tests).
func (s *Service) Wait() { s.bg.Wait() }

// Caps on what a workspace owner can pile up (Phase 6 review M3).
const (
	// MaxManualBackups is how many manual backups a workspace may hold;
	// retention never deletes them, so the owner deletes old ones first.
	MaxManualBackups = 20
	// MaxPreRestore is how many pre_restore backups retention keeps (within
	// the age limit), so repeated restores cannot fill the backup volume.
	MaxPreRestore = 3
)

// claim marks key in flight; it reports false when it already was.
func (s *Service) claim(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[key] {
		return false
	}
	if s.busy == nil {
		s.busy = map[string]bool{}
	}
	s.busy[key] = true
	return true
}

func (s *Service) unclaim(key string) {
	s.mu.Lock()
	delete(s.busy, key)
	s.mu.Unlock()
}

// Manual takes an owner-requested backup: one at a time per workspace, and at
// most MaxManualBackups kept.
func (s *Service) Manual(ctx context.Context, l *workspace.Lease, by string) (admin.Backup, error) {
	key := "backup:" + string(l.ID())
	if !s.claim(key) {
		return admin.Backup{}, fault.New(fault.WorkspaceBusy, "A backup of this workspace is already running.")
	}
	defer s.unclaim(key)
	all, err := s.Admin.ListBackups(ctx, l.ID())
	if err != nil {
		return admin.Backup{}, err
	}
	n := 0
	for _, b := range all {
		if b.Kind == admin.BackupManual {
			n++
		}
	}
	if n >= MaxManualBackups {
		return admin.Backup{}, fault.Newf(fault.QuotaExceeded, "This workspace already has %d manual backups. Delete an old one first.", MaxManualBackups)
	}
	return s.Snapshot(ctx, l, admin.BackupManual, by)
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

func (s *Service) step(name string) error {
	if s.hook != nil {
		return s.hook(name)
	}
	return nil
}

func (s *Service) retention() Retention {
	if s.Retention == (Retention{}) {
		return DefaultRetention
	}
	return s.Retention
}

// Paths ------------------------------------------------------------------------

func (s *Service) stagingDir(ws ids.WorkspaceID) (string, error) {
	return ids.SafeJoin(s.DataDir, "staging", string(ws))
}

func (s *Service) backupDir(ws ids.WorkspaceID) (string, error) {
	return ids.SafeJoin(s.BackupDir, string(ws))
}

func (s *Service) journalPath(ws ids.WorkspaceID) (string, error) {
	return ids.SafeJoin(s.DataDir, "system", "restore-journal", string(ws)+".json")
}

func (s *Service) restoreDir() string { return filepath.Join(s.DataDir, "workspaces", ".restore") }
func (s *Service) asideRoot() string  { return filepath.Join(s.DataDir, "workspaces", ".pre-restore") }

// Snapshot -----------------------------------------------------------------------

// Snapshot backs up the workspace online (writers continue) and records it.
func (s *Service) Snapshot(ctx context.Context, l *workspace.Lease, kind, by string) (admin.Backup, error) {
	start := time.Now()
	b, err := s.snapshot(ctx, l, kind, by)
	if s.Observe != nil {
		s.Observe(l.ID(), kind, time.Since(start), b.GzBytes, err)
	}
	return b, err
}

func (s *Service) snapshot(ctx context.Context, l *workspace.Lease, kind, by string) (admin.Backup, error) {
	switch kind {
	case admin.BackupManual, admin.BackupScheduled, admin.BackupRestorePoint:
	default:
		return admin.Backup{}, fmt.Errorf("backup: kind %q is not a snapshot kind", kind)
	}
	dir, err := s.stagingDir(l.ID())
	if err != nil {
		return admin.Backup{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return admin.Backup{}, err
	}
	id := ids.New()
	stage := filepath.Join(dir, "bk-"+id+".db")
	defer func() { _ = os.Remove(stage) }()
	if err := l.Maintenance(ctx, func(c *sqlitex.Conn) error {
		_, err := c.Exec(ctx, `VACUUM INTO ?`, stage)
		return err
	}); err != nil {
		return admin.Backup{}, fmt.Errorf("backup: snapshot: %w", err)
	}
	return s.archive(ctx, l.ID(), id, kind, by, stage)
}

// inspection is what verifying a raw database file reports.
type inspection struct {
	userVersion    int64
	catalogVersion int64
	sqliteVersion  string
	tables         []TableCount
}

// inspect opens path read-only and checks integrity and identity.
func inspect(ctx context.Context, path string) (inspection, error) {
	var in inspection
	fi, err := os.Stat(path)
	if err != nil {
		return in, err
	}
	c, err := sqlitex.Open(ctx, path, sqlitex.Options{Kind: sqlitex.Snapshot})
	if err != nil {
		return in, err
	}
	defer func() { _ = c.Close() }()
	check := "integrity_check"
	if fi.Size() >= integrityLimit {
		check = "quick_check"
	}
	var res string
	if err := c.QueryRow(ctx, `PRAGMA `+check).Scan(&res); err != nil {
		return in, err
	}
	if res != "ok" {
		return in, fault.Newf(fault.BackupInvalid, "The database failed %s: %s", check, res)
	}
	var appID int64
	if err := c.QueryRow(ctx, `PRAGMA application_id`).Scan(&appID); err != nil {
		return in, err
	}
	if appID != catalog.ApplicationID {
		return in, fault.New(fault.BackupInvalid, "The file is not an AbiData workspace.")
	}
	if err := c.QueryRow(ctx, `PRAGMA user_version`).Scan(&in.userVersion); err != nil {
		return in, err
	}
	if err := c.QueryRow(ctx, `SELECT sqlite_version()`).Scan(&in.sqliteVersion); err != nil {
		return in, err
	}
	var v string
	if err := c.QueryRow(ctx, `SELECT value FROM _sys_meta WHERE key = 'schema_version'`).Scan(&v); err == nil {
		_, _ = fmt.Sscan(v, &in.catalogVersion)
	}
	rows, err := c.Query(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return in, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return in, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return in, err
	}
	for _, n := range names {
		var cnt int64
		if err := c.QueryRow(ctx, `SELECT count(*) FROM `+catalog.Quote(n)).Scan(&cnt); err != nil {
			return in, err
		}
		in.tables = append(in.tables, TableCount{Name: n, Rows: cnt})
	}
	return in, nil
}

// archive verifies raw, compresses it into the backup directory, writes the
// manifest, records the backup and applies retention.
func (s *Service) archive(ctx context.Context, ws ids.WorkspaceID, id, kind, by, raw string) (admin.Backup, error) {
	info, err := inspect(ctx, raw)
	if err != nil {
		return admin.Backup{}, fmt.Errorf("backup: verify snapshot: %w", err)
	}
	dir, err := s.backupDir(ws)
	if err != nil {
		return admin.Backup{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return admin.Backup{}, err
	}
	gzPath := filepath.Join(dir, id+".db.gz")
	rawBytes, gzBytes, shaRaw, shaGz, err := compress(raw, gzPath)
	if err != nil {
		return admin.Backup{}, err
	}
	m := Manifest{Format: ManifestFormat, BackupID: id, WorkspaceID: string(ws), Kind: kind, CreatedAt: s.clock(), CreatedBy: by,
		AppVersion: s.AppVersion, CatalogVersion: info.catalogVersion, SQLiteVersion: info.sqliteVersion,
		RawBytes: rawBytes, GzBytes: gzBytes, SHA256Raw: shaRaw, SHA256Gz: shaGz, Tables: info.tables}
	if m.Tables == nil {
		m.Tables = []TableCount{}
	}
	cleanup := func() {
		_ = os.Remove(gzPath)
		_ = os.Remove(filesArchivePath(gzPath))
		_ = os.Remove(filepath.Join(dir, id+".manifest.json"))
	}
	fa, err := s.archiveFiles(ctx, ws, dir, id, raw, m.CreatedAt)
	if err != nil {
		cleanup()
		return admin.Backup{}, fmt.Errorf("backup: files: %w", err)
	}
	if fa != nil {
		m.Files, m.FilesBytes, m.SHA256Files, m.FilesMissing = fa.list, fa.bytes, fa.sha, fa.missing
	}
	signed, err := s.sign(m)
	if err != nil {
		cleanup()
		return admin.Backup{}, err
	}
	if err := writeFileSync(filepath.Join(dir, id+".manifest.json"), signed); err != nil {
		cleanup()
		return admin.Backup{}, err
	}
	b := admin.Backup{ID: id, WorkspaceID: ws, Kind: kind, Path: gzPath, RawBytes: rawBytes, GzBytes: gzBytes, SHA256Gz: shaGz,
		CreatedAt: m.CreatedAt, CreatedBy: by, ManifestJSON: string(signed)}
	if err := s.Admin.InsertBackup(ctx, b); err != nil {
		cleanup()
		return admin.Backup{}, err
	}
	if err := s.ApplyRetention(ctx, ws); err != nil {
		s.log().Warn("backup retention", "subsystem", "backup", "workspace_id", ws, "err", err)
	}
	return b, nil
}

// compress gzips src into dst (via dst.partial, fsync, rename, fsync dir),
// hashing the raw input and the gzip output.
func compress(src, dst string) (rawBytes, gzBytes int64, shaRaw, shaGz string, err error) {
	in, err := os.Open(src) //nolint:gosec // our staging file
	if err != nil {
		return 0, 0, "", "", err
	}
	defer func() { _ = in.Close() }()
	partial := dst + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // backup dir from SafeJoin
	if err != nil {
		return 0, 0, "", "", err
	}
	fail := func(e error) (int64, int64, string, string, error) {
		_ = out.Close()
		_ = os.Remove(partial)
		return 0, 0, "", "", e
	}
	hr, hg := sha256.New(), sha256.New()
	cg := &countWriter{w: io.MultiWriter(out, hg)}
	zw, err := gzip.NewWriterLevel(cg, gzip.DefaultCompression)
	if err != nil {
		return fail(err)
	}
	n, err := io.Copy(zw, io.TeeReader(in, hr))
	if err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := out.Sync(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(partial)
		return 0, 0, "", "", err
	}
	if err := os.Rename(partial, dst); err != nil {
		_ = os.Remove(partial)
		return 0, 0, "", "", err
	}
	if err := syncDir(filepath.Dir(dst)); err != nil {
		return 0, 0, "", "", err
	}
	return n, cg.n, hex.EncodeToString(hr.Sum(nil)), hex.EncodeToString(hg.Sum(nil)), nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func writeFileSync(path string, data []byte) error {
	partial := path + ".partial"
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // paths built from SafeJoin
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(partial)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(partial)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, path); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // our own directories
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// Manifests ------------------------------------------------------------------------

func (s *Service) mac(m Manifest) ([]byte, error) {
	if len(s.Key) < 32 {
		return nil, errors.New("backup: manifest key is not configured")
	}
	m.HMAC = ""
	canon, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	h := hmac.New(sha256.New, s.Key)
	h.Write(canon)
	return h.Sum(nil), nil
}

func (s *Service) sign(m Manifest) ([]byte, error) {
	sum, err := s.mac(m)
	if err != nil {
		return nil, err
	}
	m.HMAC = hex.EncodeToString(sum)
	return json.MarshalIndent(m, "", "  ")
}

// Verify checks a backup's manifest HMAC and its gzip hash and size. It reads
// the manifest file next to the archive (not the copy in the admin DB), so a
// tampered file on disk is detected.
func (s *Service) Verify(ctx context.Context, b admin.Backup) (Manifest, error) {
	invalid := func(msg string) (Manifest, error) {
		return Manifest{}, fault.New(fault.BackupInvalid, msg)
	}
	mpath := strings.TrimSuffix(b.Path, ".db.gz") + ".manifest.json"
	raw, err := os.ReadFile(mpath) //nolint:gosec // path recorded by us
	if err != nil {
		return invalid("The backup's manifest is missing.")
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return invalid("The backup's manifest is unreadable.")
	}
	want, err := s.mac(m)
	if err != nil {
		return Manifest{}, err
	}
	got, err := hex.DecodeString(m.HMAC)
	if err != nil || !hmac.Equal(got, want) {
		return invalid("The backup's manifest signature does not match; it was modified or made with another key.")
	}
	if m.Format != ManifestFormat || m.BackupID != b.ID || m.WorkspaceID != string(b.WorkspaceID) {
		return invalid("The backup's manifest does not describe this backup.")
	}
	f, err := os.Open(b.Path)
	if err != nil {
		return invalid("The backup file is missing.")
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, f})
	if err != nil {
		return Manifest{}, err
	}
	if n != m.GzBytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256Gz {
		return invalid("The backup file does not match its manifest; it is damaged or was modified.")
	}
	if err := verifyFiles(ctx, b.Path, m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// prepare verifies b and decompresses it to a checked file under .restore/,
// migrated to this server's catalog version and stamped with target's id.
func (s *Service) prepare(ctx context.Context, b admin.Backup, target ids.WorkspaceID, name string) (string, Manifest, error) {
	m, err := s.Verify(ctx, b)
	if err != nil {
		return "", Manifest{}, err
	}
	if err := os.MkdirAll(s.restoreDir(), 0o700); err != nil {
		return "", Manifest{}, err
	}
	dst := filepath.Join(s.restoreDir(), name)
	if err := decompress(ctx, b.Path, dst, m); err != nil {
		_ = os.Remove(dst)
		return "", Manifest{}, err
	}
	info, err := inspect(ctx, dst)
	if err != nil {
		_ = os.Remove(dst)
		return "", Manifest{}, err
	}
	latest := int64(catalog.Version())
	if info.userVersion > latest {
		_ = os.Remove(dst)
		return "", Manifest{}, fault.Newf(fault.BackupInvalid, "The backup was made by a newer AbiData (schema %d > %d); upgrade the server first.", info.userVersion, latest)
	}
	// Migrate the copy (never the live file) and stamp its identity.
	c, err := sqlitex.Open(ctx, dst, sqlitex.Options{Kind: sqlitex.Maintenance})
	if err != nil {
		_ = os.Remove(dst)
		return "", Manifest{}, err
	}
	if info.userVersion < latest {
		err = catalog.Migrate(ctx, target, c)
	}
	if err == nil {
		_, err = c.Exec(ctx, `UPDATE _sys_meta SET value = ? WHERE key = 'workspace_id'`, string(target))
	}
	if err == nil {
		// Leave a self-contained rollback-journal file: no -wal may travel with it.
		_, err = c.Exec(ctx, `PRAGMA journal_mode = DELETE`)
	}
	if cerr := c.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", Manifest{}, fmt.Errorf("backup: prepare restored copy: %w", err)
	}
	return dst, m, nil
}

func decompress(ctx context.Context, src, dst string, m Manifest) error {
	in, err := os.Open(src) //nolint:gosec // path recorded by us
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return fault.New(fault.BackupInvalid, "The backup file is not valid gzip.")
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // restore dir
	if err != nil {
		return err
	}
	h := sha256.New()
	// Never write more than the manifest says (bounded decompression).
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(contextReader{ctx, zr}, m.RawBytes+1))
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != m.RawBytes || hex.EncodeToString(h.Sum(nil)) != m.SHA256Raw {
		return fault.New(fault.BackupInvalid, "The decompressed backup does not match its manifest.")
	}
	return nil
}

// Restore --------------------------------------------------------------------------

// Journal states.
const (
	jPrepared   = "prepared"
	jMovedAside = "moved_aside"
	jSwapped    = "swapped"
	jReverted   = "reverted"
)

type journal struct {
	State       string `json:"state"`
	WorkspaceID string `json:"workspace_id"`
	BackupID    string `json:"backup_id"`
	RestorePath string `json:"restore_path"`
	AsideDir    string `json:"aside_dir"`
	// PrevStatus is the workspace status before the restore, put back at the
	// end (an admin suspension must survive a restore). Empty means active.
	PrevStatus admin.WorkspaceStatus `json:"prev_status,omitempty"`
	By         string                `json:"by"`
	At         time.Time             `json:"at"`
}

func (s *Service) writeJournal(j journal) error {
	p, err := s.journalPath(ids.WorkspaceID(j.WorkspaceID))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return writeFileSync(p, b)
}

func (s *Service) deleteJournal(ws ids.WorkspaceID) error {
	p, err := s.journalPath(ws)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return syncDir(filepath.Dir(p))
}

// errCrash is returned by test hooks to simulate a crash at a step boundary.
var errCrash = errors.New("backup: simulated crash")

// Restore replaces workspace ws with backup backupID.
func (s *Service) Restore(ctx context.Context, ws ids.WorkspaceID, backupID, by string) error {
	b, err := s.Admin.GetBackup(ctx, backupID)
	if err != nil {
		return fault.New(fault.NotFound, "Backup not found.")
	}
	if b.WorkspaceID != ws {
		return fault.New(fault.BadRequest, "This backup belongs to another workspace; restore it as a new workspace instead.")
	}
	// One restore per workspace at a time: two would share the journal and
	// the loser's cleanup would undo the winner's (Phase 6 review H1).
	key := "restore:" + string(ws)
	if !s.claim(key) {
		return fault.New(fault.WorkspaceBusy, "A restore of this workspace is already in progress.")
	}
	defer s.unclaim(key)
	if p, _ := s.journalPath(ws); fileExists(p) {
		return fault.New(fault.WorkspaceBusy, "A restore of this workspace is already in progress.")
	}
	l, err := s.Workspaces.Acquire(ctx, ws)
	if err != nil {
		return err
	}
	released := false
	release := func() {
		if !released {
			released = true
			l.Release()
		}
	}
	defer release()

	tmp, man, err := s.prepare(ctx, b, ws, fmt.Sprintf("%s-%s-%d.db", ws, backupID, s.clock().UnixNano()))
	if err != nil {
		return err
	}
	// The backup's files go back into the bucket before the database is
	// switched, so no restored row can point at a file that is missing.
	stage := tmp + ".files"
	defer func() { _ = os.RemoveAll(stage) }()
	if err := s.stageFiles(ctx, b.Path, man, stage); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := s.mergeFiles(ws, stage); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	j := journal{State: jPrepared, WorkspaceID: string(ws), BackupID: backupID, RestorePath: tmp,
		AsideDir: filepath.Join(s.asideRoot(), fmt.Sprintf("%s-%d", ws, s.clock().UnixNano())), By: by, At: s.clock()}
	if err := s.writeJournal(j); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := s.step("prepared"); err != nil {
		return err
	}

	var swapErr error
	exErr := l.Exclusive(ctx, 30*time.Second, func(p workspace.Paths) error {
		// Record the status (an admin may have suspended the workspace while
		// the copy was prepared) before replacing it with restoring.
		cur, err := s.Admin.GetWorkspace(context.WithoutCancel(ctx), ws)
		if err != nil {
			return err
		}
		if cur.Status != admin.StatusActive && cur.Status != admin.StatusSuspended {
			return fault.Newf(fault.BadRequest, "A %s workspace cannot be restored.", cur.Status)
		}
		j.PrevStatus = cur.Status
		if err := s.writeJournal(j); err != nil {
			return err
		}
		st := admin.StatusRestoring
		if err := s.Admin.UpdateWorkspace(context.WithoutCancel(ctx), ws, admin.WorkspacePatch{Status: &st}); err != nil {
			return err
		}
		if err := s.step("restoring"); err != nil {
			return err
		}
		if err := os.MkdirAll(j.AsideDir, 0o700); err != nil {
			return err
		}
		for _, f := range []string{p.DB, p.WAL, p.SHM} {
			if err := moveIfExists(f, filepath.Join(j.AsideDir, filepath.Base(f))); err != nil {
				swapErr = err
				return err
			}
			if err := s.step("move:" + filepath.Ext(f)); err != nil {
				return err
			}
		}
		if err := syncDir(filepath.Dir(p.DB)); err != nil {
			return err
		}
		if err := syncDir(j.AsideDir); err != nil {
			return err
		}
		j.State = jMovedAside
		if err := s.writeJournal(j); err != nil {
			return err
		}
		if err := s.step("moved_aside"); err != nil {
			return err
		}
		if err := os.Rename(tmp, p.DB); err != nil {
			swapErr = err
			return err
		}
		if err := syncDir(filepath.Dir(p.DB)); err != nil {
			return err
		}
		if err := s.step("renamed"); err != nil {
			return err
		}
		j.State = jSwapped
		if err := s.writeJournal(j); err != nil {
			return err
		}
		return s.step("swapped")
	})
	if errors.Is(exErr, errCrash) {
		return exErr
	}
	release()
	if exErr != nil {
		// Undo whatever the journal says happened, from a clean slate.
		if rerr := s.recoverOne(context.WithoutCancel(ctx), ws); rerr != nil {
			return errors.Join(exErr, swapErr, rerr)
		}
		if jj, _ := s.readJournal(ws); jj == nil {
			return fault.Wrap(fault.RestoreFailed, exErr)
		}
		return fault.Wrap(fault.RestoreFailed, exErr)
	}
	return s.finish(context.WithoutCancel(ctx), ws, j)
}

// finish runs steps 7–8 for a swapped restore: check the reopened workspace,
// revert on failure, else mark it active and keep the old files as pre_restore.
func (s *Service) finish(ctx context.Context, ws ids.WorkspaceID, j journal) error {
	if err := s.step("finish"); err != nil {
		return err
	}
	l, err := s.Workspaces.Acquire(ctx, ws)
	if err == nil {
		err = l.Maintenance(ctx, func(c *sqlitex.Conn) error {
			var res string
			if err := c.QueryRow(ctx, `PRAGMA quick_check`).Scan(&res); err != nil {
				return err
			}
			if res != "ok" {
				return fault.Newf(fault.RestoreFailed, "The restored workspace failed quick_check: %s", res)
			}
			return nil
		})
		l.Release()
	}
	if err != nil {
		s.log().Error("restore check failed; reverting", "subsystem", "backup", "workspace_id", ws, "err", err)
		if rerr := s.revertSwapped(ctx, ws, j); rerr != nil {
			return errors.Join(err, rerr)
		}
		return fault.Wrap(fault.RestoreFailed, err)
	}
	if s.AfterRestore != nil {
		s.AfterRestore(ctx, ws)
	}
	if err := s.putBackStatus(ctx, ws, &j); err != nil {
		return err
	}
	if err := s.deleteJournal(ws); err != nil {
		return err
	}
	_ = s.Admin.Audit(ctx, admin.AuditEntry{At: s.clock(), UserID: j.By, Action: "workspace.restore", ObjectKind: "workspace",
		ObjectID: string(ws), Details: map[string]any{"backup_id": j.BackupID}})
	// Keep the replaced files as a pre_restore backup (best effort, in the
	// background; if the process dies first, Recover archives them at startup).
	s.bg.Go(func() { s.keepAside(context.WithoutCancel(ctx), ws, j) })
	return nil
}

// keepAside archives the moved-aside files as a pre_restore backup, then removes them.
func (s *Service) keepAside(ctx context.Context, ws ids.WorkspaceID, j journal) {
	if !s.claim("aside:" + j.AsideDir) {
		return // already being archived (finish's goroutine vs Recover's sweep)
	}
	defer s.unclaim("aside:" + j.AsideDir)
	if !fileExists(j.AsideDir) {
		return
	}
	db := filepath.Join(j.AsideDir, string(ws)+".db")
	if !fileExists(db) {
		_ = os.RemoveAll(j.AsideDir)
		return
	}
	// Opening read-write in the aside directory folds a leftover -wal into the
	// copy; VACUUM INTO then produces a clean, self-contained file.
	stage := filepath.Join(j.AsideDir, "snapshot.db")
	c, err := sqlitex.Open(ctx, db, sqlitex.Options{Kind: sqlitex.Maintenance})
	if err == nil {
		_, err = c.Exec(ctx, `VACUUM INTO ?`, stage)
		_ = c.Close()
	}
	if err == nil {
		_, err = s.archive(ctx, ws, ids.New(), admin.BackupPreRestore, j.By, stage)
	}
	if err != nil {
		s.log().Warn("pre-restore backup failed; leaving files in place", "subsystem", "backup", "workspace_id", ws, "dir", j.AsideDir, "err", err)
		return
	}
	_ = os.RemoveAll(j.AsideDir)
}

func moveIfExists(src, dst string) error {
	if err := os.Rename(src, dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (s *Service) readJournal(ws ids.WorkspaceID) (*journal, error) {
	p, err := s.journalPath(ws)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p) //nolint:gosec // our journal directory
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j journal
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

// Recover completes or undoes interrupted restores (run at startup, before the
// workspaces are opened).
func (s *Service) Recover(ctx context.Context) error {
	dir := filepath.Join(s.DataDir, "system", "restore-journal")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		s.sweep(ctx)
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		ws := ids.WorkspaceID(strings.TrimSuffix(name, ".json"))
		if !ids.Valid(string(ws)) {
			continue
		}
		if err := s.recoverOne(ctx, ws); err != nil {
			errs = append(errs, fmt.Errorf("workspace %s: %w", ws, err))
		}
	}
	s.sweep(ctx)
	return errors.Join(errs...)
}

// sweep handles what interrupted work left behind with no journal (Phase 6
// review L1): restore copies (a crash during prepare) are deleted, and
// moved-aside files whose archiving never finished become pre_restore backups.
// Only Recover calls it, at startup, when nothing is in flight.
func (s *Service) sweep(ctx context.Context) {
	if entries, err := os.ReadDir(s.restoreDir()); err == nil {
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(s.restoreDir(), e.Name()))
		}
	}
	entries, err := os.ReadDir(s.asideRoot())
	if err != nil {
		return
	}
	for _, e := range entries {
		dir := filepath.Join(s.asideRoot(), e.Name())
		ws, _, ok := strings.Cut(e.Name(), "-")
		if !e.IsDir() || !ok || !ids.Valid(ws) {
			continue
		}
		if p, _ := s.journalPath(ids.WorkspaceID(ws)); fileExists(p) {
			continue // a journal recovery could not finish still needs these files
		}
		s.keepAside(ctx, ids.WorkspaceID(ws), journal{WorkspaceID: ws, AsideDir: dir, By: "system"})
	}
}

// recoverOne applies the journal rules: prepared → drop temps; moved_aside →
// move the old files back; swapped → finish; reverted → clean up.
func (s *Service) recoverOne(ctx context.Context, ws ids.WorkspaceID) error {
	j, err := s.readJournal(ws)
	if err != nil || j == nil {
		return err
	}
	p, err := s.Workspaces.PathsFor(ws)
	if err != nil {
		return err
	}
	switch j.State {
	case jPrepared, jReverted:
		_ = os.Remove(j.RestorePath)
		if fileExists(j.AsideDir) && !fileExists(p.DB) {
			// Interrupted while moving files aside: put back what moved.
			if err := s.moveBack(p, j); err != nil {
				return err
			}
		}
		_ = os.RemoveAll(j.AsideDir)
	case jMovedAside:
		if err := s.moveBack(p, j); err != nil {
			return err
		}
		_ = os.Remove(j.RestorePath)
		_ = os.RemoveAll(j.AsideDir)
	case jSwapped:
		return s.finish(ctx, ws, *j)
	default:
		return fmt.Errorf("unknown restore journal state %q", j.State)
	}
	if err := s.putBackStatus(ctx, ws, j); err != nil {
		return err
	}
	return s.deleteJournal(ws)
}

// putBackStatus ends the restoring status with the status from before the
// restore. A status someone else set meanwhile (needs_repair from the reopen)
// is left alone.
func (s *Service) putBackStatus(ctx context.Context, ws ids.WorkspaceID, j *journal) error {
	cur, err := s.Admin.GetWorkspace(ctx, ws)
	if err != nil {
		return err
	}
	if cur.Status != admin.StatusRestoring && cur.Status != admin.StatusActive {
		return nil
	}
	st := j.PrevStatus
	if st == "" || st == admin.StatusRestoring {
		st = admin.StatusActive
	}
	if st == cur.Status {
		return nil
	}
	return s.Admin.UpdateWorkspace(ctx, ws, admin.WorkspacePatch{Status: &st})
}

// moveBack restores the moved-aside files over whatever is in place (which can
// only be the restored copy, possibly with its own -wal/-shm).
func (s *Service) moveBack(p workspace.Paths, j *journal) error {
	for _, f := range []string{p.DB, p.WAL, p.SHM} {
		aside := filepath.Join(j.AsideDir, filepath.Base(f))
		if fileExists(aside) {
			if err := os.Rename(aside, f); err != nil {
				return err
			}
		} else if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			// The old file set had no such file: a -wal/-shm left by the new copy must go.
			return err
		}
	}
	return syncDir(filepath.Dir(p.DB))
}

// revertSwapped undoes a swapped restore whose check failed.
func (s *Service) revertSwapped(ctx context.Context, ws ids.WorkspaceID, j journal) error {
	l, err := s.Workspaces.Acquire(ctx, ws)
	if err != nil {
		// Not openable: move files back directly (no connections are open).
		p, perr := s.Workspaces.PathsFor(ws)
		if perr != nil {
			return perr
		}
		if err := s.moveBack(p, &j); err != nil {
			return err
		}
	} else {
		err = l.Exclusive(ctx, 30*time.Second, func(p workspace.Paths) error { return s.moveBack(p, &j) })
		l.Release()
		if err != nil {
			return err
		}
	}
	j.State = jReverted
	if err := s.writeJournal(j); err != nil {
		return err
	}
	_ = os.RemoveAll(j.AsideDir)
	if err := s.putBackStatus(ctx, ws, &j); err != nil {
		return err
	}
	return s.deleteJournal(ws)
}

// RestoreAsNew creates a new workspace from a backup of any workspace. create
// inserts the admin record (name, owner) for the new id first, with status
// restoring, so a name conflict fails before any file exists; the workspace is
// set active once its file is in place, or its record dropped on failure.
func (s *Service) RestoreAsNew(ctx context.Context, backupID string, create func(id ids.WorkspaceID) error) (ids.WorkspaceID, error) {
	b, err := s.Admin.GetBackup(ctx, backupID)
	if err != nil {
		return "", fault.New(fault.NotFound, "Backup not found.")
	}
	id := ids.NewWorkspaceID()
	if err := create(id); err != nil {
		return "", err
	}
	fail := func(err error) (ids.WorkspaceID, error) {
		if derr := s.Admin.DropWorkspace(context.WithoutCancel(ctx), id); derr != nil {
			return "", errors.Join(err, derr)
		}
		return "", err
	}
	tmp, man, err := s.prepare(ctx, b, id, string(id)+"-"+backupID+".db")
	if err != nil {
		return fail(err)
	}
	// A new workspace gets its own copy of the files.
	var bucket string
	if s.Files != nil && man.SHA256Files != "" {
		bk, err := s.Files.BucketFor(id)
		if err != nil {
			_ = os.Remove(tmp)
			return fail(err)
		}
		bucket = bk.Dir
		if err := extractFiles(ctx, b.Path, man, bk); err != nil {
			_ = os.Remove(tmp)
			_ = os.RemoveAll(bucket)
			return fail(err)
		}
	}
	if err := s.Workspaces.Adopt(ctx, id, tmp); err != nil {
		_ = os.Remove(tmp)
		if bucket != "" {
			_ = os.RemoveAll(bucket)
		}
		return fail(err)
	}
	st := admin.StatusActive
	if err := s.Admin.UpdateWorkspace(ctx, id, admin.WorkspacePatch{Status: &st}); err != nil {
		return "", err
	}
	if s.AfterRestore != nil {
		s.AfterRestore(ctx, id)
	}
	return id, nil
}

// Retention ------------------------------------------------------------------------

// ApplyRetention deletes backups beyond the retention policy. Manual backups
// are never deleted automatically.
func (s *Service) ApplyRetention(ctx context.Context, ws ids.WorkspaceID) error {
	all, err := s.Admin.ListBackups(ctx, ws)
	if err != nil {
		return err
	}
	keep := keepSet(all, s.retention(), s.clock())
	var errs []error
	for _, b := range all {
		if keep[b.ID] {
			continue
		}
		if err := s.Delete(ctx, b); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// keepSet decides which backups retention keeps (all is newest first).
func keepSet(all []admin.Backup, r Retention, now time.Time) map[string]bool {
	keep := map[string]bool{}
	days, weeks := map[string]bool{}, map[string]bool{}
	points, preRestore := 0, 0
	sorted := append([]admin.Backup(nil), all...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt.After(sorted[j].CreatedAt) })
	for _, b := range sorted {
		switch b.Kind {
		case admin.BackupManual:
			keep[b.ID] = true
		case admin.BackupRestorePoint:
			if points < r.RestorePoints {
				keep[b.ID] = true
				points++
			}
		case admin.BackupPreRestore:
			if now.Sub(b.CreatedAt) < r.PreRestore && preRestore < MaxPreRestore {
				keep[b.ID] = true
				preRestore++
			}
		case admin.BackupScheduled:
			day := b.CreatedAt.UTC().Format("2006-01-02")
			y, w := b.CreatedAt.UTC().ISOWeek()
			week := fmt.Sprintf("%d-%02d", y, w)
			if !days[day] && len(days) < r.Daily {
				days[day] = true
				keep[b.ID] = true
			}
			if !weeks[week] && len(weeks) < r.Weekly {
				weeks[week] = true
				keep[b.ID] = true
			}
		}
	}
	return keep
}

// Delete removes a backup's files and record.
func (s *Service) Delete(ctx context.Context, b admin.Backup) error {
	for _, p := range []string{b.Path, filesArchivePath(b.Path), strings.TrimSuffix(b.Path, ".db.gz") + ".manifest.json"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return s.Admin.DeleteBackup(ctx, b.ID)
}

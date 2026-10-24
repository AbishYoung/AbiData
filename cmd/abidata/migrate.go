package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/admin"
	"abidata/internal/backup"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/dbx"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/sqlgen"
	"abidata/internal/workspace"
)

// abidata migrate-backend moves a stopped install to the other backend
// (ADR 0012 §9): the admin store row for row, every workspace through the
// portable archive (§8), and the data directory's keys, files and artifacts.
// The source is only read. The target is a new data directory, and on
// PostgreSQL an unused schema prefix; until the move finishes it holds a
// marker that keeps the server from starting on it, and a failed move
// removes what it wrote. Running the command again after an interruption
// starts the move over.

const migrateUsage = "usage: abidata migrate-backend --to postgres|sqlite --target-data-dir DIR [--target-backup-dir DIR]"

// migrateMarker, in the target's system directory, marks a move in progress.
const migrateMarker = "migrate-backend.json"

// migrateReportFile, in the target's system directory, is the combined report.
const migrateReportFile = "migrate-backend-report.json"

type migrateMarkerFile struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Prefix    string    `json:"prefix,omitempty"` // the target's schema prefix, on PostgreSQL
	Started   time.Time `json:"started"`
	Database  string    `json:"database,omitempty"`
	BackupDir string    `json:"backup_dir,omitempty"`
}

// migrateReport is written to the target when the move finishes.
type migrateReport struct {
	From       string             `json:"from"`
	To         string             `json:"to"`
	AppVersion string             `json:"app_version"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt time.Time          `json:"finished_at"`
	AdminRows  map[string]int64   `json:"admin_rows"`
	Workspaces []migrateWorkspace `json:"workspaces"`
}

type migrateWorkspace struct {
	ID         ids.WorkspaceID       `json:"id"`
	Name       string                `json:"name,omitempty"`
	Status     admin.WorkspaceStatus `json:"status"`
	Skipped    string                `json:"skipped,omitempty"`
	FilesBytes int64                 `json:"files_bytes,omitempty"`
	Import     *catalog.ImportReport `json:"import,omitempty"`
	Notes      []string              `json:"notes,omitempty"`
}

func migrateBackendCmd(args []string) error {
	fset := flag.NewFlagSet("migrate-backend", flag.ContinueOnError)
	to := fset.String("to", "", "the backend to move the install to: postgres or sqlite (required)")
	targetDir := fset.String("target-data-dir", "", "a new, empty data directory for the moved install (required)")
	targetBackups := fset.String("target-backup-dir", "", "a new, empty directory for the moved install's backups (default: <target-data-dir>/backups)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() > 0 || *to == "" || *targetDir == "" {
		return errors.New(migrateUsage)
	}
	if *to != config.DBPostgres && *to != config.DBSQLite {
		return fmt.Errorf("--to must be %s or %s", config.DBPostgres, config.DBSQLite)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	var tgt config.TargetPostgres
	if *to == config.DBPostgres {
		if tgt, err = config.TargetPostgresFromEnv(); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	m := &migration{src: cfg, to: *to, dataDir: *targetDir, backupDir: *targetBackups, pg: tgt, out: os.Stdout}
	return m.run(ctx)
}

// migration is one run of migrate-backend.
type migration struct {
	src       config.Config // the source install
	to        string
	dataDir   string // the target's
	backupDir string // the target's
	pg        config.TargetPostgres
	out       io.Writer

	started  time.Time
	resumed  bool
	lock     *serverLock
	srcAdm   *admin.Store
	dstAdm   *admin.Store
	srcPool  *pgxpool.Pool
	dstPool  *pgxpool.Pool
	dstPC    *pgxpool.Config
	srcWM    *workspace.Manager
	dstWM    *workspace.Manager
	dialect  sqlgen.Dialect // the target's, on PostgreSQL
	wroteDst bool           // something was written to the target
}

func (m *migration) logf(format string, args ...any) { _, _ = fmt.Fprintf(m.out, format+"\n", args...) }

func (m *migration) run(ctx context.Context) (err error) {
	m.started = time.Now().UTC()
	if m.src.DB == m.to {
		return fmt.Errorf("the install in %s is already on %s", m.src.DataDir, m.to)
	}
	if err := m.resolveDirs(); err != nil {
		return err
	}
	if err := checkBackend(ctx, m.src); err != nil {
		return err
	}
	srcLock, err := acquireLock(m.src.DataDir)
	if err != nil {
		return err
	}
	defer srcLock.Release()
	if err := m.checkSource(); err != nil {
		return err
	}
	defer func() {
		if m.dstPool != nil {
			m.dstPool.Close()
		}
		m.lock.Release()
	}()
	defer m.closeAll()
	if err := m.prepareTarget(ctx); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = m.undo(err)
		}
	}()
	m.wroteDst = true
	if err := m.copyKeys(); err != nil {
		return err
	}
	if err := m.openStores(ctx); err != nil {
		return err
	}
	rep, err := m.copyAdmin(ctx)
	if err != nil {
		return err
	}
	if err := m.copyWorkspaces(ctx, rep); err != nil {
		return err
	}
	if err := copyTree(filepath.Join(m.src.DataDir, "artifacts"), filepath.Join(m.dataDir, "artifacts")); err != nil {
		return fmt.Errorf("copying artifacts: %w", err)
	}
	rep.FinishedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(m.dataDir, "system", migrateReportFile), rep); err != nil {
		return err
	}
	m.closeAll()
	if err := os.Remove(filepath.Join(m.dataDir, "system", migrateMarker)); err != nil {
		return err
	}
	m.summary(rep)
	return nil
}

// resolveDirs makes the target directories absolute and keeps them apart
// from the source's.
func (m *migration) resolveDirs() error {
	var err error
	if m.dataDir, err = filepath.Abs(m.dataDir); err != nil {
		return err
	}
	if m.backupDir == "" {
		m.backupDir = filepath.Join(m.dataDir, "backups")
	}
	if m.backupDir, err = filepath.Abs(m.backupDir); err != nil {
		return err
	}
	srcData, err := filepath.Abs(m.src.DataDir)
	if err != nil {
		return err
	}
	srcBackups, err := filepath.Abs(m.src.BackupDir)
	if err != nil {
		return err
	}
	for _, t := range []string{m.dataDir, m.backupDir} {
		for _, s := range []string{srcData, srcBackups} {
			if overlaps(t, s) {
				return fmt.Errorf("the target directory %s overlaps the source install's %s; choose a new directory", t, s)
			}
		}
	}
	m.src.DataDir, m.src.BackupDir = srcData, srcBackups
	return nil
}

func overlaps(a, b string) bool {
	in := func(x, y string) bool { return x == y || strings.HasPrefix(x, y+string(filepath.Separator)) }
	return in(a, b) || in(b, a)
}

// checkSource refuses a source with an interrupted restore: the server
// finishes or undoes it when it starts, and the move must not copy either half.
func (m *migration) checkSource() error {
	j, err := filepath.Glob(filepath.Join(m.src.DataDir, "system", "restore-journal", "*.json"))
	if err != nil {
		return err
	}
	if len(j) > 0 {
		return errors.New("the source install has an interrupted restore; start the server once so it finishes it, stop it, then run migrate-backend again")
	}
	return nil
}

// prepareTarget checks the target is unused (or left by an interrupted
// move, which is removed), then locks it and writes the marker.
func (m *migration) prepareTarget(ctx context.Context) error {
	marker := filepath.Join(m.dataDir, "system", migrateMarker)
	var prev migrateMarkerFile
	if b, err := os.ReadFile(marker); err == nil { //nolint:gosec // the operator's own target directory
		if err := json.Unmarshal(b, &prev); err != nil {
			return fmt.Errorf("%s is not readable: %w", marker, err)
		}
		if prev.To != m.to || (m.to == config.DBPostgres && prev.Prefix != m.pg.Prefix) || (prev.BackupDir != "" && prev.BackupDir != m.backupDir) {
			return fmt.Errorf("%s holds an interrupted move to %s; run the same command (with the same target settings) again, or delete the directory", m.dataDir, prev.To)
		}
		m.resumed = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !m.resumed {
		for _, d := range []string{m.dataDir, m.backupDir} {
			if err := requireEmpty(d); err != nil {
				return err
			}
		}
	}
	if m.to == config.DBPostgres {
		if err := m.connectTarget(ctx); err != nil {
			return err
		}
		n := pgdb.Names{Prefix: m.pg.Prefix}
		schemas, err := pgdb.InstallSchemas(ctx, pgdb.Pool(m.dstPool), n)
		if err != nil {
			return err
		}
		if len(schemas) > 0 && !m.resumed {
			return fmt.Errorf("the target database already holds schemas with the prefix %q (%s); set ABIDATA_TARGET_POSTGRES_SCHEMA_PREFIX to an unused prefix or use another database",
				m.pg.Prefix, strings.Join(schemas, ", "))
		}
		if m.resumed {
			if err := pgdb.DropInstall(ctx, pgdb.Pool(m.dstPool), n); err != nil {
				return err
			}
		}
	}
	if m.resumed {
		m.logf("an earlier move to %s was interrupted; starting it over", m.to)
		if err := m.clearTargetDirs(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(m.dataDir, "system"), 0o700); err != nil {
		return err
	}
	lock, err := acquireLock(m.dataDir)
	if err != nil {
		return err
	}
	m.lock = lock
	mk := migrateMarkerFile{From: m.src.DB, To: m.to, Started: m.started, BackupDir: m.backupDir}
	if m.to == config.DBPostgres {
		mk.Prefix, mk.Database = m.pg.Prefix, m.dstPC.ConnConfig.Database
	}
	return writeJSON(marker, mk)
}

func (m *migration) connectTarget(ctx context.Context) error {
	pc, err := pgxpool.ParseConfig(m.pg.URL)
	if err != nil {
		return errors.New("ABIDATA_TARGET_POSTGRES_URL is not a valid PostgreSQL connection string")
	}
	m.dstPC = pc
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return fmt.Errorf("connecting to the target PostgreSQL: %w", err)
	}
	m.dstPool = pool
	if err := pgdb.CheckServer(ctx, pgdb.Pool(pool)); err != nil {
		return err
	}
	m.dialect, err = pgdb.Dialect(ctx, pgdb.Pool(pool))
	return err
}

// requireEmpty accepts a missing or empty directory.
func requireEmpty(dir string) error {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(ents) > 0 {
		return fmt.Errorf("the target directory %s is not empty; migrate-backend writes only to a new, empty directory", dir)
	}
	return nil
}

// clearTargetDirs empties the target directories, which hold only what a
// move wrote (they were empty or marked when it began).
func (m *migration) clearTargetDirs() error {
	for _, d := range []string{m.backupDir, m.dataDir} {
		ents, err := os.ReadDir(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, e := range ents {
			if d == m.dataDir && e.Name() == "system" {
				continue // the lock and marker go last
			}
			if err := os.RemoveAll(filepath.Join(d, e.Name())); err != nil {
				return err
			}
		}
	}
	sys := filepath.Join(m.dataDir, "system")
	ents, err := os.ReadDir(sys)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.Name() == "server.lock" || e.Name() == migrateMarker {
			continue
		}
		if err := os.RemoveAll(filepath.Join(sys, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// undo removes what a failed move wrote. If that fails too, the marker stays,
// and running the command again cleans up and starts over.
func (m *migration) undo(cause error) error {
	if !m.wroteDst {
		return cause
	}
	m.closeAll()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var errs []error
	if m.to == config.DBPostgres && m.dstPool != nil {
		if err := pgdb.DropInstall(ctx, pgdb.Pool(m.dstPool), pgdb.Names{Prefix: m.pg.Prefix}); err != nil {
			errs = append(errs, err)
		}
	}
	if err := m.clearTargetDirs(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		m.lock.Release()
		m.lock = nil
		_ = os.Remove(filepath.Join(m.dataDir, "system", "server.lock"))
		_ = os.Remove(filepath.Join(m.dataDir, "system", migrateMarker))
		_ = os.Remove(filepath.Join(m.dataDir, "system"))
		m.logf("the move failed; the source is unchanged and what was written to the target has been removed")
		return cause
	}
	return fmt.Errorf("%w (cleaning up the target also failed: %w; run the command again to clean up and start over)", cause, errors.Join(errs...))
}

func (m *migration) closeAll() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if m.srcWM != nil {
		_ = m.srcWM.Shutdown(ctx)
		m.srcWM = nil
	}
	if m.dstWM != nil {
		_ = m.dstWM.Shutdown(ctx)
		m.dstWM = nil
	}
	if m.srcAdm != nil {
		_ = m.srcAdm.Close()
		m.srcAdm = nil
	}
	if m.dstAdm != nil {
		_ = m.dstAdm.Close()
		m.dstAdm = nil
	}
	if m.srcPool != nil {
		m.srcPool.Close()
		m.srcPool = nil
	}
	// The target pool stays open for undo (see run).
}

// copyKeys carries the session and manifest keys: API keys are hashed under
// the session key, and backups are signed with the manifest key.
func (m *migration) copyKeys() error {
	src := filepath.Join(m.src.DataDir, "system", "keys")
	dst := filepath.Join(m.dataDir, "system", "keys")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, k := range []string{"session.key", "manifest.key"} {
		b, err := os.ReadFile(filepath.Join(src, k)) //nolint:gosec // the install's own key directory
		if errors.Is(err, fs.ErrNotExist) {
			continue // supplied through the environment; the target must use the same
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, k), b, 0o600); err != nil { //nolint:gosec // k is one of two fixed names
			return err
		}
	}
	return nil
}

func (m *migration) openStores(ctx context.Context) error {
	var err error
	if m.src.DB == config.DBPostgres {
		pc, perr := pgxpool.ParseConfig(m.src.PostgresURL)
		if perr != nil {
			return errors.New("ABIDATA_POSTGRES_URL is not a valid PostgreSQL connection string")
		}
		if m.src.PostgresMaxConns > 0 {
			pc.MaxConns = int32(min(m.src.PostgresMaxConns, 1<<20)) //nolint:gosec // bounded above
		}
		if m.srcPool, err = pgxpool.NewWithConfig(ctx, pc); err != nil {
			return fmt.Errorf("connecting to PostgreSQL: %w", err)
		}
		dialect, err := pgdb.Dialect(ctx, pgdb.Pool(m.srcPool))
		if err != nil {
			return err
		}
		if m.srcAdm, err = admin.OpenPostgres(ctx, pc, pgdb.Names{Prefix: m.src.PostgresPrefix}); err != nil {
			return err
		}
		m.srcWM, err = workspace.NewManager(workspace.Config{DataDir: m.src.DataDir, MaxOpen: 4, Synchronous: m.src.Synchronous,
			Postgres: &workspace.PostgresConfig{Pool: m.srcPool, Database: pc.ConnConfig.Database, Prefix: m.src.PostgresPrefix, Dialect: dialect,
				Migrate: func(ctx context.Context, id ids.WorkspaceID, d dbx.Execer, n pgdb.Names) error {
					return catalog.MigratePostgres(ctx, id, d, n)
				}}})
		if err != nil {
			return err
		}
	} else {
		if m.srcAdm, err = admin.Open(ctx, filepath.Join(m.src.DataDir, "system", "admin.db"), m.src.Synchronous); err != nil {
			return err
		}
		if m.srcWM, err = workspace.NewManager(workspace.Config{DataDir: m.src.DataDir, MaxOpen: 4, Synchronous: m.src.Synchronous,
			Migrate: catalog.Migrate}); err != nil {
			return err
		}
	}
	if m.to == config.DBPostgres {
		if m.dstAdm, err = admin.OpenPostgres(ctx, m.dstPC, pgdb.Names{Prefix: m.pg.Prefix}); err != nil {
			return err
		}
		return nil
	}
	if m.dstAdm, err = admin.Open(ctx, filepath.Join(m.dataDir, "system", "admin.db"), m.src.Synchronous); err != nil {
		return err
	}
	m.dstWM, err = workspace.NewManager(workspace.Config{DataDir: m.dataDir, MaxOpen: 4, Synchronous: m.src.Synchronous, Migrate: catalog.Migrate})
	return err
}

// copyAdmin copies the admin store, and the backups its records point at.
func (m *migration) copyAdmin(ctx context.Context) (*migrateReport, error) {
	list, err := m.srcAdm.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range list {
		if w.Status == admin.StatusRestoring {
			return nil, fmt.Errorf("workspace %s (%s) is being restored; start the server once so it finishes, stop it, then run migrate-backend again", w.ID, w.Name)
		}
	}
	srcBackups := m.src.BackupDir + string(filepath.Separator)
	res, err := m.srcAdm.CopyTo(ctx, m.dstAdm, admin.CopyOptions{Rewrite: func(table, column string, v any) any {
		if table == "_adm_backups" && column == "path" {
			if p, ok := v.(string); ok && strings.HasPrefix(p, srcBackups) {
				return filepath.Join(m.backupDir, strings.TrimPrefix(p, srcBackups))
			}
		}
		return v
	}})
	if err != nil {
		return nil, err
	}
	if err := copyTree(m.src.BackupDir, m.backupDir); err != nil {
		return nil, fmt.Errorf("copying backups: %w", err)
	}
	m.logf("admin store: %d users, %d workspaces, %d audit entries, %d backups copied (sessions are not: everyone signs in again)",
		res.Rows["_adm_users"], res.Rows["_adm_workspaces"], res.Rows["_adm_audit"], res.Rows["_adm_backups"])
	names := map[ids.WorkspaceID]string{}
	for _, w := range list {
		names[w.ID] = w.Name
	}
	rep := &migrateReport{From: m.src.DB, To: m.to, AppVersion: version, StartedAt: m.started, AdminRows: res.Rows}
	for _, w := range res.Workspaces {
		rep.Workspaces = append(rep.Workspaces, migrateWorkspace{ID: w.ID, Name: names[w.ID], Status: w.Status})
	}
	sort.Slice(rep.Workspaces, func(i, j int) bool { return rep.Workspaces[i].ID < rep.Workspaces[j].ID })
	return rep, nil
}

// copyWorkspaces moves each workspace through the portable archive.
func (m *migration) copyWorkspaces(ctx context.Context, rep *migrateReport) error {
	exporter := &backup.Service{DataDir: m.src.DataDir, BackupDir: m.src.BackupDir, Admin: m.srcAdm, Workspaces: m.srcWM}
	stage := filepath.Join(m.dataDir, "staging", "migrate")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(filepath.Join(m.dataDir, "staging")) }()
	for i := range rep.Workspaces {
		w := &rep.Workspaces[i]
		ok, err := m.sourceHas(ctx, w.ID)
		if err != nil {
			return err
		}
		if !ok {
			if w.Status != admin.StatusDeleted {
				return fmt.Errorf("workspace %s (%s) has no database in the source install", w.ID, w.Name)
			}
			w.Skipped = "deleted, and its data is already gone"
			continue
		}
		if err := m.copyWorkspace(ctx, exporter, stage, w); err != nil {
			return fmt.Errorf("workspace %s (%s): %w", w.ID, w.Name, err)
		}
		flagged := 0
		if w.Import != nil {
			flagged = len(w.Import.Flagged)
		}
		m.logf("workspace %s (%s): moved, %d flagged for review", w.ID, w.Name, flagged)
	}
	return nil
}

func (m *migration) sourceHas(ctx context.Context, id ids.WorkspaceID) (bool, error) {
	if m.src.DB == config.DBPostgres {
		return pgdb.SchemaExists(ctx, pgdb.Pool(m.srcPool), m.srcWM.Names(id).Schema())
	}
	p, err := m.srcWM.PathsFor(id)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p.DB)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (m *migration) copyWorkspace(ctx context.Context, exporter *backup.Service, stage string, w *migrateWorkspace) error {
	archive := filepath.Join(stage, string(w.ID)+".db")
	defer func() { _ = os.Remove(archive) }()
	l, err := m.srcWM.Acquire(ctx, w.ID)
	if err != nil {
		return err
	}
	_, err = exporter.ExportArchive(ctx, l, archive)
	l.Release()
	if err != nil {
		return err
	}
	if m.to == config.DBPostgres {
		w.Import, err = catalog.AdoptPostgresReport(ctx, m.dstPool, m.dstPC.ConnConfig.Database, m.pg.Prefix, m.dialect, w.ID, archive)
		if err != nil {
			return err
		}
	} else {
		if err := m.dstWM.Adopt(ctx, w.ID, archive); err != nil {
			return err
		}
		w.Notes = append(w.Notes, "saved SQL queries and Lua db.* strings stay in the PostgreSQL dialect they were written in; review them on SQLite")
	}
	srcBucket, err := files.BucketDir(m.src.DataDir, w.ID)
	if err != nil {
		return err
	}
	dstBucket, err := files.BucketDir(m.dataDir, w.ID)
	if err != nil {
		return err
	}
	if err := copyTree(srcBucket, dstBucket); err != nil {
		return fmt.Errorf("copying files: %w", err)
	}
	w.FilesBytes, err = treeBytes(dstBucket)
	return err
}

func (m *migration) summary(rep *migrateReport) {
	moved, flagged := 0, 0
	for _, w := range rep.Workspaces {
		if w.Skipped == "" {
			moved++
		}
		if w.Import != nil {
			flagged += len(w.Import.Flagged)
		}
	}
	m.logf("moved the install to %s: %d workspaces, %d objects flagged for review; report in %s",
		m.to, moved, flagged, filepath.Join(m.dataDir, "system", migrateReportFile))
	m.logf("the source install is unchanged. To run the moved install, set:")
	m.logf("  ABIDATA_DB=%s", m.to)
	m.logf("  ABIDATA_DATA_DIR=%s", m.dataDir)
	m.logf("  ABIDATA_BACKUP_DIR=%s", m.backupDir)
	if m.to == config.DBPostgres {
		m.logf("  ABIDATA_POSTGRES_URL (or _FILE): the connection string of ABIDATA_TARGET_POSTGRES_URL")
		m.logf("  ABIDATA_POSTGRES_SCHEMA_PREFIX=%s", m.pg.Prefix)
	} else {
		m.logf("  and unset ABIDATA_POSTGRES_URL")
	}
	if len(m.src.SessionKey) > 0 || len(m.src.ManifestKey) > 0 {
		m.logf("  and keep ABIDATA_SESSION_KEY / ABIDATA_MANIFEST_KEY as they are")
	}
}

// copyTree copies the regular files and directories under src to dst,
// keeping their permissions. A missing src copies nothing; dst must not
// hold any of the files.
func copyTree(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			return copyFile(p, target, info.Mode().Perm())
		}
		return nil // sockets, links: not part of an install
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // under the install's own directories
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // under the target directories
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func treeBytes(dir string) (int64, error) {
	var n int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			n += info.Size()
		}
		return nil
	})
	return n, err
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

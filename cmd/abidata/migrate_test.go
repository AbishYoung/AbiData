package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/dbx"
	"abidata/internal/demo"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/pgtest"
	"abidata/internal/workspace"
)

// demoInstall builds a stopped SQLite install with both sample workspaces
// and returns its configuration.
func demoInstall(t *testing.T) config.Config {
	t.Helper()
	ctx := context.Background()
	data := t.TempDir()
	cfg := config.Config{DB: config.DBSQLite, DataDir: data, BackupDir: filepath.Join(data, "backups"), Synchronous: "NORMAL"}
	if err := os.MkdirAll(filepath.Join(data, "system", "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	adm, err := admin.Open(ctx, filepath.Join(data, "system", "admin.db"), "NORMAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = adm.Close() }()
	u := admin.User{ID: "u1", Email: "ann@example.com", DisplayName: "Ann", PasswordHash: "hash-of-a-password", CreatedAt: time.Now(), IsPlatformAdmin: true}
	if err := adm.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := adm.Audit(ctx, admin.AuditEntry{At: time.Now(), UserID: "u1", Action: "test.setup"}); err != nil {
		t.Fatal(err)
	}
	wm, err := workspace.NewManager(workspace.Config{DataDir: data, MaxOpen: 4, Synchronous: "NORMAL", Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	b := demo.NewBuilder(adm, wm, nil, &files.Service{DataDir: data}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, k := range demo.Kinds {
		if _, err := b.Create(ctx, k, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := wm.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "system", "keys", "session.key"), []byte("c2Vzc2lvbi1rZXktZm9yLXRlc3RzLW9ubHktMzItYnl0ZXM"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// installCounts opens an install and counts each workspace's rows per table.
func installCounts(t *testing.T, cfg config.Config) (users []string, counts map[string]int64) {
	t.Helper()
	ctx := context.Background()
	counts = map[string]int64{}
	var adm *admin.Store
	var wm *workspace.Manager
	var err error
	if cfg.DB == config.DBPostgres {
		pc, err := pgxpool.ParseConfig(cfg.PostgresURL)
		if err != nil {
			t.Fatal(err)
		}
		pool, err := pgxpool.NewWithConfig(ctx, pc)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		dialect, err := pgdb.Dialect(ctx, pgdb.Pool(pool))
		if err != nil {
			t.Fatal(err)
		}
		if adm, err = admin.OpenPostgres(ctx, pc, pgdb.Names{Prefix: cfg.PostgresPrefix}); err != nil {
			t.Fatal(err)
		}
		wm, err = workspace.NewManager(workspace.Config{DataDir: cfg.DataDir, Postgres: &workspace.PostgresConfig{Pool: pool,
			Database: pc.ConnConfig.Database, Prefix: cfg.PostgresPrefix, Dialect: dialect,
			Migrate: func(ctx context.Context, id ids.WorkspaceID, d dbx.Execer, n pgdb.Names) error {
				return catalog.MigratePostgres(ctx, id, d, n)
			}}})
		if err != nil {
			t.Fatal(err)
		}
	} else {
		if adm, err = admin.Open(ctx, filepath.Join(cfg.DataDir, "system", "admin.db"), "NORMAL"); err != nil {
			t.Fatal(err)
		}
		if wm, err = workspace.NewManager(workspace.Config{DataDir: cfg.DataDir, MaxOpen: 4, Migrate: catalog.Migrate}); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _ = adm.Close() }()
	defer func() { _ = wm.Shutdown(ctx) }()
	u, err := adm.GetUserByEmail(ctx, "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	users = append(users, u.ID+" "+u.PasswordHash)
	list, err := adm.ListWorkspaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range list {
		l, err := wm.Acquire(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		err = l.Read(ctx, func(q dbx.Querier) error {
			rows, err := q.Query(ctx, `SELECT physical_name FROM _sys_tables`)
			if err != nil {
				return err
			}
			var tables []string
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					rows.Close()
					return err
				}
				tables = append(tables, n)
			}
			rows.Close()
			for _, tb := range tables {
				var n int64
				if err := q.QueryRow(ctx, `SELECT count(*) FROM `+catalog.Quote(tb)).Scan(&n); err != nil {
					return err
				}
				counts[w.Name+"."+tb] = n
			}
			return nil
		})
		l.Release()
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := files.BucketDir(cfg.DataDir, w.ID); b != "" {
			n, err := treeBytes(b)
			if err != nil {
				t.Fatal(err)
			}
			counts[w.Name+".files"] = n
		}
	}
	return users, counts
}

func sameCounts(t *testing.T, what string, got, want map[string]int64) {
	t.Helper()
	if len(want) < 5 {
		t.Fatalf("too few tables to compare: %v", want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s has %d, want %d", what, k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s: %d tables, want %d", what, len(got), len(want))
	}
}

func quietMigration(src config.Config, to, dataDir string, pg config.TargetPostgres) *migration {
	return &migration{src: src, to: to, dataDir: dataDir, pg: pg, out: io.Discard}
}

// TestMigrateBackendRoundTrip moves the demo install to PostgreSQL and back,
// and compares the admin store, every table's rows and the files (ADR 0012
// Phase 5's exit criterion).
func TestMigrateBackendRoundTrip(t *testing.T) {
	e := pgtest.New(t)
	ctx := context.Background()
	src := demoInstall(t)
	wantUsers, want := installCounts(t, src)

	pgDir := filepath.Join(t.TempDir(), "pg")
	target := config.TargetPostgres{URL: e.AppURL(), Prefix: e.Prefix}
	if err := quietMigration(src, config.DBPostgres, pgDir, target).run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pgDir, "system", migrateMarker)); err == nil {
		t.Fatal("the marker is still there after a finished move")
	}
	b, err := os.ReadFile(filepath.Join(pgDir, "system", migrateReportFile))
	if err != nil {
		t.Fatal(err)
	}
	var rep migrateReport
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Workspaces) != 2 || rep.Workspaces[0].Import == nil {
		t.Fatalf("report: %s", b)
	}
	if _, err := os.Stat(filepath.Join(pgDir, "system", "keys", "session.key")); err != nil {
		t.Fatalf("session key not carried over: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pgDir, "system", "admin.db")); err == nil {
		t.Fatal("a PostgreSQL target holds admin.db")
	}
	onPG := config.Config{DB: config.DBPostgres, DataDir: pgDir, BackupDir: filepath.Join(pgDir, "backups"), Synchronous: "NORMAL",
		PostgresURL: e.AppURL(), PostgresPrefix: e.Prefix, PostgresMaxConns: 8}
	if err := checkBackend(ctx, onPG); err != nil {
		t.Fatalf("the moved install does not pass the startup check: %v", err)
	}
	gotUsers, got := installCounts(t, onPG)
	if strings.Join(gotUsers, ",") != strings.Join(wantUsers, ",") {
		t.Errorf("users on PostgreSQL: %v, want %v", gotUsers, wantUsers)
	}
	sameCounts(t, "on PostgreSQL", got, want)

	// The source was left as it was, and a used target is refused.
	if err := quietMigration(src, config.DBPostgres, filepath.Join(t.TempDir(), "again"), target).run(ctx); err == nil ||
		!strings.Contains(err.Error(), "already holds schemas") {
		t.Fatalf("a second move into the same prefix: %v", err)
	}
	if err := quietMigration(src, config.DBPostgres, pgDir, config.TargetPostgres{URL: e.AppURL(), Prefix: e.Prefix + "b"}).run(ctx); err == nil ||
		!strings.Contains(err.Error(), "not empty") {
		t.Fatalf("a move into a used data directory: %v", err)
	}

	back := filepath.Join(t.TempDir(), "back")
	if err := quietMigration(onPG, config.DBSQLite, back, config.TargetPostgres{}).run(ctx); err != nil {
		t.Fatal(err)
	}
	onSQLite := config.Config{DB: config.DBSQLite, DataDir: back, BackupDir: filepath.Join(back, "backups"), Synchronous: "NORMAL"}
	if err := checkBackend(ctx, onSQLite); err != nil {
		t.Fatal(err)
	}
	gotUsers, got = installCounts(t, onSQLite)
	if strings.Join(gotUsers, ",") != strings.Join(wantUsers, ",") {
		t.Errorf("users back on SQLite: %v, want %v", gotUsers, wantUsers)
	}
	sameCounts(t, "back on SQLite", got, want)
}

// TestMigrateBackendFailureCleansUp: a move that fails removes what it
// wrote, and a move left behind by a crash is started over.
func TestMigrateBackendFailureCleansUp(t *testing.T) {
	e := pgtest.New(t)
	ctx := context.Background()
	src := demoInstall(t)
	// A workspace whose database is missing makes the move fail midway.
	adm, err := admin.Open(ctx, filepath.Join(src.DataDir, "system", "admin.db"), "NORMAL")
	if err != nil {
		t.Fatal(err)
	}
	broken := admin.Workspace{ID: ids.NewWorkspaceID(), Name: "Broken", Status: admin.StatusActive, CreatedAt: time.Now(), CreatedBy: "u1"}
	if err := adm.CreateWorkspace(ctx, broken, "u1"); err != nil {
		t.Fatal(err)
	}
	_ = adm.Close()

	dir := filepath.Join(t.TempDir(), "pg")
	target := config.TargetPostgres{URL: e.AppURL(), Prefix: e.Prefix}
	err = quietMigration(src, config.DBPostgres, dir, target).run(ctx)
	if err == nil || !strings.Contains(err.Error(), "has no database") {
		t.Fatalf("move with a missing workspace: %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) > 0 {
		t.Fatalf("the failed move left %v in the target directory", ents)
	}
	schemas, err := pgdb.InstallSchemas(ctx, pgdb.Pool(e.App), pgdb.Names{Prefix: e.Prefix})
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) > 0 {
		t.Fatalf("the failed move left schemas %v", schemas)
	}

	// A crashed move: the marker and some of its writes remain. The server
	// refuses the directory; running the move again starts it over.
	adm, err = admin.Open(ctx, filepath.Join(src.DataDir, "system", "admin.db"), "NORMAL")
	if err != nil {
		t.Fatal(err)
	}
	if err := adm.DropWorkspace(ctx, broken.ID); err != nil {
		t.Fatal(err)
	}
	_ = adm.Close()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "system", migrateMarker), migrateMarkerFile{From: "sqlite", To: "postgres", Prefix: e.Prefix}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	leftover, err := admin.OpenPostgres(ctx, e.App.Config(), pgdb.Names{Prefix: e.Prefix})
	if err != nil {
		t.Fatal(err)
	}
	_ = leftover.Close()
	err = checkBackend(ctx, config.Config{DB: config.DBPostgres, DataDir: dir, PostgresURL: e.AppURL()})
	if err == nil || !strings.Contains(err.Error(), "unfinished abidata migrate-backend") {
		t.Fatalf("startup check on an unfinished target: %v", err)
	}
	if err := quietMigration(src, config.DBPostgres, dir, target).run(ctx); err != nil {
		t.Fatalf("running the move again: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "leftover")); err == nil {
		t.Fatal("the interrupted move's files were kept")
	}
}

// TestMigrateBackendRefusals covers the checks made before anything is written.
func TestMigrateBackendRefusals(t *testing.T) {
	ctx := context.Background()
	src := demoInstall(t)
	cases := []struct {
		name string
		m    *migration
		want string
	}{
		{"same backend", quietMigration(src, config.DBSQLite, t.TempDir(), config.TargetPostgres{}), "already on sqlite"},
		{"inside the source", quietMigration(src, config.DBPostgres, filepath.Join(src.DataDir, "moved"), config.TargetPostgres{}), "overlaps"},
		{"the source itself", quietMigration(src, config.DBPostgres, src.DataDir, config.TargetPostgres{}), "overlaps"},
	}
	for _, c := range cases {
		if err := c.m.run(ctx); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// A running server holds the source's lock.
	lock, err := acquireLock(src.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	err = quietMigration(src, config.DBPostgres, filepath.Join(t.TempDir(), "x"), config.TargetPostgres{URL: "postgres://127.0.0.1:1/x", Prefix: "abd"}).run(ctx)
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Errorf("a locked source: %v", err)
	}
}

func TestCopyTree(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(filepath.Join(src, "a", "b"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a", "b", "f"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dst, "a", "b", "f"))
	if err != nil || fi.Mode().Perm() != 0o600 || fi.Size() != 5 {
		t.Fatalf("copied file: %v %v", fi, err)
	}
	if err := copyTree(filepath.Join(src, "missing"), dst); err != nil {
		t.Fatalf("a missing source: %v", err)
	}
	if err := copyTree(src, dst); err == nil {
		t.Fatal("copying over existing files succeeded")
	}
}

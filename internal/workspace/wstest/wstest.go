// Package wstest gives tests a workspace manager on the backend the test run
// selects (ADR 0012 §10): PostgreSQL when ABIDATA_TEST_DB=postgres, which
// make test-postgres sets, and SQLite otherwise.
package wstest

import (
	"context"
	"os"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/pgtest"
	"abidata/internal/workspace"
)

// Postgres reports whether tests run against PostgreSQL.
func Postgres() bool { return os.Getenv("ABIDATA_TEST_DB") == "postgres" }

// Env is a manager and, on PostgreSQL, its database.
type Env struct {
	*workspace.Manager
	PG *pgtest.Env // nil on SQLite
}

// New returns a manager with the catalog's migrations, shut down when the test ends.
func New(t testing.TB) *Env { t.Helper(); return NewWith(t, workspace.Config{}) }

// NewWith is New with cfg's settings; DataDir and the migrations are filled
// in when empty.
func NewWith(t testing.TB, cfg workspace.Config) *Env {
	t.Helper()
	ctx := context.Background()
	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}
	if cfg.Migrate == nil {
		cfg.Migrate = catalog.Migrate
	}
	var e *pgtest.Env
	if Postgres() {
		e = pgtest.New(t)
		d := pgdb.Pool(e.App)
		if err := pgdb.InstallLib(ctx, d, pgdb.Names{Prefix: e.Prefix}); err != nil {
			t.Fatal(err)
		}
		dialect, err := pgdb.Dialect(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Postgres = &workspace.PostgresConfig{Pool: e.App, Database: e.Database, Prefix: e.Prefix, Dialect: dialect, Migrate: catalog.MigratePostgres}
	}
	m, err := workspace.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Shutdown(ctx) })
	return &Env{Manager: m, PG: e}
}

// Create creates a workspace; on PostgreSQL its roles are dropped with the database.
func (e *Env) Create(t testing.TB) ids.WorkspaceID {
	t.Helper()
	id := ids.NewWorkspaceID()
	if e.PG != nil {
		n := e.Names(id)
		e.PG.DropRoles(n.ReadLogin(), n.WriteLogin(), n.WriteRole(), n.ReadRole())
	}
	if err := e.Manager.Create(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return id
}

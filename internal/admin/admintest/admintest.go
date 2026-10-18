// Package admintest opens an admin store for tests on the backend
// ABIDATA_TEST_DB selects (ADR 0012 §10): SQLite by default, or PostgreSQL
// with ABIDATA_TEST_DB=postgres, in a throwaway database (internal/pgtest).
package admintest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"abidata/internal/admin"
	"abidata/internal/pgdb"
	"abidata/internal/pgtest"
)

// Postgres reports whether tests run the admin store on PostgreSQL.
func Postgres() bool { return os.Getenv("ABIDATA_TEST_DB") == "postgres" }

// Opener returns a function that opens the same store each time it is called.
func Opener(t testing.TB) func() (*admin.Store, error) {
	t.Helper()
	if Postgres() {
		e := pgtest.New(t)
		n := pgdb.Names{Prefix: e.Prefix}
		return func() (*admin.Store, error) { return admin.OpenPostgres(context.Background(), e.App.Config(), n) }
	}
	path := filepath.Join(t.TempDir(), "admin.db")
	return func() (*admin.Store, error) { return admin.Open(context.Background(), path, "NORMAL") }
}

// Open opens a fresh store, closed when the test ends.
func Open(t testing.TB) *admin.Store {
	t.Helper()
	s, err := Opener(t)()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"abidata/internal/config"
	"abidata/internal/pgtest"
)

func TestCheckBackend(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := checkBackend(ctx, config.Config{DB: config.DBSQLite, DataDir: dir}); err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system", "admin.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := checkBackend(ctx, config.Config{DB: config.DBPostgres, DataDir: dir, PostgresURL: "postgres://127.0.0.1:1/x"})
	if err == nil || !strings.Contains(err.Error(), "SQLite install") {
		t.Fatalf("a SQLite data directory under ABIDATA_DB=postgres: %v", err)
	}
}

func TestCheckBackendPostgres(t *testing.T) {
	e := pgtest.New(t)
	err := checkBackend(context.Background(), config.Config{DB: config.DBPostgres, DataDir: t.TempDir(), PostgresURL: e.AppURL()})
	if !errors.Is(err, errPostgresWorkspaces) {
		t.Fatalf("a server the app can run on is checked, then refused until Phase 3: %v", err)
	}
	e.Superuser(t, `ALTER ROLE `+pgtest.Ident(e.Role)+` NOCREATEROLE`)
	err = checkBackend(context.Background(), config.Config{DB: config.DBPostgres, DataDir: t.TempDir(), PostgresURL: e.AppURL()})
	if err == nil || !strings.Contains(err.Error(), "CREATEROLE") {
		t.Fatalf("without CREATEROLE: %v", err)
	}
}

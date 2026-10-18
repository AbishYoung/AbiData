// Package pgtest gives tests a throwaway PostgreSQL database (ADR 0012 §10):
// a fresh database owned by a fresh non-superuser role with CREATEROLE, as the
// app runs in production. The server comes from ABIDATA_TEST_POSTGRES_URL
// (make test-postgres starts one); without it, tests that need PostgreSQL skip
// and say so.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Env is one test's database.
type Env struct {
	App      *pgxpool.Pool // logged in as Role
	Database string
	Role     string
	Prefix   string // a schema and role prefix unique to the test
	url      string
	pass     string
	roles    []string
}

// AppURL is a connection string for the app role, as an operator would put
// in ABIDATA_POSTGRES_URL.
func (e *Env) AppURL() string {
	u, err := url.Parse(e.url)
	if err != nil {
		return ""
	}
	u.User = url.UserPassword(e.Role, e.pass)
	u.Path = "/" + e.Database
	return u.String()
}

// DropRoles adds roles the test creates, to be dropped after the database.
func (e *Env) DropRoles(roles ...string) { e.roles = append(e.roles, roles...) }

// RandHex is n random bytes in hex.
func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Ident quotes a PostgreSQL identifier.
func Ident(s string) string { return pgx.Identifier{s}.Sanitize() }

// Require returns the test server's URL, or skips the test.
func Require(t testing.TB) string {
	t.Helper()
	url := os.Getenv("ABIDATA_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("PostgreSQL-only: set ABIDATA_TEST_POSTGRES_URL to run")
	}
	return url
}

// New creates the database and role, and drops both when the test ends.
func New(t testing.TB) *Env {
	t.Helper()
	url := Require(t)
	ctx := context.Background()
	su, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = su.Close(ctx) }()
	suffix := RandHex(4)
	e := &Env{Database: "abd_test_" + suffix, Role: "abd_test_app_" + suffix, Prefix: "t" + suffix, url: url}
	pass := RandHex(16)
	e.pass = pass
	for _, q := range []string{
		`CREATE ROLE ` + Ident(e.Role) + ` LOGIN CREATEROLE PASSWORD '` + pass + `'`,
		`CREATE DATABASE ` + Ident(e.Database) + ` OWNER ` + Ident(e.Role),
	} {
		if _, err := su.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database, cfg.ConnConfig.User, cfg.ConnConfig.Password = e.Database, e.Role, pass
	if e.App, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		e.App.Close()
		stmts := []string{`DROP DATABASE IF EXISTS ` + Ident(e.Database) + ` WITH (FORCE)`}
		for _, r := range append(e.roles, e.Role) {
			stmts = append(stmts, `DROP ROLE IF EXISTS `+Ident(r))
		}
		e.Superuser(t, stmts...)
	})
	return e
}

// Superuser runs statements as the test server's superuser, logging failures
// (for cleanup).
func (e *Env) Superuser(t testing.TB, stmts ...string) {
	t.Helper()
	ctx := context.Background()
	su, err := pgx.Connect(ctx, e.url)
	if err != nil {
		t.Log(err)
		return
	}
	defer func() { _ = su.Close(ctx) }()
	for _, q := range stmts {
		if _, err := su.Exec(ctx, q); err != nil {
			t.Log(err)
		}
	}
}

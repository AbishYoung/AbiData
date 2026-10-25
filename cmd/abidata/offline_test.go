package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"abidata/internal/pgtest"
)

// TestOfflineCommandsOnPostgres: `abidata user create-admin` and `abidata
// demo` work on a PostgreSQL install (they used to open a SQLite admin.db in
// the data directory, which the server then refused), and the server starts
// on what they created.
func TestOfflineCommandsOnPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	e := pgtest.New(t)
	bin := buildBinary(t)
	s := newDemoServer(t, bin, "ABIDATA_DEMO_WORKSPACE=false", "ABIDATA_DB=postgres",
		"ABIDATA_POSTGRES_URL="+e.AppURL(), "ABIDATA_POSTGRES_SCHEMA_PREFIX="+e.Prefix)
	cli := exec.Command(bin, "user", "create-admin", "--email", "admin@example.com", "--name", "Admin")
	cli.Env, cli.Stdin = s.env, strings.NewReader("long-enough-pw\n")
	if out, err := cli.CombinedOutput(); err != nil {
		t.Fatalf("user create-admin: %v %s", err, out)
	}
	cli = exec.Command(bin, "demo", "--owner", "admin@example.com")
	cli.Env = s.env
	if out, err := cli.CombinedOutput(); err != nil || !strings.Contains(string(out), `created workspace "Northwind Traders"`) {
		t.Fatalf("abidata demo: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(envValue(s.env, "ABIDATA_DATA_DIR"), "system", "admin.db")); err == nil {
		t.Fatal("an offline command created a SQLite admin.db in a PostgreSQL install")
	}
	s.start()
	st, me := s.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@example.com", "password": "long-enough-pw"})
	if st != 200 {
		t.Fatalf("login: %d %v", st, me)
	}
	if ms, _ := me["memberships"].([]any); len(ms) < 2 {
		t.Fatalf("memberships: %v", me["memberships"])
	}
	s.stop()
}

// envValue is the last value of name in env.
func envValue(env []string, name string) string {
	v := ""
	for _, kv := range env {
		if x, ok := strings.CutPrefix(kv, name+"="); ok {
			v = x
		}
	}
	return v
}

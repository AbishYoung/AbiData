package statements_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/ids"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"

	// Every package that declares statements, so they are registered.
	_ "abidata/internal/backup"
	_ "abidata/internal/files"
	_ "abidata/internal/forms"
	_ "abidata/internal/httpapi"
	_ "abidata/internal/ingest"
	_ "abidata/internal/jobs"
	_ "abidata/internal/ops"
	_ "abidata/internal/query"
	_ "abidata/internal/records"
	_ "abidata/internal/reports"
	_ "abidata/internal/scripts"
	_ "abidata/internal/workspace"
)

// TestStatementsPrepare prepares every registered statement against a
// freshly migrated admin database and workspace database, so a statement
// that names a missing table or column fails here, not in production. The
// result columns of each are kept in testdata/statements.golden
// (ABIDATA_UPDATE_GOLDEN=1 rewrites it), so a change to what a statement
// returns shows in review.
func TestStatementsPrepare(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := admin.Open(ctx, filepath.Join(dir, "admin.db"), "NORMAL")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	adminDB := openConn(t, filepath.Join(dir, "admin.db"))
	wsDB := openConn(t, filepath.Join(dir, "ws.db"))
	if err := catalog.Migrate(ctx, ids.NewWorkspaceID(), wsDB); err != nil {
		t.Fatal(err)
	}
	// sqlite_sequence exists once a workspace has a user table, which every
	// real one does by the time the catalog rebuilds a table.
	if _, err := wsDB.Exec(ctx, `CREATE TABLE "Stock" (id INTEGER PRIMARY KEY AUTOINCREMENT)`); err != nil {
		t.Fatal(err)
	}
	// An import's staging database, as ingest creates it.
	stagingDB := openConn(t, filepath.Join(dir, "staging.db"))
	if _, err := stagingDB.Exec(ctx, static(t, "ingest.createStagingSchema").SQL); err != nil {
		t.Fatal(err)
	}
	dbs := []struct {
		name string
		c    *sqlitex.Conn
	}{{"workspace", wsDB}, {"admin", adminDB}, {"staging", stagingDB}}

	var golden strings.Builder
	for _, s := range sqlgen.Statics() {
		// A statement runs against one of the databases; which one is
		// recorded in the golden file, so a move between them shows too.
		var db string
		var cols []string
		var err error
		for _, d := range dbs {
			if strings.HasPrefix(s.Name, "admin.") && d.name != "admin" {
				continue
			}
			var e error
			if cols, e = d.c.Prepare(ctx, s.SQL); e == nil {
				db, err = d.name, nil
				break
			}
			if err == nil {
				err = e
			}
		}
		if db == "" {
			t.Errorf("%s does not prepare: %v\n%s", s.Name, err, s.SQL)
			continue
		}
		dialect := "portable"
		if s.SQLiteOnly() {
			dialect = "sqlite"
		}
		for i, c := range cols {
			cols[i] = strings.Join(strings.Fields(c), " ") // an unaliased expression keeps its line breaks
		}
		fmt.Fprintf(&golden, "%s\t%s\t%s\t%s\n", s.Name, db, dialect, strings.Join(cols, ","))
	}
	path := filepath.Join("testdata", "statements.golden")
	if os.Getenv("ABIDATA_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(golden.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != golden.String() {
		t.Errorf("statements changed; run with ABIDATA_UPDATE_GOLDEN=1 and review the diff of %s", path)
	}
}

// TestEveryStatementPackageIsImported keeps the import list above complete:
// a package with a statements.go that is not imported would go unchecked.
func TestEveryStatementPackageIsImported(t *testing.T) {
	registered := map[string]bool{}
	for _, s := range sqlgen.Statics() {
		registered[s.Name[:strings.IndexByte(s.Name, '.')]] = true
	}
	files, _ := filepath.Glob("../*/statements.go")
	for _, f := range files {
		if pkg := filepath.Base(filepath.Dir(f)); !registered[pkg] {
			t.Errorf("internal/%s declares statements but the conformance test does not import it", pkg)
		}
	}
}

func static(t *testing.T, name string) *sqlgen.Static {
	for _, s := range sqlgen.Statics() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no statement %s", name)
	return nil
}

func openConn(t *testing.T, path string) *sqlitex.Conn {
	t.Helper()
	c, err := sqlitex.Open(context.Background(), path, sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

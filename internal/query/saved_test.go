package query

import (
	"context"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

func TestSavedQueriesLifecycle(t *testing.T) {
	f, _ := shopSchema(t)
	ctx := context.Background()
	cache := catalog.NewCache()
	svc := &Service{Schemas: cache}
	cust, name := "01J00000000000000000000C01", "01J00000000000000000000C02"
	base, err := svc.Put(ctx, f.l, "", 0, SavedInput{Name: "Customer names", Kind: "visual", Visual: &Model{
		Sources: []Source{{Alias: "c", TableID: cust}},
		Fields:  []Field{{Alias: "c", ColumnID: "id"}, {Alias: "c", ColumnID: "_version"}, {Alias: "c", ColumnID: name, Sort: "asc"}}}}, "u1")
	if err != nil || !base.Editable || base.Version != 1 || base.Dialect != "sqlite" {
		t.Fatalf("%+v %v", base, err)
	}
	// A query using the first query as a source.
	top, err := svc.Put(ctx, f.l, "", 0, SavedInput{Name: "Top", Kind: "visual", Visual: &Model{
		Sources: []Source{{Alias: "q", QueryID: base.ID}}, Fields: []Field{{Alias: "q", ColumnID: "Name"}}, Limit: 1}}, "u1")
	if err != nil || !strings.Contains(top.SQL, `FROM (SELECT`) {
		t.Fatalf("%+v %v", top, err)
	}
	r, err := svc.RunSaved(ctx, f.l, Policy{}, top.ID, nil, 0)
	if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != "Ada" {
		t.Fatalf("%+v %v", r, err)
	}
	// Stale version.
	if _, err := svc.Put(ctx, f.l, base.ID, 99, SavedInput{Name: "x", Kind: "visual", Visual: base.Visual}, "u1"); fault.Classify(err).Code != fault.ConcurrentModification {
		t.Fatalf("stale: %v", err)
	}
	// Deleting a query another query uses is refused.
	if err := svc.Delete(ctx, f.l, base.ID, 1); fault.Classify(err).Code != fault.LinkedRecords {
		t.Fatalf("delete used: %v", err)
	}
	// SQL-kind queries are checked against the editor policy.
	if _, err := svc.Put(ctx, f.l, "", 0, SavedInput{Name: "Leak", Kind: "sql", SQL: "SELECT source FROM _sys_scripts"}, "u1"); fault.Classify(err).Code != fault.SQLNotAllowed {
		t.Fatalf("sys read saved: %v", err)
	}
	if _, err := svc.Put(ctx, f.l, "", 0, SavedInput{Name: "customer names", Kind: "sql", SQL: "SELECT 1"}, "u1"); fault.Classify(err).Code != fault.DuplicateValue {
		t.Fatalf("duplicate name: %v", err)
	}
	us, err := Usages(ctx, f.l, "query", base.ID)
	if err != nil || len(us) != 1 || us[0].Name != "Top" {
		t.Fatalf("usages: %+v %v", us, err)
	}

	// Renaming the column's physical name: visual queries follow by ID (no breaking issue).
	e := &catalog.Engine{Validators: []catalog.Validator{SchemaValidator}}
	rep, err := e.Run(ctx, f.l, []catalog.Op{{Kind: catalog.OpRenameColumn, TableID: cust, ColumnID: name, PhysicalName: "FullName"}}, catalog.Apply, catalog.ApplyOptions{})
	if err != nil || len(rep.Breaking) != 0 {
		t.Fatalf("rename: %+v %v", rep, err)
	}
	got, _ := svc.Get(ctx, f.l, base.ID)
	if !strings.Contains(got.SQL, `"c"."FullName"`) {
		t.Fatalf("visual query not recompiled: %s", got.SQL)
	}
	if r, err := svc.RunSaved(ctx, f.l, Policy{}, top.ID, nil, 0); err != nil || r.Rows[0][0] != "Ada" {
		t.Fatalf("dependent after rename: %+v %v", r, err)
	}
	// Dropping the column breaks the query: reported once, apply blocked.
	rep, err = e.Run(ctx, f.l, []catalog.Op{{Kind: catalog.OpDropColumn, TableID: cust, ColumnID: name}}, catalog.Preview, catalog.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, b := range rep.Breaking {
		if b.ID == base.ID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("breaking issues for base query: %d (%+v)", n, rep.Breaking)
	}
}

func TestRefreshCompiledRewritesStaleSQL(t *testing.T) {
	f, _ := shopSchema(t)
	ctx := context.Background()
	cache := catalog.NewCache()
	svc := &Service{Schemas: cache}
	q, err := svc.Put(ctx, f.l, "", 0, SavedInput{Name: "Names", Kind: "visual", Visual: &Model{
		Sources: []Source{{Alias: "c", TableID: "01J00000000000000000000C01"}},
		Fields:  []Field{{Alias: "c", ColumnID: "01J00000000000000000000C02"}}}}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate SQL stored by an older compiler, and a workspace never refreshed.
	if err := f.l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE _sys_queries SET sql_text = 'SELECT 1' WHERE id = ?`, q.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM _sys_meta WHERE key = 'query_compiler'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if err := f.l.Maintenance(ctx, func(c *sqlitex.Conn) error {
			sch, err := catalog.Load(ctx, c)
			if err != nil {
				return err
			}
			return RefreshCompiled(ctx, c, sch)
		}); err != nil {
			t.Fatal(err)
		}
	}
	stored := func() string {
		t.Helper()
		var sql string
		if err := f.l.Read(ctx, func(r dbx.Querier) error {
			return r.QueryRow(ctx, `SELECT sql_text FROM _sys_queries WHERE id = ?`, q.ID).Scan(&sql)
		}); err != nil {
			t.Fatal(err)
		}
		return sql
	}
	refresh()
	if got := stored(); got != q.SQL {
		t.Fatalf("stale SQL not refreshed: %q want %q", got, q.SQL)
	}
	// Once the version is recorded, a refresh leaves stored SQL alone.
	if err := f.l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE _sys_queries SET sql_text = 'SELECT 2' WHERE id = ?`, q.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	refresh()
	if got := stored(); got != "SELECT 2" {
		t.Fatalf("refresh ran twice: %q", got)
	}
}

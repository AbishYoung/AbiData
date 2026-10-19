package catalog

// ADR 0012 Phase 3: schema changes on a PostgreSQL workspace. The changes
// that need no table rebuild run natively; rebuilds come next.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/pgtest"
	"abidata/internal/workspace"
)

func newPGFixture(t *testing.T) (*fixture, *pgtest.Env) {
	t.Helper()
	e := pgtest.New(t)
	ctx := context.Background()
	d := pgdb.Pool(e.App)
	if err := pgdb.InstallLib(ctx, d, pgdb.Names{Prefix: e.Prefix}); err != nil {
		t.Fatal(err)
	}
	dialect, err := pgdb.Dialect(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	m, err := workspace.NewManager(workspace.Config{DataDir: t.TempDir(), Postgres: &workspace.PostgresConfig{
		Pool: e.App, Database: e.Database, Prefix: e.Prefix, Dialect: dialect, Migrate: MigratePostgres}})
	if err != nil {
		t.Fatal(err)
	}
	id := ids.NewWorkspaceID()
	n := m.Names(id)
	e.DropRoles(n.ReadLogin(), n.WriteLogin(), n.WriteRole(), n.ReadRole())
	if err := m.Create(ctx, id); err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	return &fixture{t: t, m: m, l: l, e: &Engine{}, ctx: ctx, id: id}, e
}

func (f *fixture) write(sql string, args ...any) error {
	return f.l.Write(f.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(f.ctx, sql, args...)
		return err
	})
}

func TestPostgresApplyNative(t *testing.T) {
	f, e := newPGFixture(t)
	r := f.apply(allTypesTable())
	if len(r.Breaking) != 0 || r.SchemaVersion != 1 {
		t.Fatalf("%+v", r)
	}
	s := f.schema()
	tb, ok := s.TableByPhysical("Customers")
	if !ok || len(tb.Columns) != 11 {
		t.Fatalf("%+v", tb)
	}
	if err := f.write(`INSERT INTO "Customers" ("Name", "Visits", "Born", "Seen_At", "Website") VALUES ('Ada', 3, '1815-12-10', '2026-01-02T03:04:05.000Z', 'https://ada.example')`); err != nil {
		t.Fatal(err)
	}
	var label string
	var active bool
	if err := f.l.Read(f.ctx, func(q dbx.Querier) error {
		return q.QueryRow(f.ctx, `SELECT "Label", "Active" FROM "Customers"`).Scan(&label, &active)
	}); err != nil || label != "Ada (3)" || active {
		t.Fatalf("label %q active %v: %v", label, active, err)
	}
	// The row's checks hold on PostgreSQL too.
	for _, bad := range []string{
		`INSERT INTO "Customers" ("Name", "Visits") VALUES ('Bob', -1)`,
		`INSERT INTO "Customers" ("Visits") VALUES (1)`,
		`INSERT INTO "Customers" ("Name") VALUES ('` + strings.Repeat("x", 41) + `')`,
		`INSERT INTO "Customers" ("Name", "Website") VALUES ('Eve', 'not a url')`,
	} {
		if err := f.write(bad); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}

	// The sandbox's read role may read the new table.
	n, _ := f.l.Postgres()
	var can bool
	if err := e.App.QueryRow(f.ctx, `SELECT has_table_privilege($1, $2, 'SELECT')`, n.ReadRole(), pgdb.Ident(n.Schema())+`."Customers"`).Scan(&can); err != nil || !can {
		t.Fatalf("read role SELECT: %v %v", can, err)
	}

	name, _ := tb.ColumnByPhysical("Name")
	visits, _ := tb.ColumnByPhysical("Visits")
	f.apply(
		Op{Kind: OpRenameTable, TableID: tb.ID, DisplayName: "Clients", PhysicalName: "Clients"},
		Op{Kind: OpAddColumn, TableID: tb.ID, Column: &ColumnSpec{DisplayName: "City", Type: ShortText}},
		Op{Kind: OpAddColumn, TableID: tb.ID, Column: &ColumnSpec{DisplayName: "Twice", Type: Calculated, Options: TypeOptions{Expression: "Visits * 2", ResultType: "integer"}}},
		Op{Kind: OpRenameColumn, TableID: tb.ID, ColumnID: visits.ID, DisplayName: "Count", PhysicalName: "Count"},
		Op{Kind: OpCreateIndex, Index: &IndexSpec{TableID: tb.ID, ColumnIDs: []string{name.ID}}},
	)
	var twice int64
	if err := f.l.Read(f.ctx, func(q dbx.Querier) error {
		return q.QueryRow(f.ctx, `SELECT "Twice" FROM "Clients"`).Scan(&twice)
	}); err != nil || twice != 6 {
		t.Fatalf("twice %d: %v", twice, err)
	}
	s = f.schema()
	tb, _ = s.TableByPhysical("Clients")
	if tw, _ := tb.ColumnByPhysical("Twice"); tw.Options.Expression != "Count * 2" && tw.Options.Expression != `"Count" * 2` {
		t.Errorf("expression after rename: %q", tw.Options.Expression)
	}
	if len(tb.Indexes) != 1 {
		t.Fatalf("indexes %+v", tb.Indexes)
	}

	// A unique index over duplicates is a violation, reported without aborting.
	if err := f.write(`INSERT INTO "Clients" ("Name") VALUES ('ada')`); err != nil {
		t.Fatal(err)
	}
	_, err := f.run(Apply, ApplyOptions{}, Op{Kind: OpCreateIndex, Index: &IndexSpec{TableID: tb.ID, Unique: true, ColumnIDs: []string{name.ID}}})
	var fe *fault.Error
	if !errors.As(err, &fe) || fe.Code != fault.SchemaChangeBlocked {
		t.Fatalf("unique index over duplicates: %v", err)
	}

	f.apply(Op{Kind: OpDropIndex, IndexID: tb.Indexes[0].ID})
	f.apply(Op{Kind: OpDropTable, TableID: tb.ID})
	if s := f.schema(); len(s.Tables) != 0 {
		t.Fatalf("tables left: %+v", s.Tables)
	}
	var drift []string
	if err := f.l.Read(f.ctx, func(q dbx.Querier) error {
		var err error
		drift, err = Reconcile(f.ctx, q, f.schema())
		return err
	}); err != nil || len(drift) != 0 {
		t.Fatalf("drift %v: %v", drift, err)
	}
}

func (f *fixture) rows(sql string) string {
	f.t.Helper()
	var out [][]any
	if err := f.l.Read(f.ctx, func(q dbx.Querier) error {
		rows, err := q.Query(f.ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			out = append(out, rows.Values())
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
	return fmt.Sprint(out)
}

func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func (f *fixture) drift() []string {
	f.t.Helper()
	var drift []string
	if err := f.l.Read(f.ctx, func(q dbx.Querier) error {
		var err error
		drift, err = Reconcile(f.ctx, q, f.schema())
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return drift
}

// TestPostgresRebuild mirrors TestRebuildPreservesDataSequenceAndConstraints.
func TestPostgresRebuild(t *testing.T) {
	f, e := newPGFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "T", Columns: []ColumnSpec{
		{DisplayName: "A", Type: ShortText}, {DisplayName: "B", Type: Integer}, {DisplayName: "C", Type: LongText}}}})
	tb, _ := f.schema().TableByPhysical("T")
	a, _ := tb.ColumnByPhysical("A")
	if err := f.write(`INSERT INTO "T" ("A", "B", "C") VALUES ('x', 1, 'c'), ('y', NULL, NULL), ('z', 3, NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := f.write(`DELETE FROM "T" WHERE id = 3`); err != nil {
		t.Fatal(err)
	}
	f.apply(Op{Kind: OpCreateIndex, Index: &IndexSpec{TableID: tb.ID, ColumnIDs: []string{a.ID}}})
	// Making A required with a validation rule forces a rebuild.
	f.apply(Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: a.ID, Column: &ColumnSpec{DisplayName: "A", Type: ShortText, Required: true,
		ValidationSQL: "length(A) = 1"}})
	if err := f.write(`INSERT INTO "T" ("A") VALUES ('w')`); err != nil {
		t.Fatal(err)
	}
	if got := f.rows(`SELECT id, "A", "B" FROM "T" ORDER BY id`); got != "[[1 x 1] [2 y <nil>] [4 w <nil>]]" {
		t.Fatalf("data/sequence after rebuild: %s", got)
	}
	if err := f.write(`INSERT INTO "T" ("A") VALUES ('long')`); sqlState(err) != "23514" {
		t.Fatalf("validation after rebuild: %v", err)
	}
	// Required on a column with NULLs blocks, even with force, and names the rows.
	b, _ := tb.ColumnByPhysical("B")
	r, err := f.run(Apply, ApplyOptions{Force: true}, Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: b.ID,
		Column: &ColumnSpec{DisplayName: "B", Type: Integer, Required: true}})
	if fault.Classify(err).Code != fault.SchemaChangeBlocked {
		t.Fatalf("required with NULLs not blocked: %+v %v", r, err)
	}
	if !strings.Contains(fmt.Sprint(fault.Classify(err).Details), "2 rows") || !strings.Contains(fmt.Sprint(fault.Classify(err).Details), "row IDs: 2, 4") {
		t.Errorf("report: %v", fault.Classify(err).Details)
	}
	if f.rows(`SELECT count(*) FROM "T"`) != "[[3]]" {
		t.Fatal("rows lost")
	}
	// Dropping a column rebuilds too; the index, grants and identity carry over.
	c, _ := tb.ColumnByPhysical("C")
	f.apply(Op{Kind: OpDropColumn, TableID: tb.ID, ColumnID: c.ID})
	if err := f.write(`INSERT INTO "T" ("A") VALUES ('v')`); err != nil {
		t.Fatal(err)
	}
	// The insert that broke the rule used id 5: a sequence is not rolled back.
	if got := f.rows(`SELECT max(id) FROM "T"`); got != "[[6]]" {
		t.Fatalf("identity after second rebuild: %s", got)
	}
	n, _ := f.l.Postgres()
	var can bool
	if err := e.App.QueryRow(f.ctx, `SELECT has_table_privilege($1, $2, 'SELECT')`, n.ReadRole(), pgdb.Ident(n.Schema())+`."T"`).Scan(&can); err != nil || !can {
		t.Fatalf("read role SELECT after rebuild: %v %v", can, err)
	}
	if d := f.drift(); len(d) > 0 {
		t.Fatal(d)
	}
}

// TestPostgresLookupRelationshipAndFKCheck mirrors TestLookupRelationshipAndFKCheck,
// and rebuilds a parent table under its children's foreign keys.
func TestPostgresLookupRelationshipAndFKCheck(t *testing.T) {
	f, _ := newPGFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Customers", Columns: []ColumnSpec{{DisplayName: "Name", Type: ShortText}}}})
	cust, _ := f.schema().TableByPhysical("Customers")
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Orders", Columns: []ColumnSpec{
		{DisplayName: "Customer", Type: Lookup, Lookup: &LookupSpec{ParentTableID: cust.ID}}}}})
	s := f.schema()
	if len(s.Relationships) != 1 || s.Relationships[0].OnDelete != Restrict {
		t.Fatalf("%+v", s.Relationships)
	}
	for _, q := range []string{`INSERT INTO "Customers" ("Name") VALUES ('Ada')`, `INSERT INTO "Orders" ("Customer") VALUES (1)`} {
		if err := f.write(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.write(`INSERT INTO "Orders" ("Customer") VALUES (99)`); sqlState(err) != "23503" {
		t.Fatalf("FK not enforced: %v", err)
	}
	if err := f.write(`DELETE FROM "Customers" WHERE id = 1`); sqlState(err) != "23001" && sqlState(err) != "23503" {
		t.Fatalf("RESTRICT not enforced: %v", err)
	}
	if _, err := f.run(Preview, ApplyOptions{}, Op{Kind: OpDropTable, TableID: cust.ID}); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("parent drop allowed: %v", err)
	}

	// Rebuilding the parent keeps the child's key.
	name, _ := cust.ColumnByPhysical("Name")
	f.apply(Op{Kind: OpAlterColumn, TableID: cust.ID, ColumnID: name.ID, Column: &ColumnSpec{DisplayName: "Name", Type: ShortText, ValidationSQL: "length(Name) > 0"}})
	if err := f.write(`INSERT INTO "Orders" ("Customer") VALUES (99)`); sqlState(err) != "23503" {
		t.Fatalf("FK lost in the parent's rebuild: %v", err)
	}
	if got := f.rows(`SELECT count(*) FROM pg_constraint WHERE conname LIKE 'fk\_%' AND NOT convalidated`); got != "[[0]]" {
		t.Fatalf("keys left NOT VALID: %s", got)
	}

	// A relationship over an orphaned integer column is reported.
	orders, _ := f.schema().TableByPhysical("Orders")
	f.apply(Op{Kind: OpAddColumn, TableID: orders.ID, Column: &ColumnSpec{DisplayName: "Ref", Type: Integer}})
	if err := f.write(`UPDATE "Orders" SET "Ref" = 42`); err != nil {
		t.Fatal(err)
	}
	orders, _ = f.schema().TableByPhysical("Orders")
	ref, _ := orders.ColumnByPhysical("Ref")
	r, err := f.run(Preview, ApplyOptions{}, Op{Kind: OpCreateRelationship, Relationship: &RelationshipSpec{Name: "Orders ref",
		ParentTableID: cust.ID, ChildTableID: orders.ID, ChildColumnID: ref.ID}})
	if err != nil || len(r.Breaking) == 0 || r.Breaking[0].Kind != "fk_violation" {
		t.Fatalf("%+v %v", r, err)
	}
	// Without orphans it applies, validated.
	if err := f.write(`UPDATE "Orders" SET "Ref" = 1`); err != nil {
		t.Fatal(err)
	}
	f.apply(Op{Kind: OpCreateRelationship, Relationship: &RelationshipSpec{Name: "Orders ref",
		ParentTableID: cust.ID, ChildTableID: orders.ID, ChildColumnID: ref.ID}})
	if err := f.write(`UPDATE "Orders" SET "Ref" = 7`); sqlState(err) != "23503" {
		t.Fatalf("new relationship not enforced: %v", err)
	}
	if d := f.drift(); len(d) > 0 {
		t.Fatal(d)
	}
}

// TestPostgresTypeChangeRefused: converting a field's type is not on
// PostgreSQL yet; it says so and changes nothing.
func TestPostgresTypeChangeRefused(t *testing.T) {
	f, _ := newPGFixture(t)
	f.apply(allTypesTable())
	tb, _ := f.schema().TableByPhysical("Customers")
	notes, _ := tb.ColumnByPhysical("Notes")
	_, err := f.run(Apply, ApplyOptions{}, Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: notes.ID, Column: &ColumnSpec{DisplayName: "Notes", Type: Integer}})
	var fe *fault.Error
	if !errors.As(err, &fe) || fe.Code != fault.BadRequest || !strings.Contains(fe.DisplayMessage(), "PostgreSQL") {
		t.Fatalf("type change: %v", err)
	}
	if s := f.schema(); s.Version != 1 {
		t.Fatalf("schema version %d", s.Version)
	}
}

package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

type fixture struct {
	t   *testing.T
	m   *workspace.Manager
	l   *workspace.Lease
	e   *Engine
	ctx context.Context
	dir string
	id  ids.WorkspaceID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return openFixture(t, t.TempDir(), "")
}

func openFixture(t *testing.T, dir string, id ids.WorkspaceID) *fixture {
	t.Helper()
	ctx := context.Background()
	m, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: Migrate})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		id = ids.NewWorkspaceID()
		if err := m.Create(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	return &fixture{t: t, m: m, l: l, e: &Engine{}, ctx: ctx, dir: dir, id: id}
}

func (f *fixture) run(mode Mode, opt ApplyOptions, ops ...Op) (ImpactReport, error) {
	f.t.Helper()
	return f.e.Run(f.ctx, f.l, ops, mode, opt)
}

func (f *fixture) apply(ops ...Op) ImpactReport {
	f.t.Helper()
	r, err := f.run(Apply, ApplyOptions{UserID: "u1"}, ops...)
	if err != nil {
		f.t.Fatalf("apply: %v", err)
	}
	return r
}

func (f *fixture) schema() *Schema {
	f.t.Helper()
	var s *Schema
	if err := f.l.Read(f.ctx, func(q sqlitex.Querier) error {
		var err error
		s, err = Load(f.ctx, q)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if err := f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(f.ctx, sql, args...)
		return err
	}); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *fixture) query(sql string, args ...any) [][]any {
	f.t.Helper()
	var out [][]any
	if err := f.l.Read(f.ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(f.ctx, sql, args...)
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
	return out
}

// fingerprint hashes sqlite_schema and every row of every table.
func (f *fixture) fingerprint() string {
	f.t.Helper()
	h := sha256.New()
	// sqlite_stat* tables come from PRAGMA optimize on close; they are not schema.
	for _, r := range f.query(`SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_stat%' ORDER BY name`) {
		fmt.Fprintln(h, r...)
	}
	for _, r := range f.query(`SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_stat%' ORDER BY name`) {
		var lines []string
		for _, row := range f.query(fmt.Sprintf(`SELECT * FROM %s`, Quote(r[0].(string)))) {
			lines = append(lines, fmt.Sprintln(row...))
		}
		sort.Strings(lines)
		fmt.Fprintln(h, lines)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func allTypesTable() Op {
	return Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Customers", Columns: []ColumnSpec{
		{DisplayName: "Name", Type: ShortText, Required: true, Options: TypeOptions{MaxLength: 40}},
		{DisplayName: "Notes", Type: LongText},
		{DisplayName: "Visits", Type: Integer, ValidationSQL: "Visits >= 0", ValidationText: "Visits cannot be negative."},
		{DisplayName: "Score", Type: Number},
		{DisplayName: "Balance", Type: Currency, DefaultSQL: "0"},
		{DisplayName: "Active", Type: YesNo},
		{DisplayName: "Born", Type: Date},
		{DisplayName: "Seen At", Type: DateTime},
		{DisplayName: "Website", Type: Hyperlink},
		{DisplayName: "Label", Type: Calculated, Options: TypeOptions{Expression: "Name || ' (' || Visits || ')'", ResultType: "text"}},
	}}}
}

func TestCreateTableAllTypes(t *testing.T) {
	f := newFixture(t)
	r := f.apply(allTypesTable())
	if len(r.Breaking) != 0 || r.SchemaVersion != 1 {
		t.Fatalf("%+v", r)
	}
	s := f.schema()
	tb, ok := s.TableByPhysical("Customers")
	if !ok || len(tb.Columns) != 11 || tb.Columns[0].PhysicalName != "id" || tb.Columns[8].PhysicalName != "Seen_At" {
		t.Fatalf("%+v", tb)
	}
	f.exec(`INSERT INTO Customers(Name, Visits, Born, Seen_At, Website) VALUES ('Ada', 3, '1815-12-10', '2026-01-02T03:04:05.000Z', 'https://ada.example')`)
	if got := f.query(`SELECT Label, Active, Balance, _version FROM Customers`); fmt.Sprint(got) != "[[Ada (3) 0 0 1]]" {
		t.Fatalf("%v", got)
	}
	// Constraints are enforced by SQLite for every writer.
	for _, bad := range []string{
		`INSERT INTO Customers(Name) VALUES ('` + strings.Repeat("x", 41) + `')`,
		`INSERT INTO Customers(Visits) VALUES (1)`,
		`INSERT INTO Customers(Name, Visits) VALUES ('B', -1)`,
		`INSERT INTO Customers(Name, Born) VALUES ('B', '2026-02-30')`,
		`INSERT INTO Customers(Name, Seen_At) VALUES ('B', '2026-01-02 03:04:05')`,
		`INSERT INTO Customers(Name, Website) VALUES ('B', 'ftp://x')`,
		`INSERT INTO Customers(Name, Active) VALUES ('B', 2)`,
	} {
		err := f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error { _, err := tx.Exec(f.ctx, bad); return err })
		if err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
	// The validation constraint name maps back to the column.
	err := f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(f.ctx, `INSERT INTO Customers(Name, Visits) VALUES ('B', -1)`)
		return err
	})
	fe := fault.Classify(err)
	visits, _ := tb.ColumnByPhysical("Visits")
	if colID, isRule, ok := ConstraintColumn(fe.Details["constraint"].(string)); !ok || !isRule || colID != visits.ID {
		t.Fatalf("constraint mapping: %v %v", fe.Details, err)
	}
	// OCC safety-net trigger.
	f.exec(`UPDATE Customers SET Visits = 4 WHERE id = 1`)
	if got := f.query(`SELECT _version FROM Customers WHERE id = 1`); got[0][0] != int64(2) {
		t.Fatalf("trigger did not bump _version: %v", got)
	}
	f.exec(`UPDATE Customers SET Visits = 5, _version = _version + 1 WHERE id = 1`)
	if got := f.query(`SELECT _version FROM Customers WHERE id = 1`); got[0][0] != int64(3) {
		t.Fatalf("explicit bump double-counted: %v", got)
	}
}

func TestTransactionalDDLBlockedConversionLeavesFileUnchanged(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Items", Columns: []ColumnSpec{{DisplayName: "Code", Type: ShortText}}}})
	s := f.schema()
	tb, _ := s.TableByPhysical("Items")
	code, _ := tb.ColumnByPhysical("Code")
	f.exec(`INSERT INTO Items(Code) VALUES ('12'), ('abc'), (' 7 '), ('x9'), (NULL)`)
	before := f.fingerprint()

	op := Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: code.ID, Column: &ColumnSpec{DisplayName: "Code", Type: Integer}}
	r, err := f.run(Preview, ApplyOptions{}, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ConversionFailures) != 1 || r.ConversionFailures[0].Count != 2 || len(r.ConversionFailures[0].Samples) != 2 ||
		r.ConversionFailures[0].Samples[0].RowID != 2 || r.ConversionFailures[0].Samples[0].Value != "abc" {
		t.Fatalf("preview: %+v", r)
	}
	if !r.RestorePointRequired || len(r.Breaking) == 0 {
		t.Fatalf("preview flags: %+v", r)
	}
	if after := f.fingerprint(); after != before {
		t.Fatal("preview changed the file")
	}
	_, err = f.run(Apply, ApplyOptions{}, op)
	if fe := fault.Classify(err); fe.Code != fault.SchemaChangeBlocked || fe.Details["report"] == nil {
		t.Fatalf("apply not blocked: %v", err)
	}
	if after := f.fingerprint(); after != before {
		t.Fatal("blocked apply changed the file")
	}
	// With force the convertible rows convert and the rest become NULL.
	if _, err := f.run(Apply, ApplyOptions{Force: true}, op); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(f.query(`SELECT id, Code, typeof(Code) FROM Items ORDER BY id`)); got != "[[1 12 integer] [2 <nil> null] [3 7 integer] [4 <nil> null] [5 <nil> null]]" {
		t.Fatalf("%s", got)
	}
}

func TestRestorePointHookOnlyForDestructiveApply(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.e.RestorePoint = func(context.Context, *workspace.Lease) error { calls++; return nil }
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "A", Columns: []ColumnSpec{{DisplayName: "X", Type: Integer}}}})
	tb, _ := f.schema().TableByPhysical("A")
	if _, err := f.run(Preview, ApplyOptions{}, Op{Kind: OpDropTable, TableID: tb.ID}); err != nil || calls != 0 {
		t.Fatalf("preview took restore point: %d %v", calls, err)
	}
	f.apply(Op{Kind: OpDropTable, TableID: tb.ID})
	if calls != 1 {
		t.Fatalf("restore point calls = %d", calls)
	}
	f.e.RestorePoint = func(context.Context, *workspace.Lease) error { return errors.New("disk full") }
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "B"}})
	tb, _ = f.schema().TableByPhysical("B")
	if _, err := f.run(Apply, ApplyOptions{}, Op{Kind: OpDropTable, TableID: tb.ID}); err == nil {
		t.Fatal("drop proceeded without a restore point")
	}
}

func TestRenameColumnBreaksSavedQuery(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "People", Columns: []ColumnSpec{{DisplayName: "Forename", Type: ShortText}}}})
	tb, _ := f.schema().TableByPhysical("People")
	first, _ := tb.ColumnByPhysical("Forename")
	f.exec(`INSERT INTO _sys_queries(id, name, kind, sql_text, updated_at, updated_by) VALUES (?, 'By first', 'sql', 'SELECT "First" FROM "People"', ?, 'u')`,
		ids.New(), sqlitex.Now())
	op := Op{Kind: OpRenameColumn, TableID: tb.ID, ColumnID: first.ID, PhysicalName: "GivenName"}
	r, err := f.run(Preview, ApplyOptions{}, op)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Breaking) != 1 || r.Breaking[0].Kind != "query_invalid" || !strings.Contains(r.Breaking[0].Message, "By first") {
		t.Fatalf("%+v", r)
	}
	if _, err := f.run(Apply, ApplyOptions{}, op); fault.Classify(err).Code != fault.SchemaChangeBlocked {
		t.Fatalf("apply not blocked: %v", err)
	}
	if _, err := f.run(Apply, ApplyOptions{Force: true}, op); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.schema().Tables[0].ColumnByPhysical("GivenName"); !ok {
		t.Fatal("rename not applied")
	}
}

func TestRebuildPreservesDataSequenceAndConstraints(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "T", Columns: []ColumnSpec{
		{DisplayName: "A", Type: ShortText}, {DisplayName: "B", Type: Integer}}}})
	tb, _ := f.schema().TableByPhysical("T")
	a, _ := tb.ColumnByPhysical("A")
	f.exec(`INSERT INTO T(A, B) VALUES ('x', 1), ('y', NULL), ('z', 3)`)
	f.exec(`DELETE FROM T WHERE id = 3`)
	// Making A required with a validation rule forces a rebuild.
	f.apply(Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: a.ID, Column: &ColumnSpec{DisplayName: "A", Type: ShortText, Required: true,
		ValidationSQL: "length(A) = 1"}})
	f.exec(`INSERT INTO T(A) VALUES ('w')`)
	if got := fmt.Sprint(f.query(`SELECT id, A, B FROM T ORDER BY id`)); got != "[[1 x 1] [2 y <nil>] [4 w <nil>]]" {
		t.Fatalf("data/sequence after rebuild: %s", got)
	}
	// Required on a column with NULLs blocks (violations), even with force.
	b, _ := tb.ColumnByPhysical("B")
	r, err := f.run(Apply, ApplyOptions{Force: true}, Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: b.ID,
		Column: &ColumnSpec{DisplayName: "B", Type: Integer, Required: true}})
	if fault.Classify(err).Code != fault.SchemaChangeBlocked {
		t.Fatalf("required with NULLs not blocked: %+v %v", r, err)
	}
	if len(f.query(`SELECT 1 FROM T`)) != 3 {
		t.Fatal("rows lost")
	}
	// Reconcile is clean.
	if drift, err := Reconcile(f.ctx, mustReader(f), f.schema()); err != nil || len(drift) > 0 {
		t.Fatalf("%v %v", drift, err)
	}
}

func mustReader(f *fixture) sqlitex.Querier {
	var conn *sqlitex.Conn
	_ = f.l.ReadConn(f.ctx, func(c *sqlitex.Conn) error { conn = c; return nil })
	return conn
}

func TestLookupRelationshipAndFKCheck(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Customers", Columns: []ColumnSpec{{DisplayName: "Name", Type: ShortText}}}})
	cust, _ := f.schema().TableByPhysical("Customers")
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Orders", Columns: []ColumnSpec{
		{DisplayName: "Customer", Type: Lookup, Lookup: &LookupSpec{ParentTableID: cust.ID}}}}})
	s := f.schema()
	if len(s.Relationships) != 1 || s.Relationships[0].OnDelete != Restrict {
		t.Fatalf("%+v", s.Relationships)
	}
	f.exec(`INSERT INTO Customers(Name) VALUES ('Ada')`)
	f.exec(`INSERT INTO Orders(Customer) VALUES (1)`)
	err := f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error { _, err := tx.Exec(f.ctx, `INSERT INTO Orders(Customer) VALUES (99)`); return err })
	if fault.Classify(err).Code != fault.LinkedRecords {
		t.Fatalf("FK not enforced: %v", err)
	}
	err = f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error { _, err := tx.Exec(f.ctx, `DELETE FROM Customers WHERE id = 1`); return err })
	if fault.Classify(err).Code != fault.LinkedRecords {
		t.Fatalf("RESTRICT not enforced: %v", err)
	}
	// Dropping the parent is refused while the lookup exists.
	if _, err := f.run(Preview, ApplyOptions{}, Op{Kind: OpDropTable, TableID: cust.ID}); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("parent drop allowed: %v", err)
	}
	// A relationship over an orphaned integer column is reported by foreign_key_check.
	f.apply(Op{Kind: OpAddColumn, TableID: s.Tables[1].ID, Column: &ColumnSpec{DisplayName: "Ref", Type: Integer}})
	f.exec(`UPDATE Orders SET Ref = 42`)
	orders, _ := f.schema().TableByPhysical("Orders")
	ref, _ := orders.ColumnByPhysical("Ref")
	r, err := f.run(Preview, ApplyOptions{}, Op{Kind: OpCreateRelationship, Relationship: &RelationshipSpec{Name: "Orders ref",
		ParentTableID: cust.ID, ChildTableID: orders.ID, ChildColumnID: ref.ID}})
	if err != nil || len(r.Breaking) == 0 || r.Breaking[0].Kind != "fk_violation" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestExpressionInjectionRejected(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "T", Columns: []ColumnSpec{{DisplayName: "A", Type: Integer}}}})
	tb, _ := f.schema().TableByPhysical("T")
	before := f.fingerprint()
	bad := []ColumnSpec{
		{DisplayName: "V", Type: Integer, ValidationSQL: "1); DROP TABLE T; --"},
		{DisplayName: "V", Type: Integer, ValidationSQL: "V > 0) , CHECK (1"},
		{DisplayName: "V", Type: Integer, ValidationSQL: "V IN (SELECT id FROM _sys_tables)"},
		{DisplayName: "V", Type: Integer, ValidationSQL: "V > 0 /* x */"},
		{DisplayName: "V", Type: Integer, DefaultSQL: "(SELECT 1)"},
		{DisplayName: "V", Type: Integer, DefaultSQL: "random()"},
		{DisplayName: "V", Type: Calculated, Options: TypeOptions{Expression: "load_extension('x')"}},
		{DisplayName: "V", Type: Calculated, Options: TypeOptions{Expression: "abidata_convert('text','integer',A)"}},
		{DisplayName: "V", Type: Calculated, Options: TypeOptions{Expression: "uuid()"}},
		{DisplayName: "V", Type: Integer, ValidationSQL: ":p > 1"},
		{DisplayName: "V", Type: ShortText, PhysicalName: `x"y`},
		{DisplayName: "V", Type: ShortText, PhysicalName: "select"},
		{DisplayName: "V", Type: ShortText, PhysicalName: "_sys_x"},
		{DisplayName: "V", Type: ShortText, PhysicalName: "_version"},
	}
	for _, spec := range bad {
		spec := spec
		_, err := f.run(Apply, ApplyOptions{Force: true}, Op{Kind: OpAddColumn, TableID: tb.ID, Column: &spec})
		if err == nil {
			t.Errorf("accepted %+v", spec)
		}
	}
	if f.fingerprint() != before {
		t.Fatal("a rejected change modified the file")
	}
	// A legitimate expression set works.
	f.apply(Op{Kind: OpAddColumn, TableID: tb.ID, Column: &ColumnSpec{DisplayName: "Doubled", Type: Calculated,
		Options: TypeOptions{Expression: "coalesce(A, 0) * 2", ResultType: "integer"}}})
}

func TestSchemaVersionOptimisticLock(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "A"}})
	_, err := f.run(Apply, ApplyOptions{BaseVersion: 99}, Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "B"}})
	if fault.Classify(err).Code != fault.ConcurrentModification {
		t.Fatalf("stale design accepted: %v", err)
	}
}

// TestCrashMidRebuild kills a child process inside a rebuild transaction and
// verifies the workspace reopens unchanged with integrity_check ok.
func TestCrashMidRebuild(t *testing.T) {
	if dir := os.Getenv("ABIDATA_CRASH_DIR"); dir != "" {
		crashChild(t, dir, ids.WorkspaceID(os.Getenv("ABIDATA_CRASH_WS")))
		return
	}
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "T", Columns: []ColumnSpec{{DisplayName: "A", Type: ShortText}}}})
	for i := range 200 {
		f.exec(`INSERT INTO T(A) VALUES (?)`, fmt.Sprint(i))
	}
	before := f.fingerprint()
	f.l.Release()
	if err := f.m.Shutdown(f.ctx); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashMidRebuild$")
	cmd.Env = append(os.Environ(), "ABIDATA_CRASH_DIR="+f.dir, "ABIDATA_CRASH_WS="+string(f.id))
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 137 {
		t.Fatalf("child did not crash as planned: %v\n%s", err, out)
	}

	g := openFixture(t, f.dir, f.id)
	if g.fingerprint() != before {
		t.Fatal("schema/data differ after crash mid-rebuild")
	}
	if got := g.query(`PRAGMA integrity_check`); fmt.Sprint(got) != "[[ok]]" {
		t.Fatalf("integrity: %v", got)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "workspaces")); err != nil {
		t.Fatal(err)
	}
}

func crashChild(t *testing.T, dir string, id ids.WorkspaceID) {
	f := openFixture(t, dir, id)
	tb, _ := f.schema().TableByPhysical("T")
	a, _ := tb.ColumnByPhysical("A")
	f.e.hook = func(step string) {
		if step == "op" {
			os.Exit(137) // the rebuild has run; the transaction is not committed
		}
	}
	_, _ = f.run(Apply, ApplyOptions{Force: true}, Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: a.ID,
		Column: &ColumnSpec{DisplayName: "A", Type: Integer}})
	t.Fatal("unreachable: hook should have exited")
}

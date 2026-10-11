package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	_ "abidata/internal/script/modules/db"
	// Production always links the decimal module, which makes currency a
	// decimal value; without it currency would fall back to a plain string.
	_ "abidata/internal/script/modules/decimal"
	_ "abidata/internal/script/modules/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

type fx struct {
	ctx context.Context
	l   *workspace.Lease
	rn  *script.Runner
	hd  *host.Data
}

func setup(t *testing.T) *fx {
	t.Helper()
	ctx := context.Background()
	m, err := workspace.NewManager(workspace.Config{DataDir: t.TempDir(), Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	id := ids.NewWorkspaceID()
	if err := m.Create(ctx, id); err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Items", TrackHistory: true,
		Columns: []catalog.ColumnSpec{
			{DisplayName: "Name", Type: catalog.ShortText, Required: true},
			{DisplayName: "Qty", Type: catalog.Integer, ValidationSQL: "Qty >= 0", ValidationText: "Qty cannot be negative."},
			{DisplayName: "Price", Type: catalog.Currency},
			{DisplayName: "Note", Type: catalog.LongText},
		}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	cache := catalog.NewCache()
	hd := &host.Data{Lease: l, Schemas: cache, Records: &records.Service{Schemas: cache}, CanWrite: true, UserID: "u1"}
	return &fx{ctx: ctx, l: l, rn: script.NewRunner(4, 2), hd: hd}
}

func (f *fx) run(t *testing.T, kind script.Context, src string) ([]any, error) {
	t.Helper()
	p, err := script.Compile(src, "t")
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.rn.Exec(f.ctx, script.ExecRequest{Kind: kind, Workspace: f.l.ID(), Proto: p, Host: f.hd})
	return res.Values, err
}

func code(err error) fault.Code {
	var fe *fault.Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	var se *script.Error
	if errors.As(err, &se) {
		return fault.Code("SCRIPT:" + map[script.ErrorKind]string{script.KindRuntime: "ERROR", script.KindTimeout: "TIMEOUT", script.KindLimit: "LIMIT"}[se.Kind])
	}
	return ""
}

func TestDBQueryExecNullAndBigInts(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.UIEvent, `
		local r = db.exec("INSERT INTO Items(Name, Qty, Note) VALUES (:n, :q, :note)", {n = "a", q = 2, note = db.NULL})
		local row = db.query_one("SELECT Name, Note, 9007199254740993 AS big FROM Items WHERE id = :id", {id = r.last_id})
		return r.changes, row.Name, row.Note == db.NULL, db.is_null(row.Note), db.is_null(nil), row.big, db.query_value("SELECT count(*) FROM Items")`)
	if err != nil {
		t.Fatal(err)
	}
	if vals[0] != int64(1) || vals[1] != "a" || vals[2] != true || vals[3] != true || vals[4] != true || vals[5] != "9007199254740993" || vals[6] != int64(1) {
		t.Fatalf("%#v", vals)
	}
}

func TestDBDenials(t *testing.T) {
	f := setup(t)
	for _, src := range []string{
		`db.exec("CREATE TABLE evil(x)")`,
		`db.exec("DROP TABLE Items")`,
		`db.query("SELECT source FROM _sys_scripts")`,
		`db.query("SELECT * FROM pragma_database_list")`,
		`db.exec("ATTACH 'x.db' AS x")`,
		`db.exec("UPDATE _sys_meta SET value = 'x'")`,
		`db.exec("INSERT INTO Items(Name) VALUES ('x'); DROP TABLE Items")`,
	} {
		if _, err := f.run(t, script.UIEvent, src); code(err) != fault.SQLNotAllowed {
			t.Errorf("%s: want SQL_NOT_ALLOWED, got %v", src, err)
		}
	}
	// Reports cannot write; neither can viewers.
	if _, err := f.run(t, script.Report, `db.exec("DELETE FROM Items")`); err == nil {
		t.Fatal("report wrote")
	}
	f.hd.CanWrite = false
	if _, err := f.run(t, script.UIEvent, `db.exec("DELETE FROM Items")`); code(err) != fault.SQLNotAllowed {
		t.Fatalf("viewer wrote: %v", err)
	}
}

func TestTransactionAtomicAndBudget(t *testing.T) {
	f := setup(t)
	_, err := f.run(t, script.Job, `
		db.transaction(function()
			records.insert("Items", {Name = "kept?", Qty = 1})
			db.exec("INSERT INTO Items(Name, Qty) VALUES ('x', -5)")
		end)`)
	if code(err) != fault.ValidationFailed {
		t.Fatalf("want VALIDATION_FAILED, got %v", err)
	}
	vals, _ := f.run(t, script.Job, `return db.count("Items")`)
	if vals[0] != int64(0) {
		t.Fatalf("transaction not rolled back: %v", vals)
	}
	// pcall inside the script can handle a failed transaction.
	vals, err = f.run(t, script.Job, `
		local ok, msg = pcall(db.transaction, function() error("nope") end)
		return ok, msg`)
	if err != nil || vals[0] != false {
		t.Fatalf("%v %v", vals, err)
	}
	// A runaway transaction stops at its TxBlock budget, holding the writer no longer.
	start := time.Now()
	_, err = f.run(t, script.UIEvent, `db.transaction(function() while true do end end)`)
	if code(err) != "SCRIPT:LIMIT" || time.Since(start) > 600*time.Millisecond {
		t.Fatalf("runaway tx: %v after %v", err, time.Since(start))
	}
	// Nested transactions are refused.
	if _, err := f.run(t, script.Job, `db.transaction(function() db.transaction(function() end) end)`); err == nil {
		t.Fatal("nested transaction allowed")
	}
}

func TestRecordsModuleTypedAndErrors(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.UIEvent, `
		local id = records.insert("Items", {Name = "Widget", Qty = 3, Price = "19.99"})
		local v = records.update("Items", id, {Qty = 4})
		local r = records.get("Items", id)
		return id, v, r.Qty, tostring(r.Price), r.Note == db.NULL, records.get("Items", 999) == nil`)
	if err != nil {
		t.Fatal(err)
	}
	if vals[0] != int64(1) || vals[1] != int64(2) || vals[2] != int64(4) || vals[3] != "19.99" || vals[4] != true || vals[5] != true {
		t.Fatalf("%#v", vals)
	}
	// Uncaught host errors surface with their fault code and message.
	_, err = f.run(t, script.UIEvent, `records.update("Items", 1, {Qty = -1})`)
	if code(err) != fault.ValidationFailed || fault.Classify(err).DisplayMessage() != "Qty cannot be negative. Nothing was saved." {
		t.Fatalf("%v", err)
	}
	_, err = f.run(t, script.UIEvent, `records.update("Items", 1, {Qty = 1}, {version = 1})`)
	if code(err) != fault.ConcurrentModification {
		t.Fatalf("stale version: %v", err)
	}
	// Caught errors carry the code in the message.
	vals, _ = f.run(t, script.UIEvent, `local ok, e = pcall(records.insert, "Items", {Qty = 1}) return ok, e`)
	if vals[0] != false || vals[1] != "t:1: REQUIRED_FIELD: \"Name\" is required. Nothing was saved." {
		t.Fatalf("%#v", vals)
	}
	// Row history recorded for script edits through records.
	var n int
	_ = f.l.Read(f.ctx, func(q sqlitex.Querier) error {
		return q.QueryRow(f.ctx, `SELECT count(*) FROM _sys_row_history`).Scan(&n)
	})
	if n != 2 {
		t.Fatalf("history rows = %d", n)
	}
}

func TestDLookupFamily(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.Job, `
		for i = 1, 4 do records.insert("Items", {Name = "n" .. i, Qty = i}) end
		return db.lookup("Items", "Name", "Qty = :q", {q = 3}), db.count("Items"), db.count("Items", "Qty", "Qty > 1"),
			db.sum("Items", "Qty"), db.max("Items", "Qty"), db.lookup("Items", "Name", "Qty = 99") == db.NULL`)
	if err != nil {
		t.Fatal(err)
	}
	if vals[0] != "n3" || vals[1] != int64(4) || vals[2] != int64(3) || vals[3] != int64(10) || vals[4] != int64(4) || vals[5] != true {
		t.Fatalf("%#v", vals)
	}
	for _, src := range []string{`db.lookup("Items", "Name", "1); DROP TABLE Items; --")`, `db.lookup("Nope", "Name")`, `db.sum("Items", "Missing")`,
		`db.lookup("Items", "Name", "id IN (SELECT id FROM _sys_tables)")`} {
		if _, err := f.run(t, script.Job, src); err == nil {
			t.Errorf("%s accepted", src)
		}
	}
}

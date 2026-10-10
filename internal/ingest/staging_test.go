package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	_ "abidata/internal/script/modules/all"
)

var runner = script.NewRunner(4, 2)

func (e *env) lua(kind script.Context, rs *RunStaging, src string) (script.ExecResult, error) {
	e.t.Helper()
	proto, err := script.Compile(src, "t")
	if err != nil {
		e.t.Fatal(err)
	}
	d := &host.Data{Lease: e.l, Schemas: e.cache, Records: &records.Service{Schemas: e.cache}, CanWrite: true}
	if rs != nil {
		d.Staging = rs
	}
	return runner.Exec(context.Background(), script.ExecRequest{Kind: kind, Workspace: e.ws, Proto: proto, Host: d})
}

func (e *env) fresh() *RunStaging {
	rs := e.svc.NewRunStaging(e.l, ids.New())
	e.t.Cleanup(rs.Close)
	return rs
}

func TestStagingModule(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Stock", catalog.ColumnSpec{DisplayName: "SKU", Type: catalog.ShortText}, catalog.ColumnSpec{DisplayName: "Qty", Type: catalog.Integer},
		catalog.ColumnSpec{DisplayName: "Price", Type: catalog.Currency}, catalog.ColumnSpec{DisplayName: "Active", Type: catalog.YesNo})
	e.unique(tb, "SKU")
	runID := ids.New()
	rs := e.svc.NewRunStaging(e.l, runID)
	dbFile := filepath.Join(e.dir, "staging", string(e.ws), "run-"+runID+"-0.db")

	res, err := e.lua(script.Job, rs, `
		local st = staging.create({columns = {"sku", "qty", "price", "active"}})
		st:insert({sku = "A", qty = 1, price = "10.50", active = true})
		st:insert({"B", 2, decimal.new("3"), "no"})
		st:insert({sku = "C", qty = db.NULL, active = false})
		local rows = st:query("SELECT sku, qty FROM data WHERE qty >= :min ORDER BY n", {min = 1})
		local pos = st:query("SELECT count(*) AS c FROM data WHERE sku <> ?", {"A"})
		local nulls = st:query("SELECT qty FROM data WHERE sku = 'C'")
		local r = st:merge_into("Stock", {mode = "append"})
		return st:count(), #rows, rows[2].sku, pos[1].c, db.is_null(nulls[1].qty), r.inserted, r.skipped`)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{int64(3), int64(2), "B", int64(2), true, int64(3), int64(0)}
	for i, w := range want {
		if res.Values[i] != w {
			t.Fatalf("value %d = %#v want %#v (%#v)", i, res.Values[i], w, res.Values)
		}
	}
	if _, err := os.Stat(dbFile); err != nil {
		t.Fatalf("staging file missing during the run: %v", err)
	}
	got := rowsOf(t, e, tb, "SKU", "Qty", "Price", "Active")
	if len(got) != 3 || got[0][2] != int64(105000) || got[1][3] != int64(0) || got[2][1] != nil {
		t.Fatalf("%v", got)
	}

	// upsert by key (names match case-insensitively), then replace.
	res, err = e.lua(script.Job, e.fresh(), `
		local st = staging.create({columns = {"SKU", "Qty"}})
		st:insert({"a", 50}) st:insert({"Z", 9}) st:insert({"Z", 10})
		local r = st:merge_into("stock", {mode = "upsert", key = "sku"})
		return r.inserted, r.updated, r.skipped`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Values[0] != int64(1) || res.Values[1] != int64(2) || res.Values[2] != int64(0) {
		t.Fatalf("%v", res.Values)
	}
	res, err = e.lua(script.Job, e.fresh(), `
		local st = staging.create({columns = {"sku"}})
		st:insert({"only"})
		local r = st:merge_into("Stock", {mode = "replace", chunk = 1})
		return r.inserted`)
	if err != nil || res.Values[0] != int64(1) || e.count(tb) != 1 {
		t.Fatalf("%v %v count %d", err, res.Values, e.count(tb))
	}

	// Rejected rows are reported; all_or_nothing raises.
	res, err = e.lua(script.Job, e.fresh(), `
		local st = staging.create({columns = {"sku", "qty"}})
		st:insert({"fresh1", 1}) st:insert({"new", "x"})
		local r = st:merge_into("Stock")
		return r.inserted, r.skipped, r.errors[1].row, r.errors[1].column, r.errors[1].code`)
	if err != nil || res.Values[0] != int64(1) || res.Values[1] != int64(1) || res.Values[2] != int64(2) || res.Values[3] != "Qty" || res.Values[4] != "BAD_REQUEST" {
		t.Fatalf("%v %v", err, res.Values)
	}
	before := e.count(tb)
	_, err = e.lua(script.Job, e.fresh(), `
		local st = staging.create({columns = {"sku"}})
		st:insert({"fresh"}) st:insert({"only"})
		st:merge_into("Stock", {all_or_nothing = true})`)
	if err == nil || fault.Classify(err).Code != fault.DuplicateValue || e.count(tb) != before {
		t.Fatalf("all_or_nothing: %v (rows %d, was %d)", err, e.count(tb), before)
	}

	// Argument errors.
	for name, src := range map[string]string{
		"unknown column":  `local st = staging.create({columns = {"nope"}}) st:insert({"x"}) st:merge_into("Stock")`,
		"unknown table":   `staging.create({columns = {"a"}}):merge_into("Nope")`,
		"bad column name": `staging.create({columns = {"a b"}})`,
		"repeat column":   `staging.create({columns = {"a", "A"}})`,
		"unknown key":     `staging.create({columns = {"a"}}):insert({b = 1})`,
		"too many values": `staging.create({columns = {"a"}}):insert({1, 2})`,
		"upsert no key":   `staging.create({columns = {"sku"}}):merge_into("Stock", {mode = "upsert"})`,
		"upsert no index": `staging.create({columns = {"qty"}}):merge_into("Stock", {mode = "upsert", key = "qty"})`,
		"bad mode":        `staging.create({columns = {"sku"}}):merge_into("Stock", {mode = "x"})`,
		"bad option":      `staging.create({columns = {"sku"}}):merge_into("Stock", {bogus = 1})`,
		"nested value":    `staging.create({columns = {"a"}}):insert({a = {}})`,
	} {
		if _, err := e.lua(script.Job, e.fresh(), src); err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	// SELECT only: writes, PRAGMA, ATTACH, system tables, multiple statements.
	for _, sql := range []string{
		`DELETE FROM data`, `INSERT INTO data(a) VALUES (1)`, `UPDATE data SET a = 1`, `DROP TABLE data`, `CREATE TABLE x(a)`,
		`PRAGMA table_info(data)`, `PRAGMA journal_mode=WAL`, `ATTACH DATABASE '/tmp/x.db' AS x`, `SELECT * FROM sqlite_master`,
		`SELECT * FROM pragma_table_info('data')`, `SELECT load_extension('x')`, `SELECT 1; DELETE FROM data`, `VACUUM`, `BEGIN`,
		`SELECT readfile('/etc/passwd')`, `SELECT * FROM Stock`,
	} {
		_, err := e.lua(script.Job, e.fresh(), "local st = staging.create({columns = {'a'}}) st:insert({1})\nst:query([["+sql+"]])")
		if err == nil {
			t.Errorf("%q was allowed", sql)
		} else if c := fault.Classify(err).Code; c != fault.SQLNotAllowed && c != fault.BadRequest {
			t.Errorf("%q: %v", sql, err)
		}
	}
	// ... and the staged data is untouched by the attempts.
	res, err = e.lua(script.Job, e.fresh(), `local st = staging.create({columns = {'a'}}) st:insert({1}) pcall(st.query, st, "DELETE FROM data") return st:count(), st:query("SELECT count(*) AS c FROM data")[1].c`)
	if err != nil || res.Values[0] != int64(1) || res.Values[1] != int64(1) {
		t.Fatalf("%v %v", err, res.Values)
	}

	// Unavailable outside jobs and without a binding.
	for _, kind := range []script.Context{script.UIEvent, script.Report} {
		res, err = e.lua(kind, e.fresh(), `return staging == nil`)
		if err != nil || res.Values[0] != true {
			t.Fatalf("%s: %v %v", kind, err, res.Values)
		}
	}
	if _, err := e.lua(script.Job, nil, `staging.create({columns = {"a"}})`); fault.Classify(err).Code != fault.Forbidden {
		t.Fatalf("no binding: %v", err)
	}

	// Handles per run are capped; Close removes every staging file.
	for i := 0; i < MaxRunHandles; i++ {
		if _, err := rs.Create(context.Background(), []string{"a"}); err != nil {
			break
		}
	}
	if _, err := rs.Create(context.Background(), []string{"a"}); fault.Classify(err).Code != fault.QuotaExceeded {
		t.Fatalf("handle cap: %v", err)
	}
	rs.Close()
	left, _ := filepath.Glob(filepath.Join(e.dir, "staging", string(e.ws), "run-"+runID+"-*"))
	if len(left) != 0 {
		t.Fatalf("staging files left after Close: %v", left)
	}
	if _, err := rs.Create(context.Background(), []string{"a"}); err == nil {
		t.Fatal("create after Close")
	}
}

func TestStagingSizeCap(t *testing.T) {
	e := newEnv(t)
	rs := e.svc.NewRunStaging(e.l, ids.New())
	defer rs.Close()
	h, err := rs.Create(context.Background(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.Repeat("x", 1<<20)
	var last error
	for i := 0; i < 300 && last == nil; i++ {
		last = rs.Insert(context.Background(), h, []any{blob})
	}
	if fault.Classify(last).Code != fault.QuotaExceeded {
		t.Fatalf("size cap not enforced: %v", last)
	}
}

package db_test

import (
	"testing"

	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/script"
)

// The builder needs no SQL, so these run unchanged on SQLite and PostgreSQL.
func TestBuilderSelect(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.Job, `
		for i, n in ipairs({"apple", "Banana", "cherry", "date"}) do
			records.insert("Items", {Name = n, Qty = i, Price = tostring(i) .. ".50", Note = (i % 2 == 0) and "even" or db.NULL})
		end
		local all = db.select{from = "Items", order = {"-Qty"}}
		local some = db.select{from = "Items", where = {Qty = {gt = 1, le = 3}}, order = {"Name"}}
		local any = db.select{from = "Items", where = {Name = {"apple", "date"}}, order = {"id"}}
		local empty = db.select{from = "Items", where = {Note = db.NULL}, order = {"id"}}
		local either = db.select{from = "Items", where = {_or = {{Qty = 1}, {Name = {starts = "ch"}}}}, order = {"id"}}
		local page = db.select{from = "Items", order = {"id"}, limit = 2, offset = 1}
		return #all, all[1].Name, tostring(all[1].Price), some[1].Name, some[2].Name, #any, any[2].Name,
			#empty, empty[2].Name, #either, either[2].Name, #page, page[1].Name, all[1]._version`)
	if err != nil {
		t.Fatal(err)
	}
	// Banana sorts before cherry whatever the case: text sorts case-insensitively.
	want := []any{int64(4), "date", "4.5", "Banana", "cherry", int64(2), "date", int64(2), "cherry", int64(2), "cherry", int64(2), "Banana", int64(1)}
	for i := range want {
		if vals[i] != want[i] {
			t.Fatalf("value %d: %#v, want %#v (all %#v)", i+1, vals[i], want[i], vals)
		}
	}
	for _, src := range []string{
		`db.select{from = "Nope"}`,
		`db.select{from = "Items", where = {Missing = 1}}`,
		`db.select{from = "Items", where = {Qty = {around = 1}}}`,
		`db.select{from = "Items", order = {"Missing"}}`,
		`db.select{from = "Items", limit = 0}`,
		`db.select{from = "_sys_meta"}`,
	} {
		if _, err := f.run(t, script.Job, src); err == nil {
			t.Errorf("%s accepted", src)
		}
	}
}

func TestBuilderWrites(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.Job, `
		for i = 1, 5 do records.insert("Items", {Name = "n" .. i, Qty = i}) end
		local u = db.update{table = "Items", set = {Note = "big"}, where = {Qty = {ge = 4}}}
		local d = db.delete{table = "Items", where = {Qty = {lt = 2}}}
		local rows = db.select{from = "Items", where = {Note = "big"}, order = {"id"}}
		return u.changes, d.changes, #rows, rows[1]._version, db.count("Items")`)
	if err != nil {
		t.Fatal(err)
	}
	if vals[0] != int64(2) || vals[1] != int64(1) || vals[2] != int64(2) || vals[3] != int64(2) || vals[4] != int64(4) {
		t.Fatalf("%#v", vals)
	}
	// Each change is in row history, as records.update and records.delete record it.
	var n int
	if err := f.l.Read(f.ctx, func(q dbx.Querier) error {
		return q.QueryRow(f.ctx, `SELECT count(*) FROM _sys_row_history`).Scan(&n)
	}); err != nil || n != 5+2+1 {
		t.Fatalf("history rows = %d %v", n, err)
	}
	// Validation stops the whole update: nothing changes.
	_, err = f.run(t, script.Job, `db.update{table = "Items", set = {Qty = -1}, where = {}}`)
	if code(err) != fault.ValidationFailed {
		t.Fatalf("validation: %v", err)
	}
	if vals, _ := f.run(t, script.Job, `return db.count("Items", "Qty", [["Qty" < 0]])`); vals[0] != int64(0) {
		t.Fatalf("partly updated: %v", vals)
	}
	// Inside a transaction, the builder sees and joins its writes.
	vals, err = f.run(t, script.Job, `
		return db.transaction(function()
			records.insert("Items", {Name = "tx", Qty = 9})
			local seen = #db.select{from = "Items", where = {Name = "tx"}}
			db.update{table = "Items", set = {Qty = 10}, where = {Name = "tx"}}
			return seen, db.select{from = "Items", where = {Name = "tx"}}[1].Qty
		end)`)
	if err != nil || vals[0] != int64(1) || vals[1] != int64(10) {
		t.Fatalf("transaction: %v %v", vals, err)
	}
	for _, src := range []string{
		`db.update{table = "Items", set = {Qty = 1}}`,
		`db.update{table = "Items", set = {}, where = {}}`,
		`db.update{table = "Items", set = {_version = 1}, where = {}}`,
		`db.delete{table = "Items"}`,
	} {
		if _, err := f.run(t, script.Job, src); err == nil {
			t.Errorf("%s accepted", src)
		}
	}
	if _, err := f.run(t, script.Report, `db.delete{table = "Items", where = {}}`); err == nil {
		t.Fatal("report deleted")
	}
}

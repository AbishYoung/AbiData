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

func TestFluentQueryBuilder(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.Job, `
		for i, n in ipairs({"apple", "Banana", "cherry", "date"}) do
			records.insert("Items", {Name = n, Qty = i, Note = (i % 2 == 0) and "even" or db.NULL})
		end

		-- where with operator & order
		local q1 = db.from("Items"):where("Qty", ">", 1):where("Qty", "<=", 3):order("Name"):all()
		-- first
		local first_row = db.from("Items"):where("Name", "cherry"):first()
		-- count & exists
		local cnt = db.from("Items"):where("Qty", ">=", 2):count()
		local ex1 = db.from("Items"):where("Name", "apple"):exists()
		local ex2 = db.from("Items"):where("Name", "mango"):exists()
		-- where_in & where_not_in
		local in_rows = db.from("Items"):where_in("Name", {"apple", "date"}):order("id"):pluck("Name")
		local not_in_cnt = db.from("Items"):where_not_in("Name", {"apple", "date"}):count()
		-- where_null & where_not_null
		local null_cnt = db.from("Items"):where_null("Note"):count()
		local not_null_cnt = db.from("Items"):where_not_null("Note"):count()
		-- where_between
		local between_names = db.from("Items"):where_between("Qty", 2, 3):order("Qty"):pluck("Name")
		-- where_not
		local not_cnt = db.from("Items"):where_not("Name", "apple"):count()
		-- aggregations: sum, avg, min, max
		local s = db.from("Items"):sum("Qty")
		local a = db.from("Items"):avg("Qty")
		local mn = db.from("Items"):min("Qty")
		local mx = db.from("Items"):max("Qty")

		-- db.scalar & db.pluck
		local scalar_val = db.scalar([[SELECT count(*) FROM "Items"]])
		local plucked_names = db.pluck([[SELECT "Name" FROM "Items" ORDER BY id]])

		return #q1, q1[1].Name, q1[2].Name, first_row.Name, cnt, ex1, ex2, #in_rows, in_rows[1], in_rows[2],
			not_in_cnt, null_cnt, not_null_cnt, #between_names, between_names[1], not_cnt, s, a, mn, mx,
			scalar_val, #plucked_names, plucked_names[1]
	`)
	if err != nil {
		t.Fatal(err)
	}

	want := []any{
		int64(2), "Banana", "cherry", "cherry", int64(3), true, false, int64(2), "apple", "date",
		int64(2), int64(2), int64(2), int64(2), "Banana", int64(3), int64(10), 2.5, int64(1), int64(4),
		int64(4), int64(4), "apple",
	}
	for i := range want {
		if vals[i] != want[i] {
			t.Fatalf("idx %d: got %#v, want %#v (all: %#v)", i+1, vals[i], want[i], vals)
		}
	}
}

func TestRecordsModuleExtended(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.Job, `
		-- insert_many
		local ids = records.insert_many("Items", {
			{Name = "item1", Qty = 10},
			{Name = "item2", Qty = 20},
			{Name = "item3", Qty = 30},
		})

		-- find_one
		local row = records.find_one("Items", {Name = "item2"})
		local missing = records.find_one("Items", {Name = "nope"})

		-- count & exists
		local c = records.count("Items", {Qty = {ge = 20}})
		local ex1 = records.exists("Items", {Name = "item1"})
		local ex2 = records.exists("Items", {Name = "item99"})

		-- upsert: existing
		local up_id = records.upsert("Items", {Name = "item2"}, {Qty = 25})
		local up_row = records.get("Items", up_id)

		-- upsert: new
		local new_id = records.upsert("Items", {Name = "item4"}, {Qty = 40})
		local new_row = records.get("Items", new_id)

		-- update_many
		local ures = records.update_many("Items", {Qty = {le = 25}}, {Note = "low"})

		return #ids, row.Qty, missing == nil, c, ex1, ex2, up_id == ids[2], up_row.Qty, new_row.Name, new_row.Qty, ures.changes
	`)
	if err != nil {
		t.Fatal(err)
	}

	want := []any{
		int64(3), int64(20), true, int64(2), true, false, true, int64(25), "item4", int64(40), int64(2),
	}
	for i := range want {
		if vals[i] != want[i] {
			t.Fatalf("idx %d: got %#v, want %#v (all: %#v)", i+1, vals[i], want[i], vals)
		}
	}
}

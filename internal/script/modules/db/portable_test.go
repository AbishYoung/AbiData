package db_test

import (
	"testing"

	"abidata/internal/fault"
	"abidata/internal/script"
	"abidata/internal/workspace/wstest"
)

// The db module with SQL both backends read alike (quoted names), run on
// whichever ABIDATA_TEST_DB selects (ADR 0012 Phase 3).
func TestPortableQueryExec(t *testing.T) {
	f := setup(t)
	vals, err := f.run(t, script.Job, `
		local r = db.exec([[INSERT INTO "Items"("Name", "Qty", "Note") VALUES (:n, :q, :note)]], {n = "a", q = 2, note = db.NULL})
		db.exec([[INSERT INTO "Items"("Name", "Qty") VALUES ('b', 5)]])
		local u = db.exec([[UPDATE "Items" SET "Qty" = "Qty" + 1 WHERE "Qty" < :max]], {max = 10})
		local row = db.query_one([[SELECT "Name", "Note", 9007199254740993 AS big FROM "Items" WHERE id = :id]], {id = r.last_id})
		local names = {}
		for it in db.iter([[SELECT "Name" FROM "Items" ORDER BY id]]) do names[#names + 1] = it.Name end
		return r.changes, r.last_id, u.changes, row.Name, row.Note == db.NULL, row.big,
			db.query_value([[SELECT count(*) FROM "Items"]]), table.concat(names, ","),
			db.lookup("Items", "Name", [["Qty" = :q]], {q = 6}), db.count("Items", "Qty", [["Qty" > 3]]), db.sum("Items", "Qty")`)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{int64(1), int64(1), int64(2), "a", true, "9007199254740993", int64(2), "a,b", "b", int64(1), int64(9)}
	for i := range want {
		if vals[i] != want[i] {
			t.Fatalf("value %d: %#v, want %#v (all %#v)", i+1, vals[i], want[i], vals)
		}
	}
	if vals, err := f.run(t, script.UIEvent, `return db.exec([[DELETE FROM "Items" WHERE "Name" = 'a']]).changes`); err != nil || vals[0] != int64(1) {
		t.Fatalf("delete: %v %v", vals, err)
	}
}

func TestPortableDenials(t *testing.T) {
	f := setup(t)
	if _, err := f.run(t, script.UIEvent, `db.exec([[INSERT INTO "Items"("Name", "Qty") VALUES ('a', 1)]])`); err != nil {
		t.Fatal(err)
	}
	denied := []string{
		`db.query([[SELECT source FROM _sys_scripts]])`,
		`db.query_value([[SELECT count(*) FROM _sys_meta]])`,
		`db.exec([[CREATE TABLE "Evil"(x integer)]])`,
		`db.exec([[DROP TABLE "Items"]])`,
		`db.exec([[UPDATE _sys_meta SET value = 'x']])`,
		`db.exec([[DELETE FROM "Items"; DELETE FROM "Items"]])`,
		`db.exec([[UPDATE "Items" SET "_version" = "_version" + 5]])`,
		`db.exec([[INSERT INTO "Items"("Name", "_version") VALUES ('v', 9)]])`,
		`db.exec([[UPDATE "Items" SET "Name" = (SELECT source FROM _sys_scripts LIMIT 1)]])`,
		`db.query([[DELETE FROM "Items"]])`,
		`db.lookup("Items", "Name", [[EXISTS (SELECT 1 FROM _sys_tables)]])`,
	}
	if wstest.Postgres() {
		denied = append(denied,
			`db.query([[SELECT set_config('role', 'postgres', true)]])`,
			`db.exec([[UPDATE "Items" SET "Name" = set_config('role', 'postgres', true)]])`,
			`db.lookup("Items", "Name", [[set_config('role', 'postgres', true) IS NOT NULL]])`,
			`db.query([[SELECT pg_read_file('/etc/passwd')]])`)
	}
	for _, src := range denied {
		if _, err := f.run(t, script.UIEvent, src); code(err) != fault.SQLNotAllowed {
			t.Errorf("%s: want SQL_NOT_ALLOWED, got %v", src, err)
		}
	}
	if _, err := f.run(t, script.UIEvent, `db.exec([[SELECT 1]])`); code(err) != fault.BadRequest {
		t.Errorf("exec of a query: %v", err)
	}
	if _, err := f.run(t, script.UIEvent, `db.exec([[INSERT INTO "Items"("Name", "Qty") VALUES ('x', -5)]])`); code(err) != fault.ValidationFailed {
		t.Errorf("validation: %v", err)
	}
	vals, err := f.run(t, script.UIEvent, `return db.query_value([[SELECT "_version" FROM "Items"]]), db.count("Items")`)
	if err != nil || vals[0] != int64(1) || vals[1] != int64(1) {
		t.Fatalf("after denials: %v %v", vals, err)
	}
	// Setting _version to its own value: SQLite's authorizer refuses it;
	// PostgreSQL's trigger cannot tell it from an UPDATE that leaves it alone,
	// and bumps it, as any update does.
	vals, err = f.run(t, script.UIEvent, `db.exec([[UPDATE "Items" SET "_version" = 1]]) return db.query_value([[SELECT "_version" FROM "Items"]])`)
	if wstest.Postgres() && (err != nil || vals[0] != int64(2)) || !wstest.Postgres() && code(err) != fault.SQLNotAllowed {
		t.Fatalf("_version = 1: %v %v", vals, err)
	}
}

func TestPortableTransaction(t *testing.T) {
	f := setup(t)
	// records.* and generated lookups run inside the transaction on both.
	vals, err := f.run(t, script.Job, `
		return db.transaction(function()
			records.insert("Items", {Name = "a", Qty = 1})
			return db.count("Items"), db.sum("Items", "Qty")
		end)`)
	if err != nil || vals[0] != int64(1) || vals[1] != int64(1) {
		t.Fatalf("%v %v", vals, err)
	}
	_, err = f.run(t, script.Job, `db.transaction(function() records.insert("Items", {Name = "b", Qty = 1}); error("nope") end)`)
	if err == nil {
		t.Fatal("transaction did not fail")
	}
	if vals, _ := f.run(t, script.Job, `return db.count("Items")`); vals[0] != int64(1) {
		t.Fatalf("not rolled back: %v", vals)
	}
	// SQL text inside a transaction: SQLite runs it under the authorizer;
	// PostgreSQL cannot run it on the transaction's connection and refuses.
	for _, src := range []string{
		`db.transaction(function() db.exec([[UPDATE "Items" SET "Qty" = 2]]) end)`,
		`db.transaction(function() return db.query([[SELECT * FROM "Items"]]) end)`,
		`db.transaction(function() return db.lookup("Items", "Name", [["Qty" = 1]]) end)`,
	} {
		_, err := f.run(t, script.Job, src)
		if wstest.Postgres() && code(err) != fault.SQLNotAllowed || !wstest.Postgres() && err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

func TestPortableRecursiveCTEInterrupted(t *testing.T) {
	f := setup(t)
	_, err := f.run(t, script.UIEvent, `return db.query_value("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c) SELECT count(*) FROM c")`)
	if err == nil {
		t.Fatal("not interrupted")
	}
}

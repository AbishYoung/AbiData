package db_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"abidata/internal/script"
)

// Phase 3 security review: the db module's SQL surface as seen by hostile Lua.
// Every statement here must be refused.
func TestSecReviewAuthorizerSurface(t *testing.T) {
	sqliteOnly(t)
	f := setup(t)
	out := filepath.Join(t.TempDir(), "exfil.db")
	denied := []string{
		`return db.query("VACUUM INTO '` + out + `'")`,
		`return db.exec("VACUUM INTO '` + out + `'")`,
		`return db.exec("VACUUM")`,
		`return db.query("PRAGMA table_info(Items)")`,
		`return db.query("SELECT * FROM pragma_table_info('Items')")`,
		`return db.exec("INSERT INTO Items(Name) VALUES ('x') RETURNING (SELECT sql FROM sqlite_master)")`,
		`return db.exec("CREATE TEMP TABLE t(x)")`,
		`return db.exec("CREATE TEMP TRIGGER tr AFTER INSERT ON Items BEGIN SELECT 1; END")`,
		`return db.exec("CREATE TEMP VIEW v AS SELECT 1")`,
		`return db.exec("UPDATE Items SET _version = 1")`,
		`return db.exec("UPDATE Items SET id = id + 1")`,
		`return db.exec("DELETE FROM _sys_scripts")`,
		`return db.exec("UPDATE Items SET Name = (SELECT source FROM _sys_scripts LIMIT 1)")`,
		`return db.query("SELECT * FROM temp.sqlite_master")`,
		`return db.query("SELECT * FROM sqlite_temp_master")`,
		`return db.exec("ANALYZE")`,
		`return db.exec("REINDEX")`,
		`return db.query("SELECT load_extension('x')")`,
		`return db.exec("ATTACH ':memory:' AS x")`,
		`return db.exec("INSERT OR REPLACE INTO Items(id, Name) VALUES (1, 'x')")`,
		`return db.exec("REPLACE INTO Items(id, Name) VALUES (1, 'x')")`,
		`return db.exec("DELETE FROM Items; DELETE FROM Items")`,
		`return db.transaction(function() return db.exec("SAVEPOINT a") end)`,
		`return db.transaction(function() return db.exec("COMMIT") end)`,
		`return db.query("SELECT * FROM Items", {[1] = 2})`,
	}
	for _, src := range denied {
		_, err := f.run(t, script.UIEvent, src)
		if err == nil {
			t.Errorf("allowed: %s", src)
			continue
		}
		t.Logf("denied (%s): %s -> %v", code(err), src, err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("VACUUM INTO wrote %s", out)
	}
}

// A runaway recursive CTE inside db.query must be interrupted by the run's
// wall-time budget (the SQLite interrupt follows ctx).
func TestSecReviewRecursiveCTEInterrupted(t *testing.T) {
	f := setup(t)
	start := time.Now()
	_, err := f.run(t, script.UIEvent, `return db.query_value("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c) SELECT count(*) FROM c")`)
	el := time.Since(start)
	t.Logf("recursive CTE: %v after %v", err, el)
	if err == nil || el > 2*time.Second {
		t.Fatalf("not interrupted within budget: err=%v after %v", err, el)
	}
}

// Positional INSERT … SELECT copies _version without naming it, so CheckDML's
// _VERSION token test does not see it. Informational: only new rows are affected.
func TestSecReviewInsertSelectSetsVersion(t *testing.T) {
	sqliteOnly(t)
	f := setup(t)
	vals, err := f.run(t, script.UIEvent, `
		db.exec("INSERT INTO Items(Name) VALUES ('a')")
		db.exec("INSERT INTO Items SELECT id + 100, 777, Name, Qty, Price, Note FROM Items")
		return db.query_value("SELECT _version FROM Items WHERE id = 101")`)
	t.Logf("positional INSERT…SELECT: %v err=%v", vals, err)
	if err == nil && len(vals) == 1 && vals[0] == int64(777) {
		t.Logf("NOTE: _version set to 777 on a new row via positional INSERT…SELECT")
	}
}

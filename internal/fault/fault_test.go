package fault

import (
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3"
)

// Regression: ON DELETE RESTRICT surfaces as SQLITE_CONSTRAINT_TRIGGER.
func TestClassifyRestrictIsLinkedRecords(t *testing.T) {
	c, err := sqlite3.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE p(id INTEGER PRIMARY KEY);
		CREATE TABLE ch(p INTEGER REFERENCES p(id) ON DELETE RESTRICT); INSERT INTO p VALUES (1); INSERT INTO ch VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if got := Classify(c.Exec(`DELETE FROM p`)).Code; got != LinkedRecords {
		t.Fatalf("RESTRICT → %s", got)
	}
	if got := Classify(c.Exec(`INSERT INTO ch VALUES (9)`)).Code; got != LinkedRecords {
		t.Fatalf("FK insert → %s", got)
	}
}

func TestEveryCodeRegistered(t *testing.T) {
	for _, c := range Codes() {
		e := Lookup(c)
		if e.HTTPStatus == 0 || e.Title == "" || e.Message == "" || e.Recovery == "" {
			t.Errorf("%s incomplete: %+v", c, e)
		}
	}
}

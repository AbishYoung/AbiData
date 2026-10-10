package scripts

import (
	"testing"

	_ "abidata/internal/script/modules/db"
	_ "abidata/internal/script/modules/records"
	_ "abidata/internal/script/modules/ui"
)

func TestCheck(t *testing.T) {
	ms := Check("function x(\n  return 1\nend", "form_module", nil)
	if len(ms) != 1 || ms[0].Severity != "error" || ms[0].Line != 2 {
		t.Fatalf("%+v", ms)
	}
	src := `-- db.nothing in a comment
local s = "records.bogus in a string"
function f()
  db.quer("SELECT 1")
  db.iter("SELECT 1")
  records.get("Customers", 1)
  ui.set("x", {})
  string.format("%d", 1)
end`
	ms = Check(src, "form_module", nil)
	want := map[string]bool{"unknown function db.quer": true, "db.iter is not available in ui_event scripts": true}
	if len(ms) != 2 {
		t.Fatalf("%+v", ms)
	}
	for _, m := range ms {
		if !want[m.Message] || m.Severity != "warning" || m.Line < 4 {
			t.Errorf("unexpected %+v", m)
		}
	}
	ms = Check(`ui.set("x", {})`, "job", nil)
	if len(ms) != 1 || ms[0].Message != `module "ui" is not available in job scripts` {
		t.Fatalf("%+v", ms)
	}
}

package forms

import (
	"testing"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

// The workspace's ui_event_budget_ms quota sets the UI-event wall time.
func TestUIEventBudgetFromQuota(t *testing.T) {
	f := setup(t)
	form, err := f.svc.Put(f.ctx, f.l, "", 0, FormInput{Name: "Spin", SourceKind: "table", SourceID: f.orders.ID,
		ModuleSource: "function on_load() while true do end end",
		Layout:       Layout{Width: 400, Events: map[string]string{"load": "on_load"}}}, "bu")
	if err != nil {
		t.Fatal(err)
	}
	f.svc.UIEventBudgetMS = func(ids.WorkspaceID) int { return 300 }
	start := time.Now()
	_, err = f.svc.Event(f.ctx, f.l, f.editor, form.ID, EventRequest{Event: "load"})
	took := time.Since(start)
	if fault.Classify(err).Code != fault.ScriptTimeout {
		t.Fatalf("err %v", err)
	}
	if took > 900*time.Millisecond {
		t.Fatalf("ran %v under a 300 ms budget (default is 1 s)", took)
	}
}

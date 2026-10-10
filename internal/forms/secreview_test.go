package forms

import (
	"testing"

	"abidata/internal/records"
)

// Phase 3 security review: form-protocol abuse through the service API (what the
// HTTP handlers call). These document that form-level rules are UX, not
// authorization: they pass today and describe current behaviour.

// L1 (fixed; regression): the two-phase confirm was not bound to a
// server-issued continuation: any member could invoke any global function of the form module by sending
// event="confirm" with continuation=<name> and answer=true, skipping the click
// handler's checks. Here the click handler would refuse (no approver), but the
// editor calls do_ship directly and the order is shipped.
func TestSecReviewConfirmContinuationIsCallerChosen(t *testing.T) {
	f := setup(t)
	orderForm, _ := f.createForms(t)
	order, err := f.svc.Records.Insert(f.ctx, f.l, f.orders.ID, map[string]any{col(f.orders, "Customer"): nil}, records.Actor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cont := range []string{"do_ship", "before_update", "do_ship.9999999999.forged"} {
		if _, err := f.svc.Event(f.ctx, f.l, f.editor, orderForm.ID, EventRequest{Event: "confirm", Continuation: cont, Answer: yes(true),
			RecordID: order.ID, RecordVersion: order.Version, Record: order.Fields}); err == nil {
			t.Fatalf("continuation %q was accepted", cont)
		}
	}
	cur, _ := f.svc.Records.Get(f.ctx, f.l, f.orders.ID, order.ID)
	if cur.Fields[col(f.orders, "Status")] == "Shipped" {
		t.Fatal("the order was shipped through a caller-chosen continuation")
	}
}

// Form saves accept any column of the source table (hidden, locked, unbound),
// regardless of the layout; equivalent to the rows API, which editors can call
// directly anyway. The before_update cancel is likewise bypassed via the rows API.
func TestSecReviewSaveAcceptsUnboundAndHiddenColumns(t *testing.T) {
	f := setup(t)
	orderForm, _ := f.createForms(t)
	order, _ := f.svc.Records.Insert(f.ctx, f.l, f.orders.ID, map[string]any{}, records.Actor{}, nil)
	// Approver is Visible=false on the form; editor sets it anyway.
	resp, err := f.svc.Save(f.ctx, f.l, f.editor, orderForm.ID, SaveRequest{RecordID: order.ID, RecordVersion: order.Version,
		Fields: map[string]any{col(f.orders, "Approver"): "self-approved"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("hidden field saved: %v", resp.Saved.Fields[col(f.orders, "Approver")])
	// Rows API bypasses before_update entirely.
	row, err := f.svc.Records.Update(f.ctx, f.l, f.orders.ID, order.ID, resp.Saved.Version,
		map[string]any{col(f.orders, "Status"): "Shipped"}, records.Actor{UserID: "ed"}, nil)
	t.Logf("rows-API update without handlers: %v err=%v", row.Fields[col(f.orders, "Status")], err)
}

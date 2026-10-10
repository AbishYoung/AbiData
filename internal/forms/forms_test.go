package forms

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/db"
	_ "abidata/internal/script/modules/records"
	_ "abidata/internal/script/modules/ui"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

type fx struct {
	ctx                     context.Context
	l                       *workspace.Lease
	svc                     *Service
	cust, orders, lines     *catalog.Table
	editor, viewer, builder Caller
}

func col(t *catalog.Table, phys string) string {
	c, _ := t.ColumnByPhysical(phys)
	return c.ID
}

func setup(t *testing.T) *fx {
	t.Helper()
	ctx := context.Background()
	m, err := workspace.NewManager(workspace.Config{DataDir: t.TempDir(), Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	id := ids.NewWorkspaceID()
	_ = m.Create(ctx, id)
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	cache := catalog.NewCache()
	e := &catalog.Engine{}
	apply := func(ops ...catalog.Op) {
		if _, err := e.Run(ctx, l, ops, catalog.Apply, catalog.ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	apply(catalog.Op{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Customers", Columns: []catalog.ColumnSpec{{DisplayName: "Name", Type: catalog.ShortText}}}})
	sch, _ := cache.Get(ctx, l)
	cust, _ := sch.TableByPhysical("Customers")
	apply(catalog.Op{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Orders", Columns: []catalog.ColumnSpec{
		{DisplayName: "Customer", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: cust.ID}},
		{DisplayName: "Approver", Type: catalog.ShortText},
		{DisplayName: "Status", Type: catalog.ShortText}}}})
	sch, _ = cache.Get(ctx, l)
	orders, _ := sch.TableByPhysical("Orders")
	apply(catalog.Op{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Lines", Columns: []catalog.ColumnSpec{
		{DisplayName: "OrderId", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: orders.ID, OnDelete: catalog.Cascade}},
		{DisplayName: "Qty", Type: catalog.Integer}, {DisplayName: "UnitPrice", Type: catalog.Currency}}}})
	sch, _ = cache.Get(ctx, l)
	lines, _ := sch.TableByPhysical("Lines")
	recSvc := &records.Service{Schemas: cache}
	tz, _ := time.LoadLocation("UTC")
	f := &fx{ctx: ctx, l: l, cust: cust, orders: orders, lines: lines,
		svc:    &Service{Schemas: cache, Records: recSvc, Queries: &query.Service{Schemas: cache}, Scripts: &scripts.Service{}, Runner: script.NewRunner(4, 4)},
		editor: Caller{UserID: "ed", Role: admin.RoleEditor, TZ: tz}, viewer: Caller{UserID: "vi", Role: admin.RoleViewer, TZ: tz},
		builder: Caller{UserID: "bu", Role: admin.RoleBuilder, TZ: tz}}
	return f
}

func yes(b bool) *bool { return &b }

const orderModule = `
local function order_total(id)
  if id == nil then return 0 end
  return db.query_value('SELECT coalesce(sum(Qty * UnitPrice), 0) FROM Lines WHERE OrderId = :id', {id = id})
end

function on_load()
  ui.set("Approver", {visible = true})
end

function before_update()
  local total = order_total(record.id) / 10000
  if total > 5000 and db.is_null(record.Approver) then
    ui.cancel("Orders over 5,000 need an approver.")
    return
  end
  record.Status = "Ready"
end

function after_update()
  notify.toast("Order saved", {level = "success"})
  ui.set("Status", {tone = "success"})
end

function on_ship_click()
  ui.confirm("Ship this order?", "do_ship")
end

function do_ship()
  if event.answer then records.update("Orders", record.id, {Status = "Shipped"}) end
end
`

func (f *fx) createForms(t *testing.T) (orderForm, lineForm Form) {
	t.Helper()
	lineForm, err := f.svc.Put(f.ctx, f.l, "", 0, FormInput{Name: "Order lines", SourceKind: "table", SourceID: f.lines.ID, DefaultView: "continuous",
		Layout: Layout{Width: 600, Sections: Sections{Detail: Section{Height: 28, Controls: []Control{
			{ID: "c1", Name: "Qty", Type: "textbox", W: 80, H: 24, Bind: &Bind{ColumnID: col(f.lines, "Qty")}},
			{ID: "c2", Name: "UnitPrice", Type: "textbox", X: 90, W: 100, H: 24, Bind: &Bind{ColumnID: col(f.lines, "UnitPrice")}},
			{ID: "c3", Name: "LineTotal", Type: "textbox", X: 200, W: 100, H: 24, Calc: &Calc{LuaExpr: "(Qty or 0) * UnitPrice"}},
		}}}}}, "bu")
	if err != nil {
		t.Fatal(err)
	}
	orderForm, err = f.svc.Put(f.ctx, f.l, "", 0, FormInput{Name: "Order", SourceKind: "table", SourceID: f.orders.ID, ModuleSource: orderModule,
		Layout: Layout{Width: 800, Events: map[string]string{"load": "on_load", "before_update": "before_update", "after_update": "after_update"},
			Sections: Sections{Detail: Section{Height: 400, Controls: []Control{
				{ID: "o1", Name: "Customer", Type: "combobox", W: 200, H: 24, Bind: &Bind{ColumnID: col(f.orders, "Customer")},
					Lookup: &Lookup{TableID: f.cust.ID, ValueColumnID: "id", DisplayColumnID: col(f.cust, "Name")}},
				{ID: "o2", Name: "Approver", Type: "textbox", Y: 30, W: 200, H: 24, Visible: yes(false), Bind: &Bind{ColumnID: col(f.orders, "Approver")}},
				{ID: "o3", Name: "Status", Type: "textbox", Y: 60, W: 200, H: 24, Bind: &Bind{ColumnID: col(f.orders, "Status")}},
				{ID: "o4", Name: "Lines", Type: "subform", Y: 100, W: 600, H: 200,
					Subform: &Subform{FormID: lineForm.ID, LinkMasterColumnID: "id", LinkChildColumnID: col(f.lines, "OrderId")}},
				{ID: "o5", Name: "Total", Type: "textbox", Y: 310, W: 200, H: 24,
					Calc: &Calc{LuaExpr: "db.query_value('SELECT coalesce(sum(Qty * UnitPrice), 0) FROM Lines WHERE OrderId = :id', {id = id}) / 10000"}},
				{ID: "o6", Name: "Ship", Type: "button", Y: 340, W: 100, H: 24, Caption: "Ship", Events: map[string]string{"click": "on_ship_click"}},
			}}}}}, "bu")
	if err != nil {
		t.Fatal(err)
	}
	return orderForm, lineForm
}

func TestOrderFormLifecycle(t *testing.T) {
	f := setup(t)
	orderForm, lineForm := f.createForms(t)
	cust, _ := f.svc.Records.Insert(f.ctx, f.l, f.cust.ID, map[string]any{col(f.cust, "Name"): "Ada"}, records.Actor{}, nil)
	order, err := f.svc.Records.Insert(f.ctx, f.l, f.orders.ID, map[string]any{col(f.orders, "Customer"): json.Number("1")}, records.Actor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = cust
	for _, ln := range [][2]string{{"10", "400.00"}, {"5", "250.50"}} {
		if _, err := f.svc.Records.Insert(f.ctx, f.l, f.lines.ID, map[string]any{col(f.lines, "OrderId"): json.Number("1"),
			col(f.lines, "Qty"): json.Number(ln[0]), col(f.lines, "UnitPrice"): ln[1]}, records.Actor{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// load: handler patches a control; calc computes the total (10×400 + 5×250.50 = 5252.5).
	resp, err := f.svc.Event(f.ctx, f.l, f.editor, orderForm.ID, EventRequest{Event: "load", RecordID: order.ID, RecordVersion: order.Version,
		Record: order.Fields})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Patches) != 1 || resp.Patches[0].Control != "Approver" || resp.Patches[0].Props["visible"] != true {
		t.Fatalf("patches %+v", resp.Patches)
	}
	if resp.Calc["o5"] != 5252.5 {
		t.Fatalf("calc %+v errors %+v", resp.Calc, resp.CalcErrors)
	}
	// Subform calc: line total is a decimal.
	lineResp, err := f.svc.Event(f.ctx, f.l, f.editor, lineForm.ID, EventRequest{Event: "current",
		Record: map[string]any{col(f.lines, "Qty"): json.Number("3"), col(f.lines, "UnitPrice"): "2.5000"}})
	if err != nil || fmt.Sprint(lineResp.Calc["c3"]) != "7.5" {
		t.Fatalf("line calc %+v %+v %v", lineResp.Calc, lineResp.CalcErrors, err)
	}
	// Save without approver → cancelled; nothing written.
	resp, err = f.svc.Save(f.ctx, f.l, f.editor, orderForm.ID, SaveRequest{RecordID: order.ID, RecordVersion: order.Version,
		Fields: map[string]any{col(f.orders, "Status"): "Draft"}})
	if err != nil || resp.Cancel == nil || resp.Cancel.Message != "Orders over 5,000 need an approver." || resp.Saved != nil {
		t.Fatalf("expected cancel: %+v %v", resp, err)
	}
	cur, _ := f.svc.Records.Get(f.ctx, f.l, f.orders.ID, order.ID)
	if cur.Version != order.Version {
		t.Fatal("cancelled save wrote the row")
	}
	// With an approver: saved via OCC; before_update set Status; after_update toast + patch.
	resp, err = f.svc.Save(f.ctx, f.l, f.editor, orderForm.ID, SaveRequest{RecordID: order.ID, RecordVersion: order.Version,
		Fields: map[string]any{col(f.orders, "Approver"): "Grace"}, IdempotencyKey: "save-1"})
	if err != nil || resp.Saved == nil || resp.Saved.Fields[col(f.orders, "Status")] != "Ready" {
		t.Fatalf("save: %+v %v", resp, err)
	}
	if len(resp.Notifications) != 1 || resp.Notifications[0].Text != "Order saved" || resp.Calc["o5"] != 5252.5 {
		t.Fatalf("after_update: %+v %+v", resp.Notifications, resp.Calc)
	}
	// Stale version → CONCURRENT_MODIFICATION from the OCC write.
	if _, err := f.svc.Save(f.ctx, f.l, f.editor, orderForm.ID, SaveRequest{RecordID: order.ID, RecordVersion: order.Version,
		Fields: map[string]any{col(f.orders, "Approver"): "X"}}); fault.Classify(err).Code != fault.ConcurrentModification {
		t.Fatalf("stale save: %v", err)
	}
	// Two-phase confirm: click → confirm request; confirm with answer → handler runs.
	saved := *resp.Saved
	resp, err = f.svc.Event(f.ctx, f.l, f.editor, orderForm.ID, EventRequest{Event: "click", Control: "Ship", RecordID: saved.ID, RecordVersion: saved.Version, Record: saved.Fields})
	if err != nil || resp.Confirm == nil || resp.Confirm.Continuation != "do_ship" {
		t.Fatalf("confirm: %+v %v", resp, err)
	}
	if _, err := f.svc.Event(f.ctx, f.l, f.editor, orderForm.ID, EventRequest{Event: "confirm", Continuation: "do_ship", Answer: yes(true),
		RecordID: saved.ID, RecordVersion: saved.Version, Record: saved.Fields}); err != nil {
		t.Fatal(err)
	}
	cur, _ = f.svc.Records.Get(f.ctx, f.l, f.orders.ID, order.ID)
	if cur.Fields[col(f.orders, "Status")] != "Shipped" {
		t.Fatalf("confirm handler did not run: %+v", cur.Fields)
	}
	// Viewers cannot save; unknown continuation names are rejected.
	if _, err := f.svc.Save(f.ctx, f.l, f.viewer, orderForm.ID, SaveRequest{RecordID: 1, RecordVersion: cur.Version}); fault.Classify(err).Code != fault.Forbidden {
		t.Fatalf("viewer saved: %v", err)
	}
	if _, err := f.svc.Event(f.ctx, f.l, f.editor, orderForm.ID, EventRequest{Event: "confirm", Continuation: "os.exit"}); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("bad continuation: %v", err)
	}
}

func TestFormValidationAndSchemaImpact(t *testing.T) {
	f := setup(t)
	if _, err := f.svc.Put(f.ctx, f.l, "", 0, FormInput{Name: "Bad", SourceKind: "table", SourceID: f.orders.ID,
		Layout: Layout{Width: 100, Sections: Sections{Detail: Section{Height: 20, Controls: []Control{
			{ID: "x", Name: "X", Type: "textbox", W: 10, H: 10, Bind: &Bind{ColumnID: col(f.lines, "Qty")}}}}}}}, "bu"); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("binding to another table's column accepted: %v", err)
	}
	if _, err := f.svc.Put(f.ctx, f.l, "", 0, FormInput{Name: "Syntax", SourceKind: "none", ModuleSource: "function x( end",
		Layout: Layout{Width: 100}}, "bu"); fault.Classify(err).Code != fault.ScriptError {
		t.Fatalf("module syntax error accepted: %v", err)
	}
	orderForm, _ := f.createForms(t)
	// Dropping a bound column is reported as breaking for the form.
	e := &catalog.Engine{Validators: []catalog.Validator{SchemaValidator}}
	rep, err := e.Run(f.ctx, f.l, []catalog.Op{{Kind: catalog.OpDropColumn, TableID: f.orders.ID, ColumnID: col(f.orders, "Approver")}}, catalog.Preview, catalog.ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range rep.Breaking {
		found = found || (b.Kind == "form_binding" && b.ID == orderForm.ID)
	}
	if !found {
		t.Fatalf("form binding not reported: %+v", rep.Breaking)
	}
}

// Package demo builds the "Northwind Traders" sample workspace: tables with
// realistic rows, saved queries, forms, reports, a job script and a
// scheduled job. It also carries a biosecurity (invasive species) scenario that
// shows geographic fields and file attachments: suppliers with a map location,
// species, pest sightings with GPS positions and photos, quarantine zones
// (polygons, one with a hole), spatial queries, a sighting form with a map, and
// a report with maps and images. It drives the same services the HTTP handlers use, so
// validation, audit and row history behave exactly as they do for a user.
//
// Content is deterministic (fixed random seed); dates are relative to the
// install time so the data always looks current. No script in the demo uses the
// network.
package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/files"
	"abidata/internal/forms"
	"abidata/internal/ids"
	"abidata/internal/jobs"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/script"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

// WorkspaceName is the name of the sample workspace; Create does nothing when a
// workspace with this name already exists. That includes workspaces created by
// an earlier version: there is no upgrade path, so only new installs (or a
// workspace deleted and recreated with `abidata demo`) get the biosecurity
// scenario. Existing workspaces are never modified.
const WorkspaceName = "Northwind Traders"

// Builder holds the services the demo is built with. Every field is required.
type Builder struct {
	Admin      *admin.Store
	Workspaces *workspace.Manager
	Schemas    *catalog.Cache
	Catalog    *catalog.Engine
	Records    *records.Service
	Queries    *query.Service
	Scripts    *scripts.Service
	Forms      *forms.Service
	Reports    *reports.Service
	Jobs       *jobs.Service
	// Files stores the sample photos (ADR 0011). When nil the biosecurity
	// sightings are created without photos.
	Files *files.Service
	Log   *slog.Logger
	// Now overrides the clock (tests). Zero means time.Now.
	Now func() time.Time
}

// NewBuilder wires a Builder around the server's admin store and workspace
// manager. It keeps its own schema cache and services (a cache miss simply
// reloads from the workspace file, so sharing the server's cache is not
// needed). sched may be nil (the `abidata demo` command): the job index is then
// rebuilt when the server next opens the workspace.
func NewBuilder(adm *admin.Store, wm *workspace.Manager, sched *jobs.Scheduler, fs *files.Service, log *slog.Logger) *Builder {
	schemas := catalog.NewCache()
	rec := &records.Service{Schemas: schemas}
	qs := &query.Service{Schemas: schemas}
	ss := &scripts.Service{}
	runner := script.NewRunner(2, 2)
	return &Builder{
		Admin: adm, Workspaces: wm, Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Files: fs, Log: log,
		Catalog: &catalog.Engine{
			Validators:  []catalog.Validator{query.SchemaValidator, forms.SchemaValidator, reports.SchemaValidator},
			AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) { schemas.Put(l.ID(), s) },
		},
		Forms:   &forms.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Reports: &reports.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Jobs:    &jobs.Service{Scripts: ss, Scheduler: sched},
	}
}

// Result describes what Create did.
type Result struct {
	WorkspaceID ids.WorkspaceID
	Created     bool // false: a workspace with the demo's name already existed
}

// Create builds the demo workspace owned by ownerID. When a workspace named
// "Northwind Traders" already exists it returns that workspace and Created is
// false. If building fails part-way the half-built workspace is retired
// (marked deleted and renamed) so a retry starts clean.
func (b *Builder) Create(ctx context.Context, ownerID string) (Result, error) {
	existing, err := b.Admin.ListWorkspaces(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, w := range existing {
		if strings.EqualFold(w.Name, WorkspaceName) {
			return Result{WorkspaceID: w.ID}, nil
		}
	}
	now := time.Now()
	if b.Now != nil {
		now = b.Now()
	}
	ctx = context.WithoutCancel(ctx)
	w := admin.Workspace{ID: ids.NewWorkspaceID(), Name: WorkspaceName, Status: admin.StatusActive, CreatedAt: now, CreatedBy: ownerID}
	if err := b.Workspaces.Create(ctx, w.ID); err != nil {
		return Result{}, err
	}
	if err := b.Admin.CreateWorkspace(ctx, w, ownerID); err != nil {
		if p, perr := b.Workspaces.PathsFor(w.ID); perr == nil {
			removeFiles(p.DB, p.WAL, p.SHM)
		}
		return Result{}, err
	}
	if err := b.build(ctx, w.ID, ownerID, now); err != nil {
		b.retire(ctx, w.ID)
		return Result{}, fmt.Errorf("build demo workspace: %w", err)
	}
	if err := b.Admin.Audit(ctx, admin.AuditEntry{At: time.Now(), UserID: ownerID, Action: "workspace.create",
		ObjectKind: "workspace", ObjectID: string(w.ID), Details: map[string]any{"name": WorkspaceName, "owner": ownerID, "demo": true}}); err != nil && b.Log != nil {
		b.Log.Warn("audit write failed", "err", err)
	}
	return Result{WorkspaceID: w.ID, Created: true}, nil
}

// retire hides a half-built workspace and frees its name.
func (b *Builder) retire(ctx context.Context, id ids.WorkspaceID) {
	if b.Files != nil {
		if err := b.Files.RemoveBucket(id); err != nil && b.Log != nil {
			b.Log.Warn("remove sample photos of failed demo workspace", "workspace_id", id, "err", err)
		}
	}
	st := admin.StatusDeleted
	name := fmt.Sprintf("%s (failed %s)", WorkspaceName, id)
	if err := b.Admin.UpdateWorkspace(ctx, id, admin.WorkspacePatch{Status: &st, Name: &name}); err != nil && b.Log != nil {
		b.Log.Error("retire failed demo workspace", "workspace_id", id, "err", err)
	}
}

// tbl gives access to a created table's column IDs and physical names by
// display name.
type tbl struct{ t *catalog.Table }

func (t tbl) id() string { return t.t.ID }
func (t tbl) col(display string) string {
	for _, c := range t.t.Columns {
		if c.DisplayName == display {
			return c.ID
		}
	}
	panic("demo: no column " + display + " in " + t.t.DisplayName)
}
func (t tbl) phys(display string) string {
	for _, c := range t.t.Columns {
		if c.DisplayName == display {
			return c.PhysicalName
		}
	}
	panic("demo: no column " + display + " in " + t.t.DisplayName)
}

// q quotes an identifier for SQL.
func q(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func num(n int64) json.Number { return json.Number(strconv.FormatInt(n, 10)) }

// money formats cents as a decimal currency string.
func money(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

// Column and table spec helpers shared by the Northwind and biosecurity parts.

func create(name, desc string, cols ...catalog.ColumnSpec) catalog.Op {
	return catalog.Op{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: name, Description: desc, TrackHistory: true, Columns: cols}}
}

func text(name string) catalog.ColumnSpec {
	return catalog.ColumnSpec{DisplayName: name, Type: catalog.ShortText}
}

func typed(name string, t catalog.FieldType) catalog.ColumnSpec {
	return catalog.ColumnSpec{DisplayName: name, Type: t}
}

func lookup(name string, parent tbl, display string) catalog.ColumnSpec {
	return catalog.ColumnSpec{DisplayName: name, Type: catalog.Lookup,
		Lookup: &catalog.LookupSpec{ParentTableID: parent.id(), DisplayColumnID: parent.col(display)}}
}

func required(c catalog.ColumnSpec) catalog.ColumnSpec { c.Required = true; return c }

func (b *Builder) build(ctx context.Context, id ids.WorkspaceID, owner string, now time.Time) error {
	l, err := b.Workspaces.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer l.Release()
	rnd := rand.New(rand.NewPCG(7, 1998)) //nolint:gosec // deterministic sample data, not security

	apply := func(ops ...catalog.Op) (*catalog.Schema, error) {
		if _, err := b.Catalog.Run(ctx, l, ops, catalog.Apply, catalog.ApplyOptions{UserID: owner}); err != nil {
			return nil, err
		}
		return b.Schemas.Get(ctx, l)
	}
	table := func(sch *catalog.Schema, display string) tbl {
		for i := range sch.Tables {
			if sch.Tables[i].DisplayName == display {
				return tbl{&sch.Tables[i]}
			}
		}
		panic("demo: no table " + display)
	}

	// ---- tables -------------------------------------------------------
	sch, err := apply(
		create("Employees", "The sales team.",
			required(text("Name")), text("Title"), text("Email"), typed("Hire date", catalog.Date)),
		create("Customers", "Companies that buy from Northwind.",
			required(text("Company")), text("Contact"), text("City"), text("Country"), text("Email"), typed("Active", catalog.YesNo)),
		create("Products", "What Northwind sells. Low stock is refreshed by the Refresh low-stock flags job.",
			required(text("Product")), text("Category"), typed("Unit price", catalog.Currency), typed("In stock", catalog.Integer),
			typed("Reorder level", catalog.Integer), typed("Discontinued", catalog.YesNo), typed("Low stock", catalog.YesNo)),
	)
	if err != nil {
		return err
	}
	emp, cust, prod := table(sch, "Employees"), table(sch, "Customers"), table(sch, "Products")

	sch, err = apply(create("Orders", "One row per sales order. Each order has lines in Order lines.",
		required(text("Order ref")), lookup("Customer", cust, "Company"), lookup("Employee", emp, "Name"),
		typed("Order date", catalog.Date), text("Status"), typed("Total", catalog.Currency), typed("Notes", catalog.LongText)))
	if err != nil {
		return err
	}
	ord := table(sch, "Orders")

	lineTotal := catalog.ColumnSpec{DisplayName: "Line total", Type: catalog.Calculated,
		Options: catalog.TypeOptions{Expression: "Quantity * Unit_price", ResultType: "currency"}}
	sch, err = apply(create("Order lines", "The products on each order.",
		lookup("Sales order", ord, "Order ref"), lookup("Product", prod, "Product"),
		typed("Quantity", catalog.Integer), typed("Unit price", catalog.Currency), lineTotal))
	if err != nil {
		return err
	}
	lin := table(sch, "Order lines")
	// Re-read the other tables: their columns are unchanged but pointers into
	// the previous snapshot would be stale.
	emp, cust, prod, ord = table(sch, "Employees"), table(sch, "Customers"), table(sch, "Products"), table(sch, "Orders")

	actor := records.Actor{UserID: owner}
	insert := func(t tbl, rows []map[string]any) ([]int64, error) {
		var out []int64
		for len(rows) > 0 {
			n := min(len(rows), records.MaxBatch)
			ops := make([]records.BatchOp, n)
			for i := range ops {
				ops[i] = records.BatchOp{Op: "insert", Fields: rows[i]}
			}
			res, err := b.Records.Batch(ctx, l, t.id(), ops, actor, nil)
			if err != nil {
				return nil, fmt.Errorf("insert into %s: %w", t.t.DisplayName, err)
			}
			for _, r := range res {
				out = append(out, r.ID)
			}
			rows = rows[n:]
		}
		return out, nil
	}
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// ---- rows ---------------------------------------------------------
	var rows []map[string]any
	for i, e := range employees {
		rows = append(rows, map[string]any{emp.col("Name"): e.Name, emp.col("Title"): e.Title, emp.col("Email"): e.Email,
			emp.col("Hire date"): day(today.AddDate(-(1 + i), -(i * 3), 0))})
	}
	empIDs, err := insert(emp, rows)
	if err != nil {
		return err
	}

	rows = nil
	for i, c := range customers {
		first := strings.ToLower(strings.Fields(c.Contact)[0])
		domain := strings.ToLower(strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
				return r
			}
			return -1
		}, strings.Fields(c.Company)[0]))
		rows = append(rows, map[string]any{cust.col("Company"): c.Company, cust.col("Contact"): c.Contact, cust.col("City"): c.City,
			cust.col("Country"): c.Country, cust.col("Email"): first + "@" + domain + ".example", cust.col("Active"): i%9 != 4})
	}
	custIDs, err := insert(cust, rows)
	if err != nil {
		return err
	}

	rows = nil
	type stock struct{ inStock, reorder int64 }
	stocks := make([]stock, len(products))
	for i, p := range products {
		st := stock{inStock: int64(rnd.IntN(120)), reorder: int64(10 + rnd.IntN(3)*5)}
		if i == 2 || i == 9 || i == 13 { // make a few clearly low
			st.inStock = int64(rnd.IntN(6))
		}
		stocks[i] = st
		discontinued := i == 5 || i == 18
		low := st.inStock <= st.reorder && !discontinued
		rows = append(rows, map[string]any{prod.col("Product"): p.Name, prod.col("Category"): p.Category, prod.col("Unit price"): money(p.Cents),
			prod.col("In stock"): num(st.inStock), prod.col("Reorder level"): num(st.reorder),
			prod.col("Discontinued"): discontinued, prod.col("Low stock"): low})
	}
	prodIDs, err := insert(prod, rows)
	if err != nil {
		return err
	}

	// Orders: oldest first so order numbers rise with time.
	const nOrders = 60
	ages := make([]int, nOrders)
	for i := range ages {
		ages[i] = rnd.IntN(175) + 1
	}
	slices.Sort(ages)
	slices.Reverse(ages) // oldest first
	type lineSeed struct {
		order, product int // order index, product index
		qty            int64
	}
	var lineSeeds []lineSeed
	orderTotals := make([]int64, nOrders)
	rows = nil
	notes := []string{"", "", "", "", "Customer asked for delivery before noon.", "Gift wrap requested.", "Repeat order - same as last month.", "Fragile: handle with care."}
	for i := 0; i < nOrders; i++ {
		age := ages[i]
		status := "Delivered"
		switch {
		case age < 3:
			status = []string{"New", "New", "Packed"}[rnd.IntN(3)]
		case age < 8:
			status = []string{"Packed", "Shipped", "Shipped"}[rnd.IntN(3)]
		case age < 20 && rnd.IntN(3) == 0:
			status = "Shipped"
		case rnd.IntN(25) == 0:
			status = "Cancelled"
		}
		nl := 1 + rnd.IntN(4)
		used := map[int]bool{}
		for j := 0; j < nl; j++ {
			p := rnd.IntN(len(products))
			for used[p] {
				p = rnd.IntN(len(products))
			}
			used[p] = true
			qty := int64(1 + rnd.IntN(24))
			lineSeeds = append(lineSeeds, lineSeed{order: i, product: p, qty: qty})
			orderTotals[i] += qty * products[p].Cents
		}
		rows = append(rows, map[string]any{
			ord.col("Order ref"):  fmt.Sprintf("SO-%04d", 1001+i),
			ord.col("Customer"):   num(custIDs[rnd.IntN(len(custIDs))]),
			ord.col("Employee"):   num(empIDs[rnd.IntN(len(empIDs))]),
			ord.col("Order date"): day(today.AddDate(0, 0, -age)),
			ord.col("Status"):     status,
			ord.col("Total"):      money(orderTotals[i]),
			ord.col("Notes"):      notes[rnd.IntN(len(notes))],
		})
	}
	orderIDs, err := insert(ord, rows)
	if err != nil {
		return err
	}

	rows = nil
	for _, ls := range lineSeeds {
		rows = append(rows, map[string]any{lin.col("Sales order"): num(orderIDs[ls.order]), lin.col("Product"): num(prodIDs[ls.product]),
			lin.col("Quantity"): num(ls.qty), lin.col("Unit price"): money(products[ls.product].Cents)})
	}
	if _, err := insert(lin, rows); err != nil {
		return err
	}

	// ---- saved queries ------------------------------------------------
	O, P, L, E := q(ord.t.PhysicalName), q(prod.t.PhysicalName), q(lin.t.PhysicalName), q(emp.t.PhysicalName)
	saved := []query.SavedInput{
		{Name: "Sales by month", Kind: "sql", Description: "Order count and revenue per month, excluding cancelled orders.",
			SQL: fmt.Sprintf(`SELECT substr(o.%s, 1, 7) AS Month, count(*) AS Orders, sum(o.%s) / 10000.0 AS Revenue FROM %s o `+
				`WHERE o.%s <> 'Cancelled' GROUP BY 1 ORDER BY 1`,
				q(ord.phys("Order date")), q(ord.phys("Total")), O, q(ord.phys("Status")))},
		// Top customers and Open orders are visual queries, so the sample shows the designer: a join,
		// totals, criteria, sorting and a row limit.
		{Name: "Top customers", Kind: "visual", Description: "The ten customers with the highest revenue across all orders.",
			Visual: &query.Model{
				Sources: []query.Source{{Alias: "o", TableID: ord.id(), X: 40, Y: 40}, {Alias: "c", TableID: cust.id(), X: 360, Y: 40}},
				Joins:   []query.Join{{LeftAlias: "o", LeftColumnID: ord.col("Customer"), RightAlias: "c", RightColumnID: "id", Kind: "inner"}},
				Fields: []query.Field{
					{Alias: "c", ColumnID: cust.col("Company"), OutputName: "Customer", Group: "group_by"},
					{Alias: "c", ColumnID: cust.col("Country"), Group: "group_by"},
					{Alias: "o", ColumnID: ord.col("Order ref"), OutputName: "Orders", Group: "count"},
					{Alias: "o", ColumnID: ord.col("Total"), OutputName: "Revenue", Group: "sum", Sort: "desc"},
				},
				Limit: 10,
			}},
		{Name: "Low stock", Kind: "sql", Description: "Active products at or below their reorder level.",
			SQL: fmt.Sprintf(`SELECT p.%s AS Product, p.%s AS Category, p.%s AS "In stock", p.%s AS "Reorder level" FROM %s p `+
				`WHERE p.%s <> 1 AND p.%s <= p.%s ORDER BY p.%s - p.%s`,
				q(prod.phys("Product")), q(prod.phys("Category")), q(prod.phys("In stock")), q(prod.phys("Reorder level")), P,
				q(prod.phys("Discontinued")), q(prod.phys("In stock")), q(prod.phys("Reorder level")), q(prod.phys("In stock")), q(prod.phys("Reorder level")))},
		{Name: "Open orders", Kind: "visual", Description: "Orders that are not yet delivered or cancelled.",
			Visual: &query.Model{
				Sources: []query.Source{{Alias: "o", TableID: ord.id(), X: 40, Y: 40}, {Alias: "c", TableID: cust.id(), X: 360, Y: 40}},
				Joins:   []query.Join{{LeftAlias: "o", LeftColumnID: ord.col("Customer"), RightAlias: "c", RightColumnID: "id", Kind: "inner"}},
				Fields: []query.Field{
					{Alias: "o", ColumnID: ord.col("Order ref"), OutputName: "Order"},
					{Alias: "c", ColumnID: cust.col("Company"), OutputName: "Customer"},
					{Alias: "o", ColumnID: ord.col("Order date"), Sort: "asc"},
					{Alias: "o", ColumnID: ord.col("Status"), Criteria: []query.Criterion{{Op: "in", Value: []any{"New", "Packed", "Shipped"}}}},
					{Alias: "o", ColumnID: ord.col("Total")},
				},
			}},
		{Name: "Revenue by category", Kind: "sql", Description: "Units sold and revenue per product category, from the order lines.",
			SQL: fmt.Sprintf(`SELECT p.%s AS Category, sum(l.%s) AS Units, sum(l.%s) / 10000.0 AS Revenue FROM %s l `+
				`JOIN %s p ON p.id = l.%s JOIN %s o ON o.id = l.%s WHERE o.%s <> 'Cancelled' GROUP BY 1 ORDER BY Revenue DESC`,
				q(prod.phys("Category")), q(lin.phys("Quantity")), q(lin.phys("Line total")), L, P, q(lin.phys("Product")),
				O, q(lin.phys("Sales order")), q(ord.phys("Status")))},
		{Name: "Sales by employee", Kind: "sql", Description: "Revenue per sales representative.",
			SQL: fmt.Sprintf(`SELECT e.%s AS Employee, count(*) AS Orders, sum(o.%s) / 10000.0 AS Revenue FROM %s o JOIN %s e ON e.id = o.%s `+
				`WHERE o.%s <> 'Cancelled' GROUP BY e.id ORDER BY Revenue DESC`,
				q(emp.phys("Name")), q(ord.phys("Total")), O, E, q(ord.phys("Employee")), q(ord.phys("Status")))},
	}
	for _, in := range saved {
		_, err := b.Queries.Put(ctx, l, "", 0, in, owner)
		if err != nil {
			return fmt.Errorf("save query %q: %w", in.Name, err)
		}
	}

	// ---- form ---------------------------------------------------------
	if _, err := b.Forms.Put(ctx, l, "", 0, customerForm(cust), owner); err != nil {
		return fmt.Errorf("create form: %w", err)
	}

	// ---- report -------------------------------------------------------
	if _, err := b.Reports.Put(ctx, l, "", 0, productReport(prod), owner); err != nil {
		return fmt.Errorf("create report: %w", err)
	}

	// ---- biosecurity: geographic fields and attachments ---------------
	kit := &bioKit{b: b, ctx: ctx, l: l, owner: owner, now: now, apply: apply, table: table, insert: insert}
	if err := kit.run(); err != nil {
		return fmt.Errorf("biosecurity: %w", err)
	}

	// ---- job script and job -------------------------------------------
	script, err := b.Scripts.Put(ctx, l, "", 0, scripts.ScriptInput{Name: "Refresh low-stock flags", Kind: "job", Source: lowStockScript(prod)}, owner)
	if err != nil {
		return fmt.Errorf("create script: %w", err)
	}
	yes := true
	if _, err := b.Jobs.Put(ctx, l, "", 0, jobs.JobInput{Name: "Refresh low-stock flags", ScriptID: script.ID, Schedule: "30 6 * * *", Enabled: &yes}, owner); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func lowStockScript(prod tbl) string {
	p, flag, stockC, reorder, disc := q(prod.t.PhysicalName), q(prod.phys("Low stock")), q(prod.phys("In stock")), q(prod.phys("Reorder level")), q(prod.phys("Discontinued"))
	// Lua long strings keep the SQL readable; identifiers come from the catalog.
	return fmt.Sprintf(`-- Marks products whose stock has fallen to their reorder level.
-- Runs daily (see Jobs). It only reads and writes this workspace: no network.
db.exec([[UPDATE %[1]s SET %[2]s = CASE WHEN %[3]s <= %[4]s AND %[5]s <> 1 THEN 1 ELSE 0 END]])

local low = db.query_value([[SELECT count(*) FROM %[1]s WHERE %[2]s = 1]])
local total = db.query_value([[SELECT count(*) FROM %[1]s WHERE %[5]s <> 1]])
log.info("low-stock products", {low = low, active = total})

artifacts.save("low-stock.csv", csv.encode({{"low_stock_products", low}, {"active_products", total}}))
`, p, flag, stockC, reorder, disc)
}

func customerForm(cust tbl) forms.FormInput {
	var controls []forms.Control
	y := 12
	n := 0
	add := func(display, kind string) {
		n++
		controls = append(controls,
			forms.Control{ID: fmt.Sprintf("l%d", n), Name: fmt.Sprintf("Label%d", n), Type: "label", X: 16, Y: y + 3, W: 110, H: 22, Caption: display},
			forms.Control{ID: fmt.Sprintf("t%d", n), Name: strings.ReplaceAll(display, " ", "_"), Type: kind, X: 136, Y: y, W: 280, H: 28, TabIndex: n,
				Bind: &forms.Bind{ColumnID: cust.col(display)}})
		y += 38
	}
	for _, c := range []string{"Company", "Contact", "City", "Country", "Email"} {
		add(c, "textbox")
	}
	add("Active", "checkbox")
	return forms.FormInput{Name: "Customer details", SourceKind: "table", SourceID: cust.id(), DefaultView: "single",
		Layout: forms.Layout{Width: 440, Sections: forms.Sections{Detail: forms.Section{Height: y + 12, Controls: controls}}}}
}

func productReport(prod tbl) reports.ReportInput {
	col := prod.col
	ctl := func(id, name string, x, w int, column string, format string) reports.ReportControl {
		return reports.ReportControl{ID: id, Name: name, Type: "textbox", X: x, W: w, H: 20, Bind: &forms.Bind{ColumnID: col(column)}, Format: format}
	}
	lay := reports.ReportLayout{Width: 640,
		Bands: reports.Bands{
			ReportHeader: reports.Band{Height: 36, Controls: []reports.ReportControl{{ID: "title", Name: "Title", Type: "label", W: 400, H: 30, Caption: "Product catalogue"}}},
			PageHeader: reports.Band{Height: 22, Controls: []reports.ReportControl{
				{ID: "h1", Name: "HProduct", Type: "label", W: 260, H: 20, Caption: "Product"},
				{ID: "h2", Name: "HPrice", Type: "label", X: 280, W: 100, H: 20, Caption: "Unit price"},
				{ID: "h3", Name: "HStock", Type: "label", X: 400, W: 100, H: 20, Caption: "In stock"}}},
			Detail: reports.Band{Height: 22, Controls: []reports.ReportControl{
				ctl("d1", "Product", 0, 260, "Product", ""), ctl("d2", "Price", 280, 100, "Unit price", ""), ctl("d3", "Stock", 400, 100, "In stock", "")}},
			PageFooter: reports.Band{Height: 22, Controls: []reports.ReportControl{{ID: "pn", Name: "PageNo", Type: "page_number", W: 60, H: 20}}},
			ReportFooter: reports.Band{Height: 26, Controls: []reports.ReportControl{
				{ID: "tcl", Name: "TotalProductsLabel", Type: "label", W: 100, H: 20, Caption: "Total products:", Style: map[string]string{"bold": "true"}},
				{ID: "tc", Name: "TotalProducts", Type: "textbox", X: 104, W: 60, H: 20, Aggregate: &reports.Aggregate{Func: "count", Scope: "report"}}}},
		},
		Groups: []reports.Group{{ColumnID: col("Category"), ReportSort: "asc",
			Header: reports.Band{Height: 26, Controls: []reports.ReportControl{ctl("g1", "CategoryName", 0, 260, "Category", "")}},
			Footer: reports.Band{Height: 24, Controls: []reports.ReportControl{
				{ID: "gcl", Name: "CategoryCountLabel", Type: "label", W: 70, H: 20, Caption: "Products:", Style: map[string]string{"bold": "true"}},
				{ID: "gc", Name: "CategoryCount", Type: "textbox", X: 74, W: 60, H: 20, Aggregate: &reports.Aggregate{Func: "count", Scope: "group"}},
				{ID: "gsl", Name: "CategoryStockLabel", Type: "label", X: 280, W: 116, H: 20, Caption: "Units in stock:", Style: map[string]string{"bold": "true", "align": "right"}},
				{ID: "gs", Name: "CategoryStock", Type: "textbox", X: 400, W: 100, H: 20, Aggregate: &reports.Aggregate{Func: "sum", ColumnID: col("In stock"), Scope: "group"}}}}}},
		Sorts: []reports.ReportSort{{ColumnID: col("Product")}},
	}
	return reports.ReportInput{Name: "Product catalogue", SourceKind: "table", SourceID: prod.id(), ReportLayout: lay,
		PageSetup: reports.PageSetup{Paper: "a4", Orientation: "portrait", MarginTop: 48, MarginBot: 48, MarginLeft: 48, MarginRight: 48}}
}

func removeFiles(paths ...string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

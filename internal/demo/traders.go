package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/forms"
	"abidata/internal/jobs"
	"abidata/internal/query"
	"abidata/internal/reports"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

// tradersKit builds the Northwind Traders sample workspace: commercial order-processing
// with employees, customers, products, orders, order lines, queries, a form, a report
// and a scheduled inventory refresh job.
type tradersKit struct {
	b      *Builder
	ctx    context.Context
	l      *workspace.Lease
	owner  string
	now    time.Time
	apply  func(ops ...catalog.Op) (*catalog.Schema, error)
	table  func(sch *catalog.Schema, display string) tbl
	insert func(t tbl, rows []map[string]any) ([]int64, error)
}

func (k *tradersKit) run() error {
	rnd := rand.New(rand.NewPCG(7, 1998)) //nolint:gosec // deterministic sample data, not security

	// ---- tables -------------------------------------------------------
	sch, err := k.apply(
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
	emp, cust, prod := k.table(sch, "Employees"), k.table(sch, "Customers"), k.table(sch, "Products")

	sch, err = k.apply(create("Orders", "One row per sales order. Each order has lines in Order lines.",
		required(text("Order ref")), lookup("Customer", cust, "Company"), lookup("Employee", emp, "Name"),
		typed("Order date", catalog.Date), text("Status"), typed("Total", catalog.Currency), typed("Notes", catalog.LongText)))
	if err != nil {
		return err
	}
	ord := k.table(sch, "Orders")

	lineTotal := catalog.ColumnSpec{DisplayName: "Line total", Type: catalog.Calculated,
		Options: catalog.TypeOptions{Expression: "Quantity * Unit_price", ResultType: "currency"}}
	sch, err = k.apply(create("Order lines", "The products on each order.",
		lookup("Sales order", ord, "Order ref"), lookup("Product", prod, "Product"),
		typed("Quantity", catalog.Integer), typed("Unit price", catalog.Currency), lineTotal))
	if err != nil {
		return err
	}
	lin := k.table(sch, "Order lines")
	// Re-read the other tables: their columns are unchanged but pointers into
	// the previous snapshot would be stale.
	emp, cust, prod, ord = k.table(sch, "Employees"), k.table(sch, "Customers"), k.table(sch, "Products"), k.table(sch, "Orders")

	day := func(t time.Time) string { return t.Format("2006-01-02") }
	today := time.Date(k.now.Year(), k.now.Month(), k.now.Day(), 0, 0, 0, 0, time.UTC)

	// ---- rows ---------------------------------------------------------
	var rows []map[string]any
	for i, e := range employees {
		rows = append(rows, map[string]any{emp.col("Name"): e.Name, emp.col("Title"): e.Title, emp.col("Email"): e.Email,
			emp.col("Hire date"): day(today.AddDate(-(1 + i), -(i * 3), 0))})
	}
	empIDs, err := k.insert(emp, rows)
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
	custIDs, err := k.insert(cust, rows)
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
	prodIDs, err := k.insert(prod, rows)
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
	orderIDs, err := k.insert(ord, rows)
	if err != nil {
		return err
	}

	rows = nil
	for _, ls := range lineSeeds {
		rows = append(rows, map[string]any{lin.col("Sales order"): num(orderIDs[ls.order]), lin.col("Product"): num(prodIDs[ls.product]),
			lin.col("Quantity"): num(ls.qty), lin.col("Unit price"): money(products[ls.product].Cents)})
	}
	if _, err := k.insert(lin, rows); err != nil {
		return err
	}

	// ---- saved queries ------------------------------------------------
	O, P, L, E := q(ord.t.PhysicalName), q(prod.t.PhysicalName), q(lin.t.PhysicalName), q(emp.t.PhysicalName)
	saved := []query.SavedInput{
		{Name: "Sales by month", Kind: "sql", Description: "Order count and revenue per month, excluding cancelled orders.",
			SQL: fmt.Sprintf(`SELECT substr(o.%s, 1, 7) AS "Month", count(*) AS "Orders", sum(o.%s) / 10000.0 AS "Revenue" FROM %s o `+
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
			SQL: fmt.Sprintf(`SELECT p.%s AS "Product", p.%s AS "Category", p.%s AS "In stock", p.%s AS "Reorder level" FROM %s p `+
				`WHERE NOT p.%s AND p.%s <= p.%s ORDER BY p.%s - p.%s`,
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
			SQL: fmt.Sprintf(`SELECT p.%s AS "Category", sum(l.%s) AS "Units", sum(l.%s) / 10000.0 AS "Revenue" FROM %s l `+
				`JOIN %s p ON p.id = l.%s JOIN %s o ON o.id = l.%s WHERE o.%s <> 'Cancelled' GROUP BY 1 ORDER BY "Revenue" DESC`,
				q(prod.phys("Category")), q(lin.phys("Quantity")), q(lin.phys("Line total")), L, P, q(lin.phys("Product")),
				O, q(lin.phys("Sales order")), q(ord.phys("Status")))},
		{Name: "Sales by employee", Kind: "sql", Description: "Revenue per sales representative.",
			SQL: fmt.Sprintf(`SELECT e.%s AS "Employee", count(*) AS "Orders", sum(o.%s) / 10000.0 AS "Revenue" FROM %s o JOIN %s e ON e.id = o.%s `+
				`WHERE o.%s <> 'Cancelled' GROUP BY e.id ORDER BY "Revenue" DESC`,
				q(emp.phys("Name")), q(ord.phys("Total")), O, E, q(ord.phys("Employee")), q(ord.phys("Status")))},
	}
	for _, in := range saved {
		_, err := k.b.Queries.Put(k.ctx, k.l, "", 0, in, k.owner)
		if err != nil {
			return fmt.Errorf("save query %q: %w", in.Name, err)
		}
	}

	// ---- form ---------------------------------------------------------
	if _, err := k.b.Forms.Put(k.ctx, k.l, "", 0, customerForm(cust), k.owner); err != nil {
		return fmt.Errorf("create form: %w", err)
	}

	// ---- report -------------------------------------------------------
	if _, err := k.b.Reports.Put(k.ctx, k.l, "", 0, productReport(prod), k.owner); err != nil {
		return fmt.Errorf("create report: %w", err)
	}

	// ---- job script and job -------------------------------------------
	script, err := k.b.Scripts.Put(k.ctx, k.l, "", 0, scripts.ScriptInput{Name: "Refresh low-stock flags", Kind: "job", Source: lowStockScript(prod)}, k.owner)
	if err != nil {
		return fmt.Errorf("create script: %w", err)
	}
	yes := true
	if _, err := k.b.Jobs.Put(k.ctx, k.l, "", 0, jobs.JobInput{Name: "Refresh low-stock flags", ScriptID: script.ID, Schedule: "30 6 * * *", Enabled: &yes}, k.owner); err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func lowStockScript(prod tbl) string {
	p, flag, stockC, reorder, disc := q(prod.t.PhysicalName), q(prod.phys("Low stock")), q(prod.phys("In stock")), q(prod.phys("Reorder level")), q(prod.phys("Discontinued"))
	// Lua long strings keep the SQL readable; identifiers come from the catalog.
	return fmt.Sprintf(`-- Marks products whose stock has fallen to their reorder level.
-- Runs daily (see Jobs). It only reads and writes this workspace: no network.
db.exec([[UPDATE %[1]s SET %[2]s = COALESCE(%[3]s <= %[4]s AND NOT %[5]s, FALSE)]])

local low = db.query_value([[SELECT count(*) FROM %[1]s WHERE %[2]s]])
local total = db.query_value([[SELECT count(*) FROM %[1]s WHERE NOT %[5]s]])
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

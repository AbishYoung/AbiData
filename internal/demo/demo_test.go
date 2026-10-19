package demo_test

import (
	"context"
	"image/jpeg"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/admin/admintest"
	"abidata/internal/artifacts"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/demo"
	"abidata/internal/files"
	"abidata/internal/forms"
	"abidata/internal/geo"
	"abidata/internal/ids"
	"abidata/internal/jobs"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

type rig struct {
	ctx   context.Context
	b     *demo.Builder
	adm   *admin.Store
	wm    *workspace.Manager
	owner string
	sched *jobs.Scheduler
	dir   string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	adm := admintest.Open(t)
	owner := ids.New()
	if err := adm.CreateUser(ctx, admin.User{ID: owner, Email: "owner@example.com", DisplayName: "Owner", IsPlatformAdmin: true, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	wm := wstest.New(t).Manager
	schemas := catalog.NewCache()
	engine := &catalog.Engine{Validators: []catalog.Validator{query.SchemaValidator, forms.SchemaValidator, reports.SchemaValidator},
		AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) { schemas.Put(l.ID(), s) }}
	rec := &records.Service{Schemas: schemas}
	qs := &query.Service{Schemas: schemas}
	ss := &scripts.Service{}
	runner := script.NewRunner(4, 2)
	arts := &artifacts.Store{Dir: filepath.Join(dir, "artifacts")}
	store := &jobs.SQLStore{Admin: adm, Workspaces: wm}
	exec := &jobs.LuaExecutor{Workspaces: wm, Scripts: ss, Runner: runner, Schemas: schemas, Records: rec, Artifacts: arts}
	sched := jobs.New(jobs.Config{Store: store, Executor: exec, Workers: 2})
	t.Cleanup(func() {
		sched.Wait()
	})
	return &rig{ctx: ctx, adm: adm, wm: wm, owner: owner, sched: sched, dir: dir, b: &demo.Builder{
		Admin: adm, Workspaces: wm, Schemas: schemas, Catalog: engine, Records: rec, Queries: qs, Scripts: ss,
		Forms:   &forms.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Reports: &reports.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Jobs:    &jobs.Service{Scripts: ss, Scheduler: sched},
		Files:   &files.Service{DataDir: dir},
	}}
}

func count(t *testing.T, r *rig, l *workspace.Lease, sql string) int {
	t.Helper()
	var n int
	if err := l.Read(r.ctx, func(q dbx.Querier) error { return q.QueryRow(r.ctx, sql).Scan(&n) }); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func TestCreateBuildsNorthwind(t *testing.T) {
	r := newRig(t)
	res, err := r.b.Create(r.ctx, demo.Traders, r.owner)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Fatal("expected Created")
	}
	access, err := r.adm.ListWorkspacesForUser(r.ctx, r.owner)
	if err != nil || len(access) != 1 || access[0].Workspace.Name != demo.WorkspaceName || access[0].Role != admin.RoleOwner {
		t.Fatalf("workspace listing: %+v %v", access, err)
	}
	l, err := r.wm.Acquire(r.ctx, res.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()

	// The invasive species sample is a workspace of its own.
	if n := count(t, r, l, `SELECT count(*) FROM _sys_tables WHERE display_name IN ('Suppliers', 'Species', 'Pest sightings', 'Quarantine zones')`); n != 0 {
		t.Errorf("Northwind Traders has %d invasive-species tables, want none", n)
	}

	want := map[string][2]int{"Employees": {6, 6}, "Customers": {25, 25}, "Products": {20, 20}, "Orders": {60, 60}}
	for name, w := range want {
		n := count(t, r, l, `SELECT count(*) FROM "`+name+`"`)
		if n < w[0] {
			t.Errorf("%s: %d rows, want >= %d", name, n, w[0])
		}
	}
	if n := count(t, r, l, `SELECT count(*) FROM "Order_lines"`); n < 60 {
		t.Errorf("Order_lines: %d rows", n)
	}
	// Order totals agree with their lines (a sanity check of the generator).
	if n := count(t, r, l, `SELECT count(*) FROM "Orders" o WHERE o."Total" <> (SELECT sum("Line_total") FROM "Order_lines" WHERE "Sales_order" = o.id)`); n != 0 {
		t.Errorf("%d orders whose total differs from their lines", n)
	}
	// Dates are relative to now: the newest order is recent, the oldest within ~6 months.
	var newest, oldest string
	if err := l.Read(r.ctx, func(q dbx.Querier) error {
		return q.QueryRow(r.ctx, `SELECT CAST(max("Order_date") AS TEXT), CAST(min("Order_date") AS TEXT) FROM "Orders"`).Scan(&newest, &oldest)
	}); err != nil {
		t.Fatal(err)
	}
	nt, _ := time.Parse("2006-01-02", newest)
	ot, _ := time.Parse("2006-01-02", oldest)
	if time.Since(nt) > 30*24*time.Hour || time.Since(ot) > 200*24*time.Hour || time.Since(nt) < 0 {
		t.Errorf("order dates not relative to now: %s .. %s", oldest, newest)
	}
	// History is tracked (rows were written through the records service).
	if n := count(t, r, l, `SELECT count(*) FROM _sys_row_history`); n == 0 {
		t.Error("no row history recorded")
	}

	// Saved queries exist and run.
	ql, err := r.b.Queries.List(r.ctx, l)
	if err != nil || len(ql) < 4 {
		t.Fatalf("queries: %d %v", len(ql), err)
	}
	kinds := map[string]string{}
	for _, s := range ql {
		kinds[s.Name] = s.Kind
		var params query.Params
		if s.Name == "Sightings near a point" { // the one query that needs its parameters
			params = query.Params{"lat": 51.27, "lon": 0.52, "metres": 5000.0}
		}
		out, err := r.b.Queries.RunSaved(r.ctx, l, query.Policy{}, s.ID, params, 100)
		if err != nil {
			t.Errorf("query %q: %v", s.Name, err)
			continue
		}
		if s.Name == "Sales by month" && len(out.Rows) < 4 {
			t.Errorf("Sales by month rows: %d", len(out.Rows))
		}
		if s.Name == "Top customers" && len(out.Rows) != 10 {
			t.Errorf("Top customers rows: %d", len(out.Rows))
		}
		if s.Name == "Top customers" {
			if s.Visual == nil || len(s.Visual.Joins) == 0 {
				t.Error("Top customers should be a visual query with a join")
			}
			if want := []string{"text", "text", "integer", "currency"}; !reflect.DeepEqual(out.Types, want) {
				t.Errorf("Top customers column types %v, want %v", out.Types, want)
			}
		}
		if s.Name == "Open orders" {
			if s.Visual == nil || len(s.Visual.Joins) == 0 || len(out.Rows) == 0 {
				t.Errorf("Open orders should be a visual query with a join and rows: %+v", out.Rows)
			}
			for _, row := range out.Rows {
				if st := row[3]; st != "New" && st != "Packed" && st != "Shipped" {
					t.Errorf("Open orders contains status %v", st)
				}
			}
		}
		if s.Name == "Low stock" && len(out.Rows) == 0 {
			t.Error("Low stock query empty: the demo should have some low-stock products")
		}
	}
	if kinds["Top customers"] != "visual" || kinds["Open orders"] != "visual" || kinds["Low stock"] != "sql" {
		t.Errorf("query kinds: %v (the sample mixes visual and SQL queries)", kinds)
	}
	fl, err := r.b.Forms.List(r.ctx, l)
	if err != nil || len(fl) != 1 || fl[0].Name != "Customer details" {
		t.Fatalf("forms: %v %v", fl, err)
	}
	rl, err := r.b.Reports.List(r.ctx, l)
	if err != nil || len(rl) != 1 {
		t.Fatalf("reports: %v %v", rl, err)
	}
	// Group and report footer totals carry captions.
	var rep reports.Report
	for _, x := range rl {
		if x.Name == "Product catalogue" {
			if rep, err = r.b.Reports.Get(r.ctx, l, x.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	captions := func(b reports.Band) []string {
		var out []string
		for _, c := range b.Controls {
			if c.Type == "label" {
				out = append(out, c.Caption)
			}
		}
		return out
	}
	if got := captions(rep.ReportLayout.Groups[0].Footer); !reflect.DeepEqual(got, []string{"Products:", "Units in stock:"}) {
		t.Errorf("group footer captions: %v", got)
	}
	if got := captions(rep.ReportLayout.Bands.ReportFooter); !reflect.DeepEqual(got, []string{"Total products:"}) {
		t.Errorf("report footer captions: %v", got)
	}
}

func TestJobScriptValidAndRuns(t *testing.T) {
	r := newRig(t)
	res, err := r.b.Create(r.ctx, demo.Traders, r.owner)
	if err != nil {
		t.Fatal(err)
	}
	l, err := r.wm.Acquire(r.ctx, res.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	sch, err := r.b.Schemas.Get(r.ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	sl, err := r.b.Scripts.List(r.ctx, l, "job")
	if err != nil || len(sl) != 1 {
		t.Fatalf("scripts: %v %v", sl, err)
	}
	full, err := r.b.Scripts.Get(r.ctx, l, sl[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range scripts.Check(full.Source, full.Kind, sch) {
		if m.Severity == "error" {
			t.Fatalf("script check: %+v", m)
		}
	}
	if strings.Contains(full.Source, "fetch") {
		t.Fatal("demo script must not use the network")
	}
	jl, err := r.b.Jobs.List(r.ctx, l)
	if err != nil || len(jl) != 1 || jl[0].Schedule == "" || !jl[0].Enabled {
		t.Fatalf("jobs: %+v %v", jl, err)
	}

	// Corrupt the flag, run the job, and see it repaired.
	if err := l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(r.ctx, `UPDATE "Products" SET "Low_stock" = FALSE`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	run, err := r.sched.RunNow(r.ctx, res.WorkspaceID, jl[0].ID, jobs.TriggerManual, r.owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got jobs.Run
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if got, err = r.b.Jobs.GetRun(r.ctx, l, run.ID); err == nil && got.FinishedAt != nil {
			break
		}
	}
	if got.Status != jobs.StatusSucceeded {
		t.Fatalf("run: %+v", got)
	}
	if n := count(t, r, l, `SELECT count(*) FROM "Products" WHERE "Low_stock"`); n == 0 {
		t.Error("job did not set low-stock flags")
	}
	if n := count(t, r, l, `SELECT count(*) FROM "Products" WHERE "Low_stock" AND "Discontinued"`); n != 0 {
		t.Error("discontinued products must not be flagged")
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	r := newRig(t)
	first, err := r.b.Create(r.ctx, demo.Traders, r.owner)
	if err != nil || !first.Created {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := r.b.Create(r.ctx, demo.Traders, r.owner)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.WorkspaceID != first.WorkspaceID {
		t.Fatalf("second: %+v", second)
	}
	all, _ := r.adm.ListWorkspaces(r.ctx)
	if len(all) != 1 {
		t.Fatalf("%d workspaces", len(all))
	}
}

// TestInvasivesIsItsOwnWorkspace: the biosecurity sample is a separate
// workspace with only its own tables, and both samples can live side by side.
func TestInvasivesIsItsOwnWorkspace(t *testing.T) {
	r := newRig(t)
	traders, err := r.b.Create(r.ctx, demo.Traders, r.owner)
	if err != nil || !traders.Created {
		t.Fatalf("traders: %+v %v", traders, err)
	}
	inv, err := r.b.Create(r.ctx, demo.Invasives, r.owner)
	if err != nil || !inv.Created || inv.WorkspaceID == traders.WorkspaceID {
		t.Fatalf("invasives: %+v %v", inv, err)
	}
	access, err := r.adm.ListWorkspacesForUser(r.ctx, r.owner)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range access {
		names = append(names, a.Workspace.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{demo.InvasivesName, demo.WorkspaceName}) {
		t.Fatalf("workspaces = %q", names)
	}
	l, err := r.wm.Acquire(r.ctx, inv.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	var tables []string
	if err := l.Read(r.ctx, func(q dbx.Querier) error {
		rows, err := q.Query(r.ctx, `SELECT display_name FROM _sys_tables ORDER BY display_name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			tables = append(tables, n)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Pest sightings", "Quarantine zones", "Species", "Suppliers"}; !slices.Equal(tables, want) {
		t.Fatalf("Northwind Invasives tables = %q, want %q", tables, want)
	}
	if _, err := r.b.Create(r.ctx, demo.Kind("nope"), r.owner); err == nil {
		t.Error("an unknown kind must be refused")
	}
}

func TestDeterministicContent(t *testing.T) {
	fixed := func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }
	var sums [2]string
	for i := range sums {
		r := newRig(t)
		r.b.Now = fixed
		res, err := r.b.Create(r.ctx, demo.Traders, r.owner)
		if err != nil {
			t.Fatal(err)
		}
		l, err := r.wm.Acquire(r.ctx, res.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		row := `"Order_ref" || CAST("Order_date" AS TEXT) || CAST("Total" AS TEXT) || "Status"`
		agg := `SELECT group_concat(` + row + `, ',') FROM (SELECT * FROM "Orders" ORDER BY id) AS o`
		if wstest.Postgres() {
			agg = `SELECT string_agg(` + row + `, ',' ORDER BY id) FROM "Orders"`
		}
		err = l.Read(r.ctx, func(q dbx.Querier) error { return q.QueryRow(r.ctx, agg).Scan(&sums[i]) })
		l.Release()
		if err != nil {
			t.Fatal(err)
		}
	}
	if sums[0] == "" || sums[0] != sums[1] {
		t.Fatal("demo content differs between builds with the same clock")
	}
}

// bioRig builds the Northwind Invasives sample and opens its workspace.
func bioRig(t *testing.T) (*rig, *workspace.Lease, *catalog.Schema) {
	t.Helper()
	r := newRig(t)
	res, err := r.b.Create(r.ctx, demo.Invasives, r.owner)
	if err != nil {
		t.Fatal(err)
	}
	l, err := r.wm.Acquire(r.ctx, res.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Release)
	sch, err := r.b.Schemas.Get(r.ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	return r, l, sch
}

func tableByName(t *testing.T, sch *catalog.Schema, name string) *catalog.Table {
	t.Helper()
	for i := range sch.Tables {
		if sch.Tables[i].DisplayName == name {
			return &sch.Tables[i]
		}
	}
	t.Fatalf("table %q missing", name)
	return nil
}

func colByName(t *testing.T, tb *catalog.Table, name string) *catalog.Column {
	t.Helper()
	for i := range tb.Columns {
		if tb.Columns[i].DisplayName == name {
			return &tb.Columns[i]
		}
	}
	t.Fatalf("field %q missing from %q", name, tb.DisplayName)
	return nil
}

func savedByName(t *testing.T, r *rig, l *workspace.Lease, name string) query.Saved {
	t.Helper()
	ql, err := r.b.Queries.List(r.ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ql {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("query %q missing", name)
	return query.Saved{}
}

// TestBiosecurityTablesAndData: the invasive-species tables, fields and rows.
func TestBiosecurityTablesAndData(t *testing.T) {
	r, l, sch := bioRig(t)
	want := map[string]map[string]catalog.FieldType{
		"Species":          {"Name": catalog.ShortText, "Scientific name": catalog.ShortText, "Category": catalog.ShortText, "Risk level": catalog.ShortText, "Notes": catalog.LongText},
		"Suppliers":        {"Name": catalog.ShortText, "Location": catalog.GeoPoint},
		"Pest sightings":   {"Species": catalog.Lookup, "Observed at": catalog.DateTime, "Location": catalog.GeoPoint, "GPS accuracy (m)": catalog.Number, "Count / abundance": catalog.Integer, "Status": catalog.ShortText, "Supplier": catalog.Lookup, "Photos": catalog.Attachments, "Notes": catalog.LongText},
		"Quarantine zones": {"Name": catalog.ShortText, "Boundary": catalog.GeoArea, "Species": catalog.Lookup, "Declared on": catalog.Date, "Status": catalog.ShortText, "Notes": catalog.LongText},
	}
	for tn, cols := range want {
		tb := tableByName(t, sch, tn)
		for cn, ft := range cols {
			if got := colByName(t, tb, cn).Type; got != ft {
				t.Errorf("%s.%s: type %s, want %s", tn, cn, got, ft)
			}
		}
	}
	if got := colByName(t, tableByName(t, sch, "Pest sightings"), "Photos").MaxFilesLimit(); got != 10 {
		t.Errorf("Photos max files: %d, want 10", got)
	}

	for table, min := range map[string]int{"Species": 6, "Suppliers": 6, "Pest_sightings": 40, "Quarantine_zones": 3} {
		if n := count(t, r, l, `SELECT count(*) FROM "`+table+`"`); n < min {
			t.Errorf("%s: %d rows, want >= %d", table, n, min)
		}
	}
	if n := count(t, r, l, `SELECT count(*) FROM "Suppliers" WHERE geo_lat("Location") IS NULL`); n != 0 {
		t.Errorf("%d suppliers without a location", n)
	}
	if n := count(t, r, l, `SELECT count(DISTINCT "Supplier") FROM "Pest_sightings"`); n < 3 || n > 4 {
		t.Errorf("sightings cluster around %d farms, want 3 or 4", n)
	}
	// Every sighting is within 5 km of its supplier's farm.
	if n := count(t, r, l, `SELECT count(*) FROM "Pest_sightings" s JOIN "Suppliers" f ON f.id = s."Supplier" WHERE geo_distance_m(s."Location", f."Location") > 5000`); n != 0 {
		t.Errorf("%d sightings far from their supplier", n)
	}
	// Dates spread over about six months before the (real) seed time, none in the future.
	var oldest, newest any
	if err := l.Read(r.ctx, func(q dbx.Querier) error {
		return q.QueryRow(r.ctx, `SELECT min("Observed_at"), max("Observed_at") FROM "Pest_sightings"`).Scan(&oldest, &newest)
	}); err != nil {
		t.Fatal(err)
	}
	ot, err1 := asTime(oldest)
	nt, err2 := asTime(newest)
	if err1 != nil || err2 != nil {
		t.Fatalf("observed at: %q %q", oldest, newest)
	}
	if age := time.Since(ot); age < 120*24*time.Hour || age > 190*24*time.Hour {
		t.Errorf("oldest sighting is %v old", age)
	}
	if age := time.Since(nt); age < 0 || age > 10*24*time.Hour {
		t.Errorf("newest sighting is %v old", age)
	}
	if n := count(t, r, l, `SELECT count(*) FROM "Pest_sightings" WHERE "GPS_accuracy_m" IS NOT NULL`); n < 30 {
		t.Errorf("%d sightings with an accuracy", n)
	}
	// Row history is tracked for the new tables as well.
	if n := count(t, r, l, `SELECT count(*) FROM _sys_row_history WHERE table_id = '`+tableByName(t, sch, "Pest sightings").ID+`'`); n < 40 {
		t.Errorf("%d history rows for sightings", n)
	}
	// The observed-at default stamps new rows with the current time.
	if err := l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(r.ctx, `INSERT INTO "Pest_sightings"("Species") VALUES (1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var stamp any
	if err := l.Read(r.ctx, func(q dbx.Querier) error {
		return q.QueryRow(r.ctx, `SELECT "Observed_at" FROM "Pest_sightings" ORDER BY id DESC LIMIT 1`).Scan(&stamp)
	}); err != nil {
		t.Fatal(err)
	}
	if st, err := asTime(stamp); err != nil || time.Since(st) > time.Minute || time.Since(st) < -time.Minute {
		t.Errorf("default observed at: %q %v", stamp, err)
	}
}

// TestBiosecuritySpatialQueries: the spatial join, the distance and area queries.
func TestBiosecuritySpatialQueries(t *testing.T) {
	r, l, _ := bioRig(t)
	total := count(t, r, l, `SELECT count(*) FROM "Pest_sightings"`)

	inside := savedByName(t, r, l, "Sightings inside quarantine zones")
	if inside.Kind != "visual" || inside.Visual == nil || len(inside.Visual.Joins) == 0 || inside.Visual.Joins[0].Op != "within" {
		t.Fatalf("expected a visual query with a spatial join: %+v", inside.Visual)
	}
	out, err := r.b.Queries.RunSaved(r.ctx, l, query.Policy{}, inside.ID, nil, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Zone", "Species", "Observed at", "Status", "Zone status"}; !reflect.DeepEqual(out.Columns, want) {
		t.Fatalf("columns: %v", out.Columns)
	}
	if len(out.Rows) < 5 || len(out.Rows) >= total {
		t.Fatalf("%d of %d sightings inside zones: want some, but not all", len(out.Rows), total)
	}
	zonesSeen := map[any]bool{}
	for _, row := range out.Rows {
		zonesSeen[row[0]] = true
		if row[1] == nil || row[2] == nil || row[3] == nil {
			t.Errorf("row with empty species, time or status: %v", row)
		}
	}
	if len(zonesSeen) != 3 {
		t.Errorf("sightings fall inside %d zones, want all 3: %v", len(zonesSeen), zonesSeen)
	}
	// The hole: sightings in the treated orchard block are inside the zone's outer ring but not in the zone.
	holeSightings := count(t, r, l, `SELECT count(*) FROM "Pest_sightings" s, "Quarantine_zones" z, "Suppliers" f
		WHERE z."Name" = 'Loire Hornet Exclusion' AND f."Name" = 'Loire Valley Growers'
		AND geo_distance_m(s."Location", geo_point(geo_lat(f."Location") - 300 / 111320.0, geo_lon(f."Location") + 400 / (111320.0 * cos(radians(geo_lat(f."Location")))))) < 600
		AND NOT geo_within(s."Location", z."Boundary")`)
	if holeSightings < 2 {
		t.Errorf("%d sightings in the hole of the Loire zone, want at least 2", holeSightings)
	}
	if n := count(t, r, l, jsonSQL(`SELECT count(*) FROM "Quarantine_zones" WHERE json_array_length(json_extract("Boundary", '$.coordinates')) = 2`,
		`SELECT count(*) FROM "Quarantine_zones" WHERE jsonb_array_length("Boundary" -> 'coordinates') = 2`)); n != 1 {
		t.Errorf("%d zones with a hole, want exactly 1", n)
	}
	// And some sightings are outside every zone.
	outside := count(t, r, l, `SELECT count(*) FROM "Pest_sightings" s WHERE NOT EXISTS (SELECT 1 FROM "Quarantine_zones" z WHERE geo_within(s."Location", z."Boundary"))`)
	if outside < 5 || outside+len(out.Rows) < total {
		t.Errorf("%d sightings outside every zone (%d inside rows, %d total)", outside, len(out.Rows), total)
	}

	near := savedByName(t, r, l, "Sightings near a point")
	if got := len(near.Parameters); got != 3 {
		t.Fatalf("near-point parameters: %+v", near.Parameters)
	}
	within := func(metres float64) [][]any {
		res, err := r.b.Queries.RunSaved(r.ctx, l, query.Policy{}, near.ID, query.Params{"lat": 51.2704, "lon": 0.5227, "metres": metres}, 1000)
		if err != nil {
			t.Fatal(err)
		}
		return res.Rows
	}
	small, large := within(1500.0), within(6000.0)
	if len(small) == 0 || len(large) <= len(small) {
		t.Errorf("near the Kent farm: %d within 1.5 km, %d within 6 km", len(small), len(large))
	}
	last := -1.0
	for _, row := range large {
		d, _ := row[4].(float64)
		if d < last {
			t.Fatalf("not ordered by distance: %v", large)
		}
		last = d
	}

	sizes := savedByName(t, r, l, "Zone sizes")
	res, err := r.b.Queries.RunSaved(r.ctx, l, query.Policy{}, sizes.ID, nil, 100)
	if err != nil || len(res.Rows) != 3 {
		t.Fatalf("zone sizes: %v %v", res.Rows, err)
	}
	for _, row := range res.Rows {
		ha, _ := row[3].(float64)
		if ha < 500 || ha > 5000 {
			t.Errorf("zone %v: %v ha is not plausible for a zone a few km across", row[0], row[3])
		}
	}
}

// TestBiosecurityPhotos: the sample photos are real files in the bucket, attached to sightings.
func TestBiosecurityPhotos(t *testing.T) {
	r, l, sch := bioRig(t)
	sig := tableByName(t, sch, "Pest sightings")
	photos := colByName(t, sig, "Photos")
	with := count(t, r, l, `SELECT count(*) FROM "Pest_sightings" WHERE "Photos" IS NOT NULL`)
	if with < 15 || with > 38 {
		t.Errorf("%d sightings with photos: want many, not all", with)
	}
	if n := count(t, r, l, `SELECT count(*) FROM _sys_file_refs WHERE table_id = '`+sig.ID+`' AND column_id = '`+photos.ID+`'`); n < with {
		t.Errorf("%d file refs for %d sightings with photos", n, with)
	}
	if n := count(t, r, l, jsonSQL(`SELECT count(*) FROM "Pest_sightings" WHERE json_array_length("Photos") > 3`,
		`SELECT count(*) FROM "Pest_sightings" WHERE jsonb_array_length("Photos") > 3`)); n != 0 {
		t.Errorf("%d sightings with more than 3 photos", n)
	}
	if n := count(t, r, l, jsonSQL(`SELECT count(*) FROM "Pest_sightings" WHERE json_array_length("Photos") >= 2`,
		`SELECT count(*) FROM "Pest_sightings" WHERE jsonb_array_length("Photos") >= 2`)); n == 0 {
		t.Error("no sighting has several photos")
	}
	if n := count(t, r, l, `SELECT count(*) FROM _sys_files WHERE state <> 'attached'`); n != 0 {
		t.Errorf("%d sample photos left pending", n)
	}
	var ids []string
	if err := l.Read(r.ctx, func(q dbx.Querier) error {
		rows, err := q.Query(r.ctx, `SELECT id FROM _sys_files ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if len(ids) < 6 {
		t.Fatalf("%d sample photos, want about 6", len(ids))
	}
	hashes := map[string]bool{}
	for _, id := range ids {
		m, err := r.b.Files.Get(r.ctx, l, id)
		if err != nil {
			t.Fatal(err)
		}
		hashes[m.SHA256] = true
		if m.ContentType != "image/jpeg" || m.Size < 1000 || m.Size > 30_000 || m.Width == nil || m.Height == nil || !m.HasThumbnail {
			t.Errorf("photo %s: %+v", m.Name, m)
		}
		f, _, err := r.b.Files.Open(r.ctx, l, id)
		if err != nil {
			t.Fatalf("photo %s is not in the bucket: %v", m.Name, err)
		}
		img, err := jpeg.Decode(f)
		_ = f.Close()
		if err != nil {
			t.Errorf("photo %s does not decode: %v", m.Name, err)
		} else if b := img.Bounds(); m.Width != nil && m.Height != nil && (b.Dx() != *m.Width || b.Dy() != *m.Height) {
			t.Errorf("photo %s: %v, metadata %dx%d", m.Name, b, *m.Width, *m.Height)
		}
	}
	if len(hashes) != len(ids) {
		t.Errorf("%d distinct photos among %d files: placeholders must look different", len(hashes), len(ids))
	}
}

// TestBiosecurityFormsAndReport: the forms and the report validate and hold the geo and attachment controls.
func TestBiosecurityFormsAndReport(t *testing.T) {
	r, l, sch := bioRig(t)
	fl, err := r.b.Forms.List(r.ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	exists := func(string) bool { return true }
	byName := map[string]forms.Form{}
	for _, f := range fl {
		full, err := r.b.Forms.Get(r.ctx, l, f.ID)
		if err != nil {
			t.Fatal(err)
		}
		byName[f.Name] = full
		tb, ok := sch.Table(full.SourceID)
		if !ok {
			t.Fatalf("form %q: source table gone", f.Name)
		}
		if err := forms.Validate(&full.Layout, forms.Source{Table: tb}, sch, exists); err != nil {
			t.Errorf("form %q: %v", f.Name, err)
		}
	}
	ctrls := func(f forms.Form, typ string) []*forms.Control {
		var out []*forms.Control
		for _, c := range f.Layout.AllControls() {
			if c.Type == typ {
				out = append(out, c)
			}
		}
		return out
	}
	rec, ok := byName["Record a sighting"]
	if !ok || rec.Layout.Width > 480 {
		t.Fatalf("Record a sighting should exist and be phone-sized: %+v", rec.Layout.Width)
	}
	maps, atts := ctrls(rec, "map"), ctrls(rec, "attachments")
	if len(maps) != 1 || len(atts) != 1 {
		t.Fatalf("Record a sighting: %d map and %d attachments controls", len(maps), len(atts))
	}
	zonesQ := savedByName(t, r, l, "Zone boundaries")
	if maps[0].Map == nil || maps[0].Map.OverlayQueryID != zonesQ.ID || maps[0].Map.OverlayGeoColumn != "Boundary" || maps[0].Map.OverlayLabelColumn != "Zone" {
		t.Errorf("the sighting map should overlay the zones: %+v", maps[0].Map)
	}
	if got := colByName(t, tableByName(t, sch, "Pest sightings"), "Photos").ID; atts[0].Bind.ColumnID != got {
		t.Error("the attachments control is not bound to Photos")
	}
	// The overlay query returns one boundary per zone.
	ov, err := r.b.Queries.RunSaved(r.ctx, l, query.Policy{}, zonesQ.ID, nil, 100)
	if err != nil || len(ov.Rows) != 3 || ov.Columns[1] != "Boundary" || ov.Types[1] != "geo_area" {
		t.Errorf("zone overlay: %+v %v", ov, err)
	}
	zone, ok := byName["Quarantine zone"]
	if !ok || len(ctrls(zone, "map")) != 1 {
		t.Fatalf("Quarantine zone form should have one map: %v", ok)
	}
	if m := ctrls(zone, "map")[0]; m.Map == nil || m.Map.OverlayQueryID != savedByName(t, r, l, "Sighting locations").ID {
		t.Errorf("the zone map should overlay the sightings: %+v", m.Map)
	}
	if len(byName) != 2 {
		t.Errorf("Northwind Invasives forms: %d, want Record a sighting and Quarantine zone", len(byName))
	}

	rl, err := r.b.Reports.List(r.ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	var rep reports.Report
	for _, x := range rl {
		if x.Name == "Biosecurity sightings report" {
			if rep, err = r.b.Reports.Get(r.ctx, l, x.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if rep.ID == "" {
		t.Fatal("Biosecurity sightings report missing")
	}
	tb, _ := sch.Table(rep.SourceID)
	if err := reports.Validate(&rep.ReportLayout, &rep.PageSetup, tb, nil); err != nil {
		t.Fatalf("report: %v", err)
	}
	var mapID, imgID string
	for _, c := range rep.ReportLayout.Bands.Detail.Controls {
		switch c.Type {
		case "map":
			mapID = c.ID
		case "image":
			imgID = c.ID
		}
	}
	if mapID == "" || imgID == "" {
		t.Fatal("the detail band needs a map and an image control")
	}
	// Run it: every sighting's map has a point and the image control the photo ids.
	var details, mapsWithPoint, imagesWithIDs int
	err = r.b.Reports.Run(r.ctx, reports.RunInput{Lease: l, Schema: sch, Report: rep, Caller: reports.Caller{UserID: r.owner, Builder: true}, Emit: func(in reports.Instance) error {
		if in.Band != "detail" {
			return nil
		}
		details++
		if v, ok := in.Values[mapID].(geo.Raw); ok && strings.Contains(string(v), `"Point"`) {
			mapsWithPoint++
		}
		if ids, ok := in.Values[imgID].([]string); ok && len(ids) > 0 {
			imagesWithIDs++
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if details < 40 || mapsWithPoint != details || imagesWithIDs < 15 {
		t.Errorf("report: %d detail rows, %d maps with a point, %d with photos", details, mapsWithPoint, imagesWithIDs)
	}
}

// TestWithoutFileServiceSkipsPhotos: a builder without a file service still builds the whole scenario.
func TestWithoutFileServiceSkipsPhotos(t *testing.T) {
	r := newRig(t)
	r.b.Files = nil
	res, err := r.b.Create(r.ctx, demo.Invasives, r.owner)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := r.wm.Acquire(r.ctx, res.WorkspaceID)
	defer l.Release()
	if n := count(t, r, l, `SELECT count(*) FROM "Pest_sightings" WHERE "Photos" IS NOT NULL`); n != 0 {
		t.Errorf("%d sightings with photos", n)
	}
	if n := count(t, r, l, `SELECT count(*) FROM "Pest_sightings"`); n != 40 {
		t.Errorf("%d sightings", n)
	}
}

// Every expression in both samples is inside the expression language
// (ADR 0013 Phase D), so nothing in them needs attention.
func TestSampleExpressionsParse(t *testing.T) {
	r := newRig(t)
	for _, kind := range []demo.Kind{demo.Traders, demo.Invasives} {
		res, err := r.b.Create(r.ctx, kind, r.owner)
		if err != nil {
			t.Fatal(err)
		}
		l, err := r.wm.Acquire(r.ctx, res.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		sch, err := r.b.Schemas.Get(r.ctx, l)
		l.Release()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, tb := range sch.Tables {
			for _, c := range tb.Columns {
				if c.DefaultSQL != "" || c.ValidationSQL != "" || c.Options.Expression != "" {
					n++
				}
				if c.ExprIssues != nil {
					t.Errorf("%v %s.%s: %v", kind, tb.PhysicalName, c.PhysicalName, c.ExprIssues)
				}
			}
		}
		if n == 0 {
			t.Errorf("%v has no expressions to check", kind)
		}
	}
}

// jsonSQL picks the test's SQLite or PostgreSQL spelling of a JSON query.
func jsonSQL(sqlite, postgres string) string {
	if wstest.Postgres() {
		return postgres
	}
	return sqlite
}

// asTime reads a stored date-time: RFC 3339 text on SQLite, a time on PostgreSQL.
func asTime(v any) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	s, _ := v.(string)
	return time.Parse(time.RFC3339, s)
}

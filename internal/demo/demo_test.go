package demo_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/artifacts"
	"abidata/internal/catalog"
	"abidata/internal/demo"
	"abidata/internal/forms"
	"abidata/internal/ids"
	"abidata/internal/jobs"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
	"abidata/internal/scripts"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
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
	adm, err := admin.Open(ctx, filepath.Join(dir, "system", "admin.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	owner := ids.New()
	if err := adm.CreateUser(ctx, admin.User{ID: owner, Email: "owner@example.com", DisplayName: "Owner", IsPlatformAdmin: true, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	wm, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
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
		_ = wm.Shutdown(ctx)
		_ = adm.Close()
	})
	return &rig{ctx: ctx, adm: adm, wm: wm, owner: owner, sched: sched, dir: dir, b: &demo.Builder{
		Admin: adm, Workspaces: wm, Schemas: schemas, Catalog: engine, Records: rec, Queries: qs, Scripts: ss,
		Forms:   &forms.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Reports: &reports.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Jobs:    &jobs.Service{Scripts: ss, Scheduler: sched},
	}}
}

func count(t *testing.T, r *rig, l *workspace.Lease, sql string) int {
	t.Helper()
	var n int
	if err := l.Read(r.ctx, func(q sqlitex.Querier) error { return q.QueryRow(r.ctx, sql).Scan(&n) }); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func TestCreateBuildsNorthwind(t *testing.T) {
	r := newRig(t)
	res, err := r.b.Create(r.ctx, r.owner)
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
	if err := l.Read(r.ctx, func(q sqlitex.Querier) error {
		return q.QueryRow(r.ctx, `SELECT max("Order_date"), min("Order_date") FROM "Orders"`).Scan(&newest, &oldest)
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
		out, err := r.b.Queries.RunSaved(r.ctx, l, query.Policy{}, s.ID, nil, 100)
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
	if err != nil || len(fl) != 1 {
		t.Fatalf("forms: %v %v", fl, err)
	}
	rl, err := r.b.Reports.List(r.ctx, l)
	if err != nil || len(rl) != 1 {
		t.Fatalf("reports: %v %v", rl, err)
	}
	// Group and report footer totals carry captions.
	rep, err := r.b.Reports.Get(r.ctx, l, rl[0].ID)
	if err != nil {
		t.Fatal(err)
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
	res, err := r.b.Create(r.ctx, r.owner)
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
	if err := l.Write(r.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(r.ctx, `UPDATE "Products" SET "Low_stock" = 0`)
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
	if n := count(t, r, l, `SELECT count(*) FROM "Products" WHERE "Low_stock" = 1`); n == 0 {
		t.Error("job did not set low-stock flags")
	}
	if n := count(t, r, l, `SELECT count(*) FROM "Products" WHERE "Low_stock" = 1 AND "Discontinued" = 1`); n != 0 {
		t.Error("discontinued products must not be flagged")
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	r := newRig(t)
	first, err := r.b.Create(r.ctx, r.owner)
	if err != nil || !first.Created {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := r.b.Create(r.ctx, r.owner)
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

func TestDeterministicContent(t *testing.T) {
	fixed := func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }
	var sums [2]string
	for i := range sums {
		r := newRig(t)
		r.b.Now = fixed
		res, err := r.b.Create(r.ctx, r.owner)
		if err != nil {
			t.Fatal(err)
		}
		l, _ := r.wm.Acquire(r.ctx, res.WorkspaceID)
		if err := l.Read(r.ctx, func(q sqlitex.Querier) error {
			return q.QueryRow(r.ctx, `SELECT group_concat("Order_ref" || "Order_date" || "Total" || "Status", ',') FROM (SELECT * FROM "Orders" ORDER BY id)`).Scan(&sums[i])
		}); err != nil {
			t.Fatal(err)
		}
		l.Release()
	}
	if sums[0] == "" || sums[0] != sums[1] {
		t.Fatal("demo content differs between builds with the same clock")
	}
}

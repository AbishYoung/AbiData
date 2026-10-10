package reports

import (
	"runtime"
	"testing"
	"time"

	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Phase 3 security review H5 (fixed; regression). A report run materialised every source row in Go maps before streaming, bounded only by
// the SizeOf estimate (256 MiB of raw values, excluding per-row/per-map
// overhead), and nothing caps concurrent runs. Any member (viewers included)
// can POST /reports/{id}/run in parallel, and a slow-reading client keeps each
// run's rows alive (no write timeout on the stream), so a few requests exhaust
// the node's memory for every tenant.
func TestSecReviewReportRunHeap(t *testing.T) {
	f := setup(t)
	if err := f.l.Write(f.ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(f.ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 190000)
			INSERT INTO Sales(Region, Rep, Amount, Qty, Day) SELECT 'R' || (i % 20), printf('%.240c', 'x') || i, i, i % 7, '2026-01-01' FROM n`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, during runtime.MemStats
	runtime.ReadMemStats(&before)
	sampled := false
	lay := ReportLayout{Width: 700, Bands: Bands{Detail: Band{Height: 20, Controls: []ReportControl{
		{ID: "rep", Name: "Rep", Type: "textbox", W: 100, H: 20, Bind: &Bind{ColumnID: col(f.sales, "Rep")}},
		{ID: "region", Name: "Region", Type: "textbox", X: 110, W: 100, H: 20, Bind: &Bind{ColumnID: col(f.sales, "Region")}},
	}}}}
	rep, err := f.svc.Put(f.ctx, f.l, "", 0, ReportInput{Name: "Big", SourceKind: "table", SourceID: f.sales.ID, ReportLayout: lay,
		PageSetup: PageSetup{Paper: "letter", Orientation: "portrait"}}, "u")
	if err != nil {
		t.Fatal(err)
	}
	sch, _ := f.svc.Schemas.Get(f.ctx, f.l)
	start := time.Now()
	err = f.svc.Run(f.ctx, RunInput{Lease: f.l, Schema: sch, Report: rep, Caller: Caller{TZ: time.UTC},
		Emit: func(Instance) error {
			if !sampled {
				sampled = true
				runtime.GC()
				runtime.ReadMemStats(&during)
			}
			return nil
		}})
	live := int64(during.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("190k-row report: live heap retained while streaming = %d MiB (took %v, err=%v)", live>>20, time.Since(start), err)
	if live > 256<<20 {
		t.Fatalf("a report run retained %d MiB", live>>20)
	}
}

func TestReportRunSlots(t *testing.T) {
	ws := ids.NewWorkspaceID()
	var rel []func()
	for range MaxRunsPerWorkspace {
		r, err := slots.acquire(ws)
		if err != nil {
			t.Fatal(err)
		}
		rel = append(rel, r)
	}
	if _, err := slots.acquire(ws); err == nil {
		t.Fatal("a workspace exceeded its concurrent report runs")
	}
	r, err := slots.acquire(ids.NewWorkspaceID())
	if err != nil {
		t.Fatalf("another workspace was refused: %v", err)
	}
	r()
	for _, r := range rel {
		r()
	}
	if _, err := slots.acquire(ws); err != nil {
		t.Fatalf("slots were not released: %v", err)
	}
}

func TestFoldCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"abc", "ABC", 0}, {"a", "B", -1}, {"Zeta", "alpha", 1}, {"ab", "abc", -1}, {"Éclair", "éclair", 0}} {
		if got := foldCompare(c.a, c.b); got != c.want {
			t.Errorf("foldCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// H5 (fixed; regression): Lua group keys were sorted in Go with compare(), which fell back to
// strings.ToLower(fmt.Sprint(a)) for every comparison and never checks ctx. A
// group expression that returns one shared 1 MiB upper-case string (free in
// Lua) makes each comparison copy and lower-case 2 MiB, so sorting even a few
// thousand rows burns CPU long after the 30 s Lua budget has ended.
func TestSecReviewLuaGroupSortCPU(t *testing.T) {
	f := setup(t)
	if err := f.l.Write(f.ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(f.ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 1000)
			INSERT INTO Sales(Region, Rep, Amount, Qty, Day) SELECT 'R', 'r' || i, i, i, '2026-01-01' FROM n`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lay := ReportLayout{Width: 700, Groups: []Group{{LuaExpr: "K", ReportSort: "asc"}},
		Bands: Bands{Detail: Band{Height: 20, Controls: []ReportControl{
			{ID: "rep", Name: "Rep", Type: "textbox", W: 100, H: 20, Bind: &Bind{ColumnID: col(f.sales, "Rep")}}}}}}
	rep, err := f.svc.Put(f.ctx, f.l, "", 0, ReportInput{Name: "Sort", SourceKind: "table", SourceID: f.sales.ID, ReportLayout: lay,
		ModuleSource: `K = string.rep("X", 1024 * 1024)`, PageSetup: PageSetup{Paper: "letter", Orientation: "portrait"}}, "u")
	if err != nil {
		t.Fatal(err)
	}
	sch, _ := f.svc.Schemas.Get(f.ctx, f.l)
	start := time.Now()
	err = f.svc.Run(f.ctx, RunInput{Lease: f.l, Schema: sch, Report: rep, Caller: Caller{TZ: time.UTC}, Emit: func(Instance) error { return nil }})
	t.Logf("1,000-row report with a 1 MiB Lua group key took %v (err=%v)", time.Since(start), err)
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("an oversized Lua group key was accepted or slow: %v", err)
	}
}

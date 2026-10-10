package reports

import (
	"os"
	"runtime"
	"testing"
	"time"

	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Phase 3 security review (gated: ABIDATA_SECREVIEW=1). A report run
// materialises every source row in Go maps before streaming, bounded only by
// the SizeOf estimate (256 MiB of raw values, excluding per-row/per-map
// overhead), and nothing caps concurrent runs. Any member (viewers included)
// can POST /reports/{id}/run in parallel, and a slow-reading client keeps each
// run's rows alive (no write timeout on the stream), so a few requests exhaust
// the node's memory for every tenant.
func TestSecReviewReportRunHeap(t *testing.T) {
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("set ABIDATA_SECREVIEW=1")
	}
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
}

// Lua group keys are sorted in Go with compare(), which falls back to
// strings.ToLower(fmt.Sprint(a)) for every comparison and never checks ctx. A
// group expression that returns one shared 1 MiB upper-case string (free in
// Lua) makes each comparison copy and lower-case 2 MiB, so sorting even a few
// thousand rows burns CPU long after the 30 s Lua budget has ended.
func TestSecReviewLuaGroupSortCPU(t *testing.T) {
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("set ABIDATA_SECREVIEW=1")
	}
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
}

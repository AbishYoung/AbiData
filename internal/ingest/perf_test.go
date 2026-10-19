package ingest

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

var raceEnabled = false

// TestPerfUpsertUnderInteractiveWrites is the Gate 5 import check: a 200k-row
// CSV upsert (half updates, half inserts) runs while 20 goroutines save single
// rows; interactive write p99 must stay under 50 ms.
func TestPerfUpsertUnderInteractiveWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("perf test")
	}
	e := newEnv(t)
	tb := e.table("Parts", catalog.ColumnSpec{DisplayName: "SKU", Type: catalog.ShortText}, catalog.ColumnSpec{DisplayName: "Qty", Type: catalog.Integer},
		catalog.ColumnSpec{DisplayName: "Price", Type: catalog.Currency}, catalog.ColumnSpec{DisplayName: "Note", Type: catalog.ShortText})
	e.unique(tb, "SKU")
	live := e.table("Live", catalog.ColumnSpec{DisplayName: "Who", Type: catalog.ShortText})
	total := 200_000
	if raceEnabled {
		total = 40_000
	}
	if err := e.l.Write(e.ctx, workspace.Background, func(tx dbx.Tx) error {
		_, err := tx.Exec(e.ctx, fmt.Sprintf(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < %d)
			INSERT INTO "Parts"("SKU", "Qty", "Price", "Note") SELECT 'SKU-' || i, 0, 0, 'old' FROM n`, total/2))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("sku,qty,price,note\n")
	for i := 1; i <= total; i++ { // SKU-1..100000 update, 100001..200000 insert
		fmt.Fprintf(&sb, "SKU-%d,%d,%d.25,imported row %d\n", i, i%97, i%1000, i)
	}
	m := e.upload("parts.csv", sb.String(), UploadOptions{})
	if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeUpsert, "SKU", "SKU", "Qty", "Price", "Note")); err != nil {
		t.Fatal(err)
	}
	if v, err := e.svc.Validate(e.ctx, e.l, m.ID); err != nil || v.ErrorCount != 0 || v.RowsChecked != int64(total) {
		t.Fatalf("%v %+v", err, v)
	}

	rec := &records.Service{Schemas: e.cache}
	who := col2(t, live, "Who").ID
	// Think time between a user's saves. The gate is strict priority, so writers that
	// keep it permanently busy would starve any background writer; under the race
	// detector each save takes far longer, so users are slower to keep utilisation below 1.
	think := 3 * time.Millisecond
	if raceEnabled {
		think = 60 * time.Millisecond
	}
	var stop atomic.Bool
	var mu sync.Mutex
	var lat []time.Duration
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			l, err := e.wm.Acquire(e.ctx, e.ws) // one lease per "request", as in the HTTP layer
			if err != nil {
				t.Error(err)
				return
			}
			defer l.Release()
			for i := 0; !stop.Load(); i++ {
				start := time.Now()
				_, err := rec.Insert(e.ctx, l, live.ID, map[string]any{who: fmt.Sprintf("g%d-%d", g, i)}, records.Actor{}, nil)
				d := time.Since(start)
				if err != nil {
					t.Errorf("interactive write: %v", err)
					return
				}
				mu.Lock()
				lat = append(lat, d)
				mu.Unlock()
				time.Sleep(think)
			}
		}(g)
	}
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	op, err := e.svc.StartCommit(e.ctx, e.l, m.ID, "u")
	if err != nil {
		t.Fatal(err)
	}
	op = e.wait(op)
	took := time.Since(start)
	stop.Store(true)
	wg.Wait()
	r := result(t, op)
	if r.Inserted != int64(total/2) || r.Updated != int64(total/2) || r.Skipped != 0 {
		t.Fatalf("%+v", r)
	}
	if e.count(tb) != int64(total) {
		t.Fatalf("rows %d", e.count(tb))
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p := func(q float64) time.Duration { return lat[int(float64(len(lat)-1)*q)] }
	t.Logf("import of %d rows took %s (%.0f rows/s); %d interactive writes: p50 %s p99 %s max %s (race=%v)",
		total, took, float64(total)/took.Seconds(), len(lat), p(0.5), p(0.99), lat[len(lat)-1], raceEnabled)
	limit := 50 * time.Millisecond
	if raceEnabled {
		// A 2,000-row chunk holds the writer for about a second under the race
		// detector, so only gross starvation is caught there; the 50 ms budget is
		// checked in normal runs.
		limit = 5 * time.Second
	}
	if len(lat) < 100 || p(0.99) > limit {
		t.Fatalf("interactive write p99 %s exceeds %s (n=%d)", p(0.99), limit, len(lat))
	}
}

package forms

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"abidata/internal/records"
)

var raceEnabled = false

// TestPerfEventRoundTrip is the Gate 3 perf check: with 50 concurrent users,
// form event server time p95 < 150 ms.
func TestPerfEventRoundTrip(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("perf test (not meaningful under -short or -race)")
	}
	f := setup(t)
	orderForm, _ := f.createForms(t)
	_, _ = f.svc.Records.Insert(f.ctx, f.l, f.cust.ID, map[string]any{col(f.cust, "Name"): "Ada"}, records.Actor{}, nil)
	order, err := f.svc.Records.Insert(f.ctx, f.l, f.orders.ID, map[string]any{col(f.orders, "Customer"): json.Number("1")}, records.Actor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		_, _ = f.svc.Records.Insert(f.ctx, f.l, f.lines.ID, map[string]any{col(f.lines, "OrderId"): json.Number("1"),
			col(f.lines, "Qty"): json.Number("2"), col(f.lines, "UnitPrice"): "9.99"}, records.Actor{}, nil)
	}
	var mu sync.Mutex
	var lat []time.Duration
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for u := 0; u < 50; u++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				start := time.Now()
				resp, err := f.svc.Event(f.ctx, f.l, f.editor, orderForm.ID, EventRequest{Event: "load", RecordID: order.ID, RecordVersion: order.Version, Record: order.Fields})
				d := time.Since(start)
				if err != nil {
					errs <- err
					return
				}
				if len(resp.Patches) != 1 || resp.Calc["o5"] == nil {
					errs <- err
					return
				}
				mu.Lock()
				lat = append(lat, d)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	slices.Sort(lat)
	p50, p95 := lat[len(lat)/2], lat[len(lat)*95/100]
	t.Logf("form event (handler + calc), 50 concurrent users × 20: p50=%v p95=%v max=%v", p50, p95, lat[len(lat)-1])
	if p95 > 150*time.Millisecond {
		t.Fatalf("p95 %v exceeds 150 ms", p95)
	}
}

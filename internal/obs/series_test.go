package obs

import (
	"context"
	"math"
	"testing"
	"time"
)

func mkCum(requests float64) cum {
	c := cum{latBuckets: make([]float64, len(latencyBuckets))}
	c.req[0] = requests
	c.latCount = requests
	// Every request lands in the 2.5-5 ms bucket (index 3) and above.
	for i := 3; i < len(c.latBuckets); i++ {
		c.latBuckets[i] = requests
	}
	return c
}

func TestRingWrap(t *testing.T) {
	r := newRing[int](3)
	for i := 1; i <= 5; i++ {
		r.push(i)
	}
	got := r.items()
	if len(got) != 3 || got[0] != 3 || got[2] != 5 {
		t.Fatalf("wrapped ring %v", got)
	}
	if e := newRing[int](3).items(); len(e) != 0 {
		t.Fatalf("empty ring %v", e)
	}
}

func TestSamplerFineWrapAndRates(t *testing.T) {
	s := NewSampler(nil)
	t0 := time.Unix(1_000_000, 0)
	total := FineCap + 20
	for i := 0; i <= total; i++ {
		// 100 requests per 10 s = 10/s.
		s.ingest(t0.Add(time.Duration(i)*SampleInterval), mkCum(float64(i*100)), gauges{heap: 5, goroutines: 7, openWS: 2})
	}
	pts := s.Series(Range1h)
	if len(pts) != FineCap {
		t.Fatalf("fine points %d, want %d", len(pts), FineCap)
	}
	if pts[0].T >= pts[len(pts)-1].T {
		t.Fatal("points not oldest first")
	}
	last := pts[len(pts)-1]
	if math.Abs(last.Req2xx-10) > 1e-9 || last.Goroutines != 7 || last.OpenWorkspaces != 2 {
		t.Fatalf("last point %+v", last)
	}
	if last.P95 <= 0 || last.P95 > 5 || math.IsNaN(last.P50) {
		t.Fatalf("latency p50=%v p95=%v (all samples are in the 2.5-5 ms bucket)", last.P50, last.P95)
	}
}

func TestSamplerNoTrafficLatencyIsNaN(t *testing.T) {
	s := NewSampler(nil)
	t0 := time.Unix(1_000_000, 0)
	s.ingest(t0, mkCum(0), gauges{})
	s.ingest(t0.Add(SampleInterval), mkCum(0), gauges{})
	pts := s.Series(Range1h)
	if len(pts) != 1 || !math.IsNaN(pts[0].P50) || pts[0].Req2xx != 0 {
		t.Fatalf("%+v", pts)
	}
}

func TestSamplerRollup(t *testing.T) {
	s := NewSampler(nil)
	t0 := time.Unix(1_000_000, 0)
	s.ingest(t0, mkCum(0), gauges{})
	// 12 samples = two minutes. Heap alternates 10 / 20 so each minute averages 15.
	for i := 1; i <= 12; i++ {
		heap := 10.0
		if i%2 == 0 {
			heap = 20
		}
		c := mkCum(float64(i * 60)) // 60 requests per 10 s = 6/s
		c.scriptOK = float64(i)
		s.ingest(t0.Add(time.Duration(i)*SampleInterval), c, gauges{heap: heap})
	}
	co := s.Series(Range24h)
	if len(co) != 2 {
		t.Fatalf("coarse points %d, want 2", len(co))
	}
	for _, p := range co {
		if math.Abs(p.Req2xx-6) > 1e-9 || p.Heap != 15 || p.ScriptOK != 6 {
			t.Fatalf("rollup %+v", p)
		}
	}
	if co[1].T-co[0].T != 60 {
		t.Fatalf("rollup spacing %d", co[1].T-co[0].T)
	}
}

func TestSamplerCoarseWrap(t *testing.T) {
	s := NewSampler(nil)
	t0 := time.Unix(1_000_000, 0)
	s.ingest(t0, mkCum(0), gauges{})
	for i := 1; i <= (CoarseCap+5)*coarseEvery; i++ {
		s.ingest(t0.Add(time.Duration(i)*SampleInterval), mkCum(float64(i)), gauges{})
	}
	if n := len(s.Series(Range24h)); n != CoarseCap {
		t.Fatalf("coarse %d, want %d", n, CoarseCap)
	}
}

func TestCounterResetNeverNegative(t *testing.T) {
	s := NewSampler(nil)
	t0 := time.Unix(1_000_000, 0)
	s.ingest(t0, mkCum(500), gauges{})
	s.ingest(t0.Add(SampleInterval), mkCum(10), gauges{})
	if p := s.Series(Range1h)[0]; p.Req2xx < 0 {
		t.Fatalf("negative rate %v", p.Req2xx)
	}
}

func TestFailuresNewestFirstAndBounded(t *testing.T) {
	s := NewSampler(nil)
	for i := range failureCap + 10 {
		kind := "job"
		if i%2 == 1 {
			kind = "backup"
		}
		s.RecordFailure(Failure{Kind: kind, At: time.Unix(int64(i), 0)})
	}
	all := s.Failures("", 1000)
	if len(all) != failureCap || all[0].At.Unix() != int64(failureCap+9) {
		t.Fatalf("%d failures, newest %v", len(all), all[0].At)
	}
	for _, f := range s.Failures("backup", 5) {
		if f.Kind != "backup" {
			t.Fatal("kind filter")
		}
	}
}

func TestSamplerReadsMetrics(t *testing.T) {
	m := NewMetrics()
	s := NewSampler(m)
	m.HTTPRequests.WithLabelValues("GET", "/api/v1/x", "200").Observe(0.002)
	m.HTTPRequests.WithLabelValues("GET", "/api/v1/auth/login", "401").Observe(0.002)
	m.HTTPRequests.WithLabelValues("GET", "/api/v1/w/{ws}/events", "200").Observe(30)
	m.JobRuns.WithLabelValues("failed").Inc()
	m.EgressRequests.WithLabelValues("denied").Inc()
	m.ScriptRuns.WithLabelValues("job", "error").Inc()
	m.OpenWorkspaces.Set(3)
	c, open := m.readCum()
	if c.req[0] != 1 || c.req[2] != 1 || c.latCount != 2 || c.loginFail != 1 || c.jobFail != 1 || c.egDenied != 1 || c.scriptErr != 1 || open != 3 {
		t.Fatalf("%+v open=%v", c, open)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx) // returns promptly once ctx is done
}

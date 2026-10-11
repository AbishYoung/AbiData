package obs

import (
	"context"
	"math"
	"runtime"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// The sampler keeps the admin dashboard's time series in memory: one point per
// SampleInterval for the last hour, and one point per minute for the last day.
// Memory is fixed (two ring buffers of small structs); nothing is persisted, so
// the history restarts empty with the process.
const (
	SampleInterval = 10 * time.Second
	FineCap        = 360  // 1 h at 10 s
	CoarseCap      = 1440 // 24 h at 1 min
	coarseEvery    = 6    // fine samples per coarse point
	failureCap     = 50
)

// Point is one sample. Counters are deltas over the sampled interval (except
// the Req* fields, which are per-second rates); gauges are the value at the
// sample (the mean over the minute for coarse points). P50/P95 are
// milliseconds and NaN when the interval saw no requests.
type Point struct {
	T int64 // unix seconds at the end of the interval

	Req2xx, Req3xx, Req4xx, Req5xx float64 // requests per second by status class
	P50, P95                       float64 // latency in ms (NaN: no requests)
	RateLimited                    float64 // 429 responses in the interval
	LoginFailures                  float64 // failed logins (4xx on the login route) in the interval

	Heap, Goroutines, OpenWorkspaces float64

	ScriptOK, ScriptError                  float64
	JobSucceeded, JobFailed, JobOther      float64
	EgressAllowed, EgressDenied, EgressErr float64
}

// ring is a fixed-capacity buffer that overwrites its oldest entry.
type ring[T any] struct {
	buf  []T
	head int // next write index
	n    int
}

func newRing[T any](capacity int) *ring[T] { return &ring[T]{buf: make([]T, capacity)} }

func (r *ring[T]) push(v T) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// items returns the entries oldest first.
func (r *ring[T]) items() []T {
	out := make([]T, 0, r.n)
	start := (r.head - r.n + len(r.buf)) % len(r.buf)
	for i := range r.n {
		out = append(out, r.buf[(start+i)%len(r.buf)])
	}
	return out
}

// cum is the cumulative state of the counters the series are derived from.
type cum struct {
	req                        [4]float64 // 2xx, 3xx, 4xx, 5xx
	limited                    float64
	loginFail                  float64
	latBuckets                 []float64 // cumulative counts per latencyBuckets bound
	latCount                   float64
	scriptOK, scriptErr        float64
	jobOK, jobFail, jobOther   float64
	egAllowed, egDenied, egErr float64
}

// gauges are the instantaneous values.
type gauges struct{ heap, goroutines, openWS float64 }

// Failure is a recorded job or backup failure (since process start).
type Failure struct {
	At          time.Time
	Kind        string // "job" or "backup"
	WorkspaceID string
	Subject     string // job id, or backup kind
	SubjectName string // job name resolved when recorded; empty for backups or if the job no longer exists
	Status      string
	Message     string
}

// Sampler samples a Metrics set into ring buffers.
type Sampler struct {
	m   *Metrics
	now func() time.Time

	mu       sync.RWMutex
	fine     *ring[Point]
	coarse   *ring[Point]
	prev     cum
	prevT    time.Time
	havePrev bool
	// Minute accumulator.
	minPrev  cum
	minT     time.Time
	minN     int
	minGauge gauges
	failures *ring[Failure]
}

// NewSampler returns a sampler over m (m may be nil: the series stay empty).
func NewSampler(m *Metrics) *Sampler {
	return &Sampler{m: m, now: time.Now, fine: newRing[Point](FineCap), coarse: newRing[Point](CoarseCap),
		failures: newRing[Failure](failureCap)}
}

// Run samples every SampleInterval until ctx ends.
func (s *Sampler) Run(ctx context.Context) {
	t := time.NewTicker(SampleInterval)
	defer t.Stop()
	s.Sample()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sample()
		}
	}
}

// Sample reads the current counters and appends a point.
func (s *Sampler) Sample() {
	if s.m == nil {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	c, open := s.m.readCum()
	s.ingest(s.now(), c, gauges{heap: float64(ms.HeapAlloc), goroutines: float64(runtime.NumGoroutine()), openWS: open})
}

// ingest appends the point for the interval ending at now.
func (s *Sampler) ingest(now time.Time, c cum, g gauges) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.havePrev {
		s.prev, s.prevT, s.havePrev = c, now, true
		s.minPrev, s.minT = c, now
		return
	}
	dt := now.Sub(s.prevT).Seconds()
	if dt <= 0 {
		return
	}
	p := pointFrom(s.prev, c, g, now, dt)
	s.fine.push(p)
	s.prev, s.prevT = c, now

	s.minN++
	s.minGauge.heap += g.heap
	s.minGauge.goroutines += g.goroutines
	s.minGauge.openWS += g.openWS
	if s.minN >= coarseEvery {
		n := float64(s.minN)
		mg := gauges{heap: s.minGauge.heap / n, goroutines: s.minGauge.goroutines / n, openWS: s.minGauge.openWS / n}
		s.coarse.push(pointFrom(s.minPrev, c, mg, now, now.Sub(s.minT).Seconds()))
		s.minPrev, s.minT, s.minN, s.minGauge = c, now, 0, gauges{}
	}
}

func nonNeg(v float64) float64 { return math.Max(v, 0) }

func pointFrom(a, b cum, g gauges, now time.Time, dt float64) Point {
	p := Point{T: now.Unix(),
		Req2xx: nonNeg(b.req[0]-a.req[0]) / dt, Req3xx: nonNeg(b.req[1]-a.req[1]) / dt,
		Req4xx: nonNeg(b.req[2]-a.req[2]) / dt, Req5xx: nonNeg(b.req[3]-a.req[3]) / dt,
		RateLimited: nonNeg(b.limited - a.limited), LoginFailures: nonNeg(b.loginFail - a.loginFail),
		Heap: g.heap, Goroutines: g.goroutines, OpenWorkspaces: g.openWS,
		ScriptOK: nonNeg(b.scriptOK - a.scriptOK), ScriptError: nonNeg(b.scriptErr - a.scriptErr),
		JobSucceeded: nonNeg(b.jobOK - a.jobOK), JobFailed: nonNeg(b.jobFail - a.jobFail), JobOther: nonNeg(b.jobOther - a.jobOther),
		EgressAllowed: nonNeg(b.egAllowed - a.egAllowed), EgressDenied: nonNeg(b.egDenied - a.egDenied),
		EgressErr: nonNeg(b.egErr - a.egErr),
		P50:       math.NaN(), P95: math.NaN()}
	delta := make([]float64, len(b.latBuckets))
	for i := range delta {
		var prev float64
		if i < len(a.latBuckets) {
			prev = a.latBuckets[i]
		}
		delta[i] = nonNeg(b.latBuckets[i] - prev)
	}
	total := nonNeg(b.latCount - a.latCount)
	if total > 0 {
		p.P50 = quantileMS(delta, total, 0.5)
		p.P95 = quantileMS(delta, total, 0.95)
	}
	return p
}

// quantileMS estimates a quantile in milliseconds from cumulative bucket
// counts (linear interpolation inside the bucket, like Prometheus).
func quantileMS(cumCounts []float64, total, q float64) float64 {
	rank := q * total
	var lower, prevCount float64
	for i, upper := range latencyBuckets {
		if i >= len(cumCounts) {
			break
		}
		if cumCounts[i] >= rank {
			in := cumCounts[i] - prevCount
			if in <= 0 {
				return upper * 1000
			}
			return (lower + (upper-lower)*(rank-prevCount)/in) * 1000
		}
		lower, prevCount = upper, cumCounts[i]
	}
	// Beyond the last bucket: report its bound.
	return latencyBuckets[len(latencyBuckets)-1] * 1000
}

// Range selects which ring a query reads.
type Range string

const (
	Range1h  Range = "1h"
	Range24h Range = "24h"
)

// IntervalSeconds is the spacing of points in the range.
func (r Range) IntervalSeconds() int {
	if r == Range24h {
		return 60
	}
	return int(SampleInterval / time.Second)
}

// Series returns the points of the range, oldest first.
func (s *Sampler) Series(r Range) []Point {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r == Range24h {
		return s.coarse.items()
	}
	return s.fine.items()
}

// RecordFailure remembers a job or backup failure for the dashboard.
func (s *Sampler) RecordFailure(f Failure) {
	if f.At.IsZero() {
		f.At = s.now()
	}
	s.mu.Lock()
	s.failures.push(f)
	s.mu.Unlock()
}

// Failures returns recorded failures of kind ("" for all), newest first, at most limit.
func (s *Sampler) Failures(kind string, limit int) []Failure {
	s.mu.RLock()
	all := s.failures.items()
	s.mu.RUnlock()
	var out []Failure
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		if kind == "" || all[i].Kind == kind {
			out = append(out, all[i])
		}
	}
	return out
}

// readCum reads the cumulative counters out of the Prometheus registry.
func (m *Metrics) readCum() (cum, float64) {
	var c cum
	var open float64
	fams, err := m.reg.Gather()
	if err != nil && len(fams) == 0 {
		return c, 0
	}
	c.latBuckets = make([]float64, len(latencyBuckets))
	for _, f := range fams {
		switch f.GetName() {
		case namespace + "_http_request_duration_seconds":
			for _, mt := range f.GetMetric() {
				m.addHTTP(&c, mt)
			}
		case namespace + "_script_runs_total":
			for _, mt := range f.GetMetric() {
				if label(mt, "outcome") == "ok" {
					c.scriptOK += mt.GetCounter().GetValue()
				} else {
					c.scriptErr += mt.GetCounter().GetValue()
				}
			}
		case namespace + "_job_runs_total":
			for _, mt := range f.GetMetric() {
				v := mt.GetCounter().GetValue()
				switch label(mt, "status") {
				case "succeeded":
					c.jobOK += v
				case "failed", "timed_out":
					c.jobFail += v
				default:
					c.jobOther += v
				}
			}
		case namespace + "_egress_requests_total":
			for _, mt := range f.GetMetric() {
				v := mt.GetCounter().GetValue()
				switch label(mt, "outcome") {
				case "allowed":
					c.egAllowed += v
				case "denied":
					c.egDenied += v
				default:
					c.egErr += v
				}
			}
		case namespace + "_open_workspaces":
			for _, mt := range f.GetMetric() {
				open = mt.GetGauge().GetValue()
			}
		}
	}
	return c, open
}

func label(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

// addHTTP folds one request histogram series into c. Long-lived streams
// (events, live logs) are skipped: their duration is the connection's.
func (m *Metrics) addHTTP(c *cum, mt *dto.Metric) {
	route, status := label(mt, "route"), label(mt, "status")
	if route == "/api/v1/w/{ws}/events" || route == "/api/v1/admin/logs/stream" {
		return
	}
	h := mt.GetHistogram()
	n := float64(h.GetSampleCount())
	if len(status) > 0 && status[0] >= '2' && status[0] <= '5' {
		c.req[status[0]-'2'] += n
	}
	if status == "429" {
		c.limited += n
	}
	if route == "/api/v1/auth/login" && len(status) > 0 && status[0] == '4' {
		c.loginFail += n
	}
	c.latCount += n
	// Prometheus buckets are cumulative; the +Inf bucket is implicit.
	bs := h.GetBucket()
	for i := range c.latBuckets {
		if i < len(bs) {
			c.latBuckets[i] += float64(bs[i].GetCumulativeCount())
		}
	}
}

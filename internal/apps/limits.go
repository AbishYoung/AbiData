package apps

import (
	"sync"
	"time"
)

// Budget is a sustained rate with a burst allowance.
type Budget struct {
	PerMinute int
	Burst     int
}

// Request budgets of public apps (ADR 0014), per process.
var (
	Reads      = Budget{PerMinute: 120, Burst: 60}  // per client address per app
	Submits    = Budget{PerMinute: 10, Burst: 5}    // per client address per app
	AppSubmits = Budget{PerMinute: 300, Burst: 100} // per app, all addresses together
)

const (
	limiterSweepAfterIdle = 10 * time.Minute
	limiterSweepAtBuckets = 4096
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a set of token buckets keyed by string. Each bucket holds its
// budget's burst and refills continuously at its rate.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

// NewLimiter returns an empty limiter.
func NewLimiter() *Limiter {
	return &Limiter{buckets: map[string]*bucket{}, now: time.Now}
}

// Allow spends one request from key's bucket. When the bucket is empty it
// returns false and how long until one is available.
func (l *Limiter) Allow(key string, b Budget) (bool, time.Duration) {
	capacity := float64(max(1, b.Burst))
	perSec := float64(b.PerMinute) / 60
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > limiterSweepAtBuckets {
		for k, b := range l.buckets {
			if now.Sub(b.last) > limiterSweepAfterIdle {
				delete(l.buckets, k)
			}
		}
	}
	bk, ok := l.buckets[key]
	if !ok {
		bk = &bucket{tokens: capacity, last: now}
		l.buckets[key] = bk
	}
	if el := now.Sub(bk.last).Seconds(); el > 0 {
		bk.tokens = min(capacity, bk.tokens+el*perSec)
		bk.last = now
	}
	if bk.tokens < 1 {
		return false, time.Duration((1 - bk.tokens) / perSec * float64(time.Second))
	}
	bk.tokens--
	return true, 0
}

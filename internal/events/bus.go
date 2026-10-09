// Package events is the per-workspace event bus with resumable delivery
// (brief <realtime>): a monotonic sequence, a true circular ring for replay,
// subscribe-before-replay ordering, and slow consumers dropped (never silently
// losing events — the client reconnects with Last-Event-ID).
package events

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is one published message.
type Event struct {
	ID   uint64          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Event types.
const (
	RowsChanged       = "rows.changed"
	TableInvalidated  = "table.invalidated"
	SchemaChanged     = "schema.changed"
	DesignChanged     = "design.changed"
	JobRun            = "job.run"
	OperationProgress = "operation.progress"
	Reset             = "reset"
)

// DefaultRing and SubBuffer are the brief's sizes.
const (
	DefaultRing = 10000
	SubBuffer   = 256
)

// Bus is a single stream (one per workspace, plus the admin log stream).
type Bus struct {
	mu    sync.Mutex
	seq   uint64
	ring  []Event
	start int // index of the oldest event
	n     int // events held
	subs  map[*Sub]struct{}
}

// NewBus returns a bus retaining up to ring events for replay.
func NewBus(ring int) *Bus {
	if ring <= 0 {
		ring = DefaultRing
	}
	// IDs start from the wall clock (ms × 1000) so they keep increasing across
	// restarts: a client resuming with an ID from before a restart gets "reset"
	// instead of silently missing events.
	return &Bus{ring: make([]Event, ring), subs: map[*Sub]struct{}{}, seq: uint64(time.Now().UnixMilli()) * 1000}
}

// Sub is a live subscription.
type Sub struct {
	C       chan Event
	b       *Bus
	dropped bool
	closed  bool
}

// Dropped reports whether the subscriber was cut off for falling behind.
func (s *Sub) Dropped() bool {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	return s.dropped
}

// Close unsubscribes. Safe to call more than once.
func (s *Sub) Close() {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if !s.closed {
		s.closed = true
		delete(s.b.subs, s)
		close(s.C)
	}
}

// Publish appends an event and fans it out. data is JSON-encoded.
func (b *Bus) Publish(typ string, data any) (Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	ev := Event{ID: b.seq, Type: typ, Data: raw}
	if b.n < len(b.ring) {
		b.ring[(b.start+b.n)%len(b.ring)] = ev
		b.n++
	} else {
		b.ring[b.start] = ev
		b.start = (b.start + 1) % len(b.ring)
	}
	for s := range b.subs {
		select {
		case s.C <- ev:
		default:
			// Slow consumer: cut it off; it resumes from Last-Event-ID.
			s.dropped, s.closed = true, true
			delete(b.subs, s)
			close(s.C)
		}
	}
	return ev, nil
}

// Subscribe registers a subscriber (call before Since to avoid gaps).
func (b *Bus) Subscribe() *Sub {
	s := &Sub{C: make(chan Event, SubBuffer), b: b}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Since returns retained events with ID > after. reset is true when after is
// older than the ring's tail (events were lost to the client): it must refetch.
// after == 0 means "no history wanted" and returns nothing.
func (b *Bus) Since(after uint64) (evs []Event, reset bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if after == 0 || after >= b.seq {
		return nil, false
	}
	if b.n == 0 {
		return nil, after < b.seq
	}
	oldest := b.ring[b.start].ID
	if after+1 < oldest {
		return nil, true
	}
	for i := 0; i < b.n; i++ {
		ev := b.ring[(b.start+i)%len(b.ring)]
		if ev.ID > after {
			evs = append(evs, ev)
		}
	}
	return evs, false
}

// Seq returns the last published ID.
func (b *Bus) Seq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Hub holds one bus per key (workspace ID), created on demand.
type Hub struct {
	mu    sync.Mutex
	ring  int
	buses map[string]*Bus
}

// NewHub returns a hub whose buses retain ring events each.
func NewHub(ring int) *Hub { return &Hub{ring: ring, buses: map[string]*Bus{}} }

// Bus returns the bus for key.
func (h *Hub) Bus(key string) *Bus {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.buses[key]
	if !ok {
		b = NewBus(h.ring)
		h.buses[key] = b
	}
	return b
}

// Publish publishes on key's bus.
func (h *Hub) Publish(key, typ string, data any) {
	_, _ = h.Bus(key).Publish(typ, data)
}

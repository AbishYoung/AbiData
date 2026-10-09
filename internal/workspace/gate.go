package workspace

import (
	"context"
	"sync"
)

// Priority orders writers waiting for a workspace's single writer connection.
type Priority int

const (
	Interactive Priority = iota // user requests: served first
	Background                  // imports, jobs, maintenance-style writers
)

func (p Priority) String() string {
	if p == Interactive {
		return "interactive"
	}
	return "background"
}

// gate is a two-level priority mutex: when released it hands off to the oldest
// interactive waiter, else the oldest background waiter. Waiters that give up
// (ctx done) leave the queue; a waiter granted the gate concurrently with its
// cancellation passes it on, so the gate is never lost.
type gate struct {
	mu     sync.Mutex
	held   bool
	queues [2][]*waiter
}

type waiter struct {
	ch      chan struct{}
	granted bool
}

func (g *gate) acquire(ctx context.Context, p Priority) error {
	g.mu.Lock()
	if !g.held && len(g.queues[Interactive]) == 0 && len(g.queues[Background]) == 0 {
		g.held = true
		g.mu.Unlock()
		return nil
	}
	w := &waiter{ch: make(chan struct{})}
	g.queues[p] = append(g.queues[p], w)
	g.mu.Unlock()

	select {
	case <-w.ch:
		return nil
	case <-ctx.Done():
		g.mu.Lock()
		if w.granted {
			g.mu.Unlock()
			g.release()
			return ctx.Err()
		}
		q := g.queues[p]
		for i, x := range q {
			if x == w {
				g.queues[p] = append(q[:i:i], q[i+1:]...)
				break
			}
		}
		g.mu.Unlock()
		return ctx.Err()
	}
}

func (g *gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for p := range g.queues {
		if q := g.queues[p]; len(q) > 0 {
			w := q[0]
			g.queues[p] = q[1:]
			w.granted = true
			close(w.ch)
			return
		}
	}
	g.held = false
}

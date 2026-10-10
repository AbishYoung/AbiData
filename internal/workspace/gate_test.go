package workspace

import (
	"context"
	"testing"
	"time"
)

// A background waiter is served after at most maxInteractiveStreak interactive
// grants even while interactive writers keep arriving.
func TestGateBackgroundNotStarved(t *testing.T) {
	ctx := context.Background()
	var g gate
	if err := g.acquire(ctx, Interactive); err != nil {
		t.Fatal(err)
	}
	bg := make(chan struct{})
	go func() {
		_ = g.acquire(ctx, Background)
		close(bg)
	}()
	waitQueued(t, &g, Background, 1)
	grants := 0
	for {
		// Keep an interactive writer always queued, then hand the gate on.
		done := make(chan struct{})
		go func() {
			_ = g.acquire(ctx, Interactive)
			close(done)
		}()
		waitQueued(t, &g, Interactive, 1)
		g.release()
		select {
		case <-bg:
			if grants > maxInteractiveStreak {
				t.Fatalf("background waited for %d interactive grants", grants)
			}
			g.release() // background done → the queued interactive writer
			<-done
			g.release()
			return
		case <-done:
			grants++
		case <-time.After(2 * time.Second):
			t.Fatal("gate stalled")
		}
	}
}

func waitQueued(t *testing.T, g *gate, p Priority, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		g.mu.Lock()
		l := len(g.queues[p])
		g.mu.Unlock()
		if l >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%v waiters never queued", p)
		}
		time.Sleep(time.Millisecond)
	}
}

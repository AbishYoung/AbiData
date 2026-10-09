package events

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRingReplayAndReset(t *testing.T) {
	b := NewBus(5)
	first, _ := b.Publish(RowsChanged, map[string]int{"n": 0})
	for i := 1; i < 8; i++ {
		_, _ = b.Publish(RowsChanged, map[string]int{"n": i})
	}
	evs, reset := b.Since(first.ID + 4)
	if reset || len(evs) != 3 || evs[0].ID != first.ID+5 || evs[2].ID != first.ID+7 {
		t.Fatalf("since: %v %v", evs, reset)
	}
	if _, reset := b.Since(first.ID); !reset {
		t.Fatal("too-old ID did not reset")
	}
	if evs, reset := b.Since(first.ID + 7); reset || len(evs) != 0 {
		t.Fatal("up-to-date ID returned events")
	}
}

func TestSlowConsumerDropped(t *testing.T) {
	b := NewBus(100)
	s := b.Subscribe()
	for i := 0; i < SubBuffer+1; i++ {
		_, _ = b.Publish(RowsChanged, i)
	}
	if !s.Dropped() {
		t.Fatal("slow subscriber not dropped")
	}
	n := 0
	for range s.C {
		n++
	}
	if n != SubBuffer {
		t.Fatalf("drained %d", n)
	}
	s.Close() // idempotent after drop
}

type sseEvent struct {
	id  uint64
	typ string
}

func readEvents(t *testing.T, srv *httptest.Server, lastID uint64, want int) []sseEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if lastID > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatUint(lastID, 10))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Connection") != "" {
		t.Fatalf("headers %v", resp.Header)
	}
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && len(out) < want {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id, _ = strconv.ParseUint(line[4:], 10, 64)
		case strings.HasPrefix(line, "event: "):
			cur.typ = line[7:]
		case line == "" && cur.typ != "":
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	return out
}

func TestStreamResumeNoGapsNoDuplicates(t *testing.T) {
	b := NewBus(1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Stream(w, r, b, StreamOptions{})
	}))
	defer srv.Close()
	start, _ := b.Publish(RowsChanged, 0)
	// Disconnected while 50 events happen; reconnect with Last-Event-ID.
	for i := 1; i <= 50; i++ {
		_, _ = b.Publish(RowsChanged, i)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		for i := 51; i <= 60; i++ {
			_, _ = b.Publish(RowsChanged, i)
		}
	}()
	evs := readEvents(t, srv, start.ID, 60)
	for i, ev := range evs {
		if ev.id != start.ID+uint64(i)+1 || ev.typ != RowsChanged {
			t.Fatalf("event %d = %+v (start %d)", i, ev, start.ID)
		}
	}
	if len(evs) != 60 {
		t.Fatalf("got %d events", len(evs))
	}
	// Too-old ID → reset.
	small := NewBus(3)
	old, _ := small.Publish(RowsChanged, 0)
	for i := 0; i < 5; i++ {
		_, _ = small.Publish(RowsChanged, i)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Stream(w, r, small, StreamOptions{}) }))
	defer srv2.Close()
	if evs := readEvents(t, srv2, old.ID, 1); len(evs) != 1 || evs[0].typ != Reset {
		t.Fatalf("want reset, got %v", evs)
	}
}

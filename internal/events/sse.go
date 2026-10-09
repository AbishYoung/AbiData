package events

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// StreamOptions configure Stream.
type StreamOptions struct {
	Heartbeat time.Duration                   // default 15 s
	Recheck   time.Duration                   // default 5 min
	Authorize func(ctx context.Context) error // re-checked every Recheck; error ends the stream
	Filter    func(Event) bool                // optional per-subscriber filter (e.g. log level)
}

// Stream serves b as Server-Sent Events: subscribe first, replay events after
// Last-Event-ID (or send "reset" if it is older than the ring), then live events,
// skipping any already sent. Returns when the client goes away, the subscriber
// is dropped for being slow, or authorization fails.
func Stream(w http.ResponseWriter, r *http.Request, b *Bus, opt StreamOptions) {
	if opt.Heartbeat <= 0 {
		opt.Heartbeat = 15 * time.Second
	}
	if opt.Recheck <= 0 {
		opt.Recheck = 5 * time.Minute
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sub := b.Subscribe()
	defer sub.Close()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	var last uint64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		last, _ = strconv.ParseUint(v, 10, 64)
	} else if v := r.URL.Query().Get("last_event_id"); v != "" {
		last, _ = strconv.ParseUint(v, 10, 64)
	}
	send := func(ev Event) bool {
		if opt.Filter != nil && !opt.Filter(ev) {
			last = ev.ID
			return true
		}
		_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, ev.Data)
		last = ev.ID
		return err == nil
	}
	if last > 0 {
		evs, reset := b.Since(last)
		if reset {
			// The client's state is stale; it refetches. Resume from now.
			cur := b.Seq()
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: {}\n\n", cur, Reset); err != nil {
				return
			}
			last = cur
		}
		for _, ev := range evs {
			if !send(ev) {
				return
			}
		}
	} else {
		// Fresh connection: announce the current position so a reconnect can resume.
		if _, err := fmt.Fprintf(w, "id: %d\nevent: hello\ndata: {}\n\n", b.Seq()); err != nil {
			return
		}
		last = b.Seq()
	}
	flusher.Flush()

	hb := time.NewTicker(opt.Heartbeat)
	defer hb.Stop()
	rc := time.NewTicker(opt.Recheck)
	defer rc.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return // dropped for falling behind; the client reconnects with Last-Event-ID
			}
			if ev.ID <= last {
				continue
			}
			if !send(ev) {
				return
			}
			flusher.Flush()
		case <-hb.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-rc.C:
			if opt.Authorize != nil && opt.Authorize(r.Context()) != nil {
				return
			}
		}
	}
}

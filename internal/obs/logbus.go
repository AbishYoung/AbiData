package obs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"abidata/internal/events"
)

// LogEvent is the event type of a streamed log record.
const LogEvent = "log"

// LogRecord is a log record as streamed to platform admins.
type LogRecord struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// BusHandler publishes log records at or above Level to a bus, for the admin
// console's live log stream (replayable via Last-Event-ID within the ring).
type BusHandler struct {
	bus    *events.Bus
	level  slog.Leveler
	attrs  []slog.Attr
	prefix string
}

// NewBusHandler returns a handler publishing to bus.
func NewBusHandler(bus *events.Bus, level slog.Leveler) *BusHandler {
	return &BusHandler{bus: bus, level: level}
}

func (h *BusHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }

func (h *BusHandler) Handle(_ context.Context, r slog.Record) error {
	rec := LogRecord{Time: r.Time.UTC(), Level: r.Level.String(), Message: r.Message}
	add := func(prefix string, a slog.Attr) {
		if rec.Attrs == nil {
			rec.Attrs = map[string]any{}
		}
		rec.Attrs[prefix+a.Key] = busValue(a)
	}
	for _, a := range h.attrs {
		add("", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(h.prefix, a)
		return true
	})
	_, err := h.bus.Publish(LogEvent, rec)
	return err
}

// busValue renders an attribute for the stream: errors as their text (they
// would otherwise encode as {}), and secrets (keys ending in "token") redacted,
// since the stream is replayable for as long as the record stays in the ring.
func busValue(a slog.Attr) any {
	if strings.HasSuffix(strings.ToLower(a.Key), "token") {
		return "[redacted]"
	}
	v := a.Value.Resolve().Any()
	switch x := v.(type) {
	case error:
		return x.Error()
	case fmt.Stringer:
		return x.String()
	}
	return v
}

func (h *BusHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), prefixed(h.prefix, as)...)
	return &c
}

func (h *BusHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.prefix = h.prefix + name + "."
	return &c
}

func prefixed(p string, as []slog.Attr) []slog.Attr {
	if p == "" {
		return as
	}
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = slog.Attr{Key: p + a.Key, Value: a.Value}
	}
	return out
}

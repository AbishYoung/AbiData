// Package obs provides logging and metrics shared across AbiData.
package obs

import (
	"context"
	"errors"
	"io"
	"log/slog"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyWorkspace
	keyUser
)

func get(ctx context.Context, k ctxKey) string {
	s, _ := ctx.Value(k).(string)
	return s
}

// WithRequestID returns ctx carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// RequestID returns the request ID stored in ctx, or "".
func RequestID(ctx context.Context) string { return get(ctx, keyRequestID) }

// WithWorkspace returns ctx carrying the workspace ID.
func WithWorkspace(ctx context.Context, ws string) context.Context {
	return context.WithValue(ctx, keyWorkspace, ws)
}

// WorkspaceID returns the workspace ID stored in ctx, or "".
func WorkspaceID(ctx context.Context) string { return get(ctx, keyWorkspace) }

// WithUser returns ctx carrying the user ID.
func WithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, keyUser, userID)
}

// UserID returns the user ID stored in ctx, or "".
func UserID(ctx context.Context) string { return get(ctx, keyUser) }

// Subsystem returns a logger that tags every record with the subsystem name.
func Subsystem(l *slog.Logger, name string) *slog.Logger {
	return l.With("subsystem", name)
}

// NewLogger returns a JSON logger writing to w at the given level. Records are
// enriched with request_id, workspace_id and user_id from the context. Each
// handler in tees also receives every record, subject to its own Enabled.
func NewLogger(w io.Writer, level slog.Level, tees ...slog.Handler) *slog.Logger {
	var base slog.Handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	if len(tees) > 0 {
		hs := make([]slog.Handler, 0, len(tees)+1)
		hs = append(hs, base)
		hs = append(hs, tees...)
		base = &fanout{handlers: hs}
	}
	return slog.New(&ctxHandler{next: base})
}

// ctxHandler adds context-derived attributes to each record.
type ctxHandler struct{ next slog.Handler }

func (h *ctxHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if v := RequestID(ctx); v != "" {
		r.AddAttrs(slog.String("request_id", v))
	}
	if v := WorkspaceID(ctx); v != "" {
		r.AddAttrs(slog.String("workspace_id", v))
	}
	if v := UserID(ctx); v != "" {
		r.AddAttrs(slog.String("user_id", v))
	}
	return h.next.Handle(ctx, r)
}

func (h *ctxHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &ctxHandler{next: h.next.WithAttrs(a)}
}

func (h *ctxHandler) WithGroup(name string) slog.Handler {
	return &ctxHandler{next: h.next.WithGroup(name)}
}

// fanout delivers records to several handlers.
type fanout struct{ handlers []slog.Handler }

func (f *fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f *fanout) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range f.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (f *fanout) WithAttrs(a []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		hs[i] = h.WithAttrs(a)
	}
	return &fanout{handlers: hs}
}

func (f *fanout) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &fanout{handlers: hs}
}

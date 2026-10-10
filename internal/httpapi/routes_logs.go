package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"abidata/internal/events"
	"abidata/internal/obs"
)

// logsHandler streams server log records to platform admins as SSE (plain chi
// route). Last-Event-ID resumes within the ring; an older ID gets "reset".
// ?level=warn|error filters the stream.
func (s *Server) logsHandler(w http.ResponseWriter, r *http.Request) {
	if s.deps.Logs == nil {
		writeError(w, r, s.log, errNotFound)
		return
	}
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		writeError(w, r, s.log, errUnauth)
		return
	}
	p, err := s.deps.Auth.Authenticate(r.Context(), c.Value)
	if err != nil {
		writeError(w, r, s.log, err)
		return
	}
	if !p.IsPlatformAdmin {
		writeError(w, r, s.log, errForbidden)
		return
	}
	min := slog.LevelDebug
	switch strings.ToLower(r.URL.Query().Get("level")) {
	case "info":
		min = slog.LevelInfo
	case "warn":
		min = slog.LevelWarn
	case "error":
		min = slog.LevelError
	}
	events.Stream(w, r, s.deps.Logs, events.StreamOptions{
		Authorize: func(ctx context.Context) error {
			p2, err := s.deps.Auth.Authenticate(ctx, c.Value)
			if err == nil && !p2.IsPlatformAdmin {
				err = errForbidden
			}
			return err
		},
		Recheck: time.Minute,
		Filter: func(ev events.Event) bool {
			if min == slog.LevelDebug {
				return true
			}
			var rec obs.LogRecord
			if json.Unmarshal(ev.Data, &rec) != nil {
				return true
			}
			var l slog.Level
			if l.UnmarshalText([]byte(rec.Level)) != nil {
				return true
			}
			return l >= min
		},
	})
}

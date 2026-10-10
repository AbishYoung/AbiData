package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"abidata/internal/obs"
)

type sse struct{ id, typ, data string }

// readSSE connects to path and returns the first n events (or fewer on timeout).
func readSSE(t *testing.T, c *client, path, lastID string, n int) (int, []sse) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", c.e.srv.URL+path, nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil
	}
	var out []sse
	var cur sse
	sc := bufio.NewScanner(resp.Body)
	for len(out) < n && sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id = line[4:]
		case strings.HasPrefix(line, "event: "):
			cur.typ = line[7:]
		case strings.HasPrefix(line, "data: "):
			cur.data = line[6:]
		case line == "":
			if cur.typ != "" {
				out = append(out, cur)
			}
			cur = sse{}
		}
	}
	return 200, out
}

// Gate 6: log stream replay, reset when too old, admins only.
func TestLogStream(t *testing.T) {
	e := newEnv(t)
	_, root, o, _, _, _ := roles(t, e)
	logger := slog.New(obs.NewBusHandler(e.s.deps.Logs, slog.LevelDebug))
	const path = "/api/v1/admin/logs/stream"
	if st, _ := readSSE(t, o, path, "", 1); st != 403 {
		t.Fatalf("workspace owner opened the log stream: %d", st)
	}
	st, evs := readSSE(t, root, path, "", 1)
	if st != 200 || len(evs) != 1 || evs[0].typ != "hello" {
		t.Fatalf("%d %v", st, evs)
	}
	resume := evs[0].id
	// Disconnected: 50 records are emitted.
	for i := range 50 {
		logger.Info("tick", "i", i)
	}
	_, evs = readSSE(t, root, path, resume, 50)
	if len(evs) != 50 {
		t.Fatalf("replayed %d of 50", len(evs))
	}
	prev, _ := strconv.ParseUint(resume, 10, 64)
	for i, ev := range evs {
		id, _ := strconv.ParseUint(ev.id, 10, 64)
		if ev.typ != obs.LogEvent || id != prev+1 || !strings.Contains(ev.data, fmt.Sprintf(`"i":%d`, i)) {
			t.Fatalf("event %d out of order or wrong: %+v (prev id %d)", i, ev, prev)
		}
		prev = id
	}
	// The ring holds 100 events: an ID older than that gets reset.
	for i := range 150 {
		logger.Warn("flood", "i", i)
	}
	_, evs = readSSE(t, root, path, resume, 1)
	if len(evs) != 1 || evs[0].typ != "reset" {
		t.Fatalf("expected reset, got %v", evs)
	}
	// Filters: level, subsystem, workspace.
	logger.Info("quiet")
	logger.Error("loud")
	logger.Info("job ran", "subsystem", "jobs", "workspace_id", "W1")
	logger.Info("job ran", "subsystem", "jobs", "workspace_id", "W2")
	from := strconv.FormatUint(prev+150, 10)
	_, evs = readSSE(t, root, path+"?level=error", from, 1)
	if len(evs) != 1 || !strings.Contains(evs[0].data, `"loud"`) {
		t.Fatalf("level filter: %v", evs)
	}
	_, evs = readSSE(t, root, path+"?subsystem=jobs&workspace=W2", from, 1)
	if len(evs) != 1 || !strings.Contains(evs[0].data, `"W2"`) {
		t.Fatalf("subsystem/workspace filter: %v", evs)
	}
}

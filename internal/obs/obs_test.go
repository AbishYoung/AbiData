package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &m); err != nil {
		t.Fatalf("bad json %q: %v", b, err)
	}
	return m
}

func TestContextAttrs(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, slog.LevelInfo)
	ctx := WithUser(WithWorkspace(WithRequestID(context.Background(), "r1"), "w1"), "u1")
	l.InfoContext(ctx, "hello")
	m := decode(t, buf.Bytes())
	if m["request_id"] != "r1" || m["workspace_id"] != "w1" || m["user_id"] != "u1" || m["msg"] != "hello" {
		t.Fatalf("got %v", m)
	}
	if RequestID(ctx) != "r1" || WorkspaceID(ctx) != "w1" || UserID(ctx) != "u1" {
		t.Fatal("accessors")
	}
}

func TestNoContextAttrsWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, slog.LevelInfo)
	l.InfoContext(WithRequestID(context.Background(), "r1"), "x")
	l.Info("y")
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		m := decode(t, []byte(line))
		if _, ok := m["workspace_id"]; ok {
			t.Fatal("unexpected workspace_id")
		}
		if _, ok := m["user_id"]; ok {
			t.Fatal("unexpected user_id")
		}
	}
	if RequestID(context.Background()) != "" {
		t.Fatal("empty ctx")
	}
}

func TestSubsystem(t *testing.T) {
	var buf bytes.Buffer
	Subsystem(NewLogger(&buf, slog.LevelInfo), "backup").Info("x")
	if decode(t, buf.Bytes())["subsystem"] != "backup" {
		t.Fatalf("got %s", buf.String())
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf, slog.LevelWarn).Info("nope")
	if buf.Len() != 0 {
		t.Fatalf("unexpected output %s", buf.String())
	}
}

func TestTees(t *testing.T) {
	var main, tee, quiet bytes.Buffer
	teeH := slog.NewJSONHandler(&tee, &slog.HandlerOptions{Level: slog.LevelDebug})
	quietH := slog.NewJSONHandler(&quiet, &slog.HandlerOptions{Level: slog.LevelError})
	l := Subsystem(NewLogger(&main, slog.LevelInfo, teeH, quietH), "s")
	ctx := WithRequestID(context.Background(), "r9")
	l.InfoContext(ctx, "fan")
	l.Debug("dbg")
	if m := decode(t, main.Bytes()); m["msg"] != "fan" {
		t.Fatalf("main: %s", main.String())
	}
	lines := strings.Split(strings.TrimSpace(tee.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("tee lines: %v", lines)
	}
	if m := decode(t, []byte(lines[0])); m["request_id"] != "r9" || m["subsystem"] != "s" {
		t.Fatalf("tee: %v", m)
	}
	if quiet.Len() != 0 {
		t.Fatalf("quiet tee got %s", quiet.String())
	}
}

func TestMetricsHandler(t *testing.T) {
	m := NewMetrics()
	m.OpenWorkspaces.Set(3)
	m.HTTPRequests.WithLabelValues("GET", "/x", "200").Observe(0.01)
	m.WALBytes.WithLabelValues("w").Set(1)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{"abidata_open_workspaces 3", "abidata_http_request_duration_seconds_bucket", "go_goroutines", "abidata_wal_size_bytes"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestNewMetricsTwice(t *testing.T) {
	a, b := NewMetrics(), NewMetrics()
	a.OpenWorkspaces.Set(1)
	rec := httptest.NewRecorder()
	b.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "abidata_open_workspaces 0") {
		t.Fatal("registries are not independent")
	}
}

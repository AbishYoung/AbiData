package httpapi

import (
	"testing"
	"time"

	"abidata/internal/obs"
)

func TestAdminOverview(t *testing.T) {
	e := newEnv(t)
	ws, root, o, _, _, _ := roles(t, e)
	sampler := obs.NewSampler(obs.NewMetrics())
	e.s.deps.Series = sampler
	sampler.RecordFailure(obs.Failure{Kind: "job", WorkspaceID: ws, Subject: "job1", Status: "failed", Message: "boom", At: time.Now()})
	sampler.RecordFailure(obs.Failure{Kind: "backup", WorkspaceID: ws, Subject: "scheduled", Status: "failed", Message: "disk full", At: time.Now()})
	sampler.Sample()

	// Not for workspace owners, not for anonymous callers.
	if st, _, _ := o.do("GET", "/api/v1/admin/overview", nil); st != 403 {
		t.Fatalf("owner read overview: %d", st)
	}
	if st, _, _ := e.client().do("GET", "/api/v1/admin/overview", nil); st != 401 {
		t.Fatalf("anonymous read overview: %d", st)
	}

	st, body, _ := root.do("GET", "/api/v1/admin/overview", nil)
	if st != 200 {
		t.Fatalf("%d %v", st, body)
	}
	if body["range"] != "1h" {
		t.Fatalf("default range %v", body["range"])
	}
	stats := body["stats"].(map[string]any)
	if stats["users"].(float64) < 5 || stats["heap_bytes"].(float64) <= 0 {
		t.Fatalf("stats %v", stats)
	}
	series := body["series"].(map[string]any)
	if series["interval_seconds"].(float64) != 10 {
		t.Fatalf("interval %v", series["interval_seconds"])
	}
	// Every column has one entry per timestamp.
	n := len(series["times"].([]any))
	for k, v := range series {
		if arr, ok := v.([]any); ok && len(arr) != n {
			t.Fatalf("series %s has %d entries, times has %d", k, len(arr), n)
		}
	}
	wss := body["workspaces"].([]any)
	if len(wss) < 1 {
		t.Fatalf("workspaces %v", wss)
	}
	fails := body["failures"].([]any)
	if len(fails) != 2 || fails[0].(map[string]any)["workspace_name"] != "Jobs" {
		t.Fatalf("failures %v", fails)
	}
	var sawFailedBackup bool
	for _, b := range body["backups"].([]any) {
		bm := b.(map[string]any)
		if bm["workspace_id"] == ws && bm["status"] == "failed" && bm["last_failure_message"] == "disk full" {
			sawFailedBackup = true
		}
	}
	if !sawFailedBackup {
		t.Fatalf("backups %v", body["backups"])
	}
	audit := body["audit"].([]any)
	if len(audit) == 0 || audit[0].(map[string]any)["actor"] == "" {
		t.Fatalf("audit %v", audit)
	}

	st, body, _ = root.do("GET", "/api/v1/admin/overview?range=24h", nil)
	if st != 200 || body["series"].(map[string]any)["interval_seconds"].(float64) != 60 {
		t.Fatalf("24h: %d %v", st, body["series"])
	}
	if st, _, _ = root.do("GET", "/api/v1/admin/overview?range=7d", nil); st != 422 && st != 400 {
		t.Fatalf("bad range accepted: %d", st)
	}
}

// Without a sampler the endpoint still answers, with empty series.
func TestAdminOverviewNoSampler(t *testing.T) {
	e := newEnv(t)
	_, root, _, _, _, _ := roles(t, e)
	st, body, _ := root.do("GET", "/api/v1/admin/overview", nil)
	if st != 200 || len(body["series"].(map[string]any)["times"].([]any)) != 0 {
		t.Fatalf("%d %v", st, body)
	}
}

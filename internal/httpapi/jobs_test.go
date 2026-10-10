package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"abidata/internal/ids"
	"abidata/internal/ops"
	"abidata/internal/workspace"
)

// roles sets up a workspace with an owner, a builder, two editors and returns their clients.
func roles(t *testing.T, e *env) (ws string, root, owner, builder, editor, editor2 *client) {
	t.Helper()
	for _, u := range []string{"o@example.com", "b@example.com", "ed@example.com", "ed2@example.com"} {
		e.user(u, false)
	}
	e.user("root@example.com", true)
	root = e.login("root@example.com")
	_, body, _ := root.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Jobs", "owner_email": "o@example.com"})
	ws = body["id"].(string)
	owner = e.login("o@example.com")
	for email, role := range map[string]string{"b@example.com": "builder", "ed@example.com": "editor", "ed2@example.com": "editor"} {
		if st, body, _ := owner.do("POST", "/api/v1/w/"+ws+"/members", map[string]string{"email": email, "role": role}); st != 201 {
			t.Fatalf("%d %v", st, body)
		}
	}
	return ws, root, owner, e.login("b@example.com"), e.login("ed@example.com"), e.login("ed2@example.com")
}

func TestJobRoutes(t *testing.T) {
	e := newEnv(t)
	ws, _, _, b, ed, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	st, sc, _ := b.do("POST", base+"/scripts", map[string]any{"name": "Export", "kind": "job",
		"source": `log.info("hi", {n = job.args.n}) artifacts.save("out.html", "<script>alert(1)</script>")`})
	if st != 201 {
		t.Fatalf("script %d %v", st, sc)
	}
	st, job, _ := b.do("POST", base+"/jobs", map[string]any{"name": "Export", "script_id": sc["id"], "schedule": "@daily", "args": map[string]any{"n": 1}})
	if st != 201 {
		t.Fatalf("job %d %v", st, job)
	}
	jid := job["id"].(string)
	if st, _, _ := ed.do("GET", base+"/jobs", nil); st != 403 {
		t.Fatalf("editor listed jobs: %d", st)
	}
	if st, _, _ := ed.do("POST", base+"/jobs/"+jid+"/run", nil); st != 403 {
		t.Fatalf("editor ran job: %d", st)
	}
	if st, body, _ := b.do("POST", base+"/jobs", map[string]any{"name": "Bad", "script_id": sc["id"], "schedule": "61 * * * *"}); st != 400 {
		t.Fatalf("bad schedule %d %v", st, body)
	}
	st, run, _ := b.do("POST", base+"/jobs/"+jid+"/run", map[string]any{"args": map[string]any{"n": 2}})
	if st != 202 {
		t.Fatalf("run %d %v", st, run)
	}
	rid := run["id"].(string)
	var detail map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, detail, _ = b.do("GET", base+"/runs/"+rid, nil)
		if detail["finished_at"] != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if detail["status"] != "succeeded" || !strings.Contains(detail["log"].(string), "hi n=2") {
		t.Fatalf("%v", detail)
	}
	arts := detail["artifacts"].([]any)
	if len(arts) != 1 || arts[0].(map[string]any)["name"] != "out.html" {
		t.Fatalf("artifacts %v", arts)
	}
	// Artifacts download as attachments under a sandbox CSP (never rendered in our origin).
	req, _ := http.NewRequest("GET", e.srv.URL+base+"/runs/"+rid+"/artifacts/out.html", nil)
	resp, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "<script>alert(1)</script>" ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") ||
		!strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("%d %v %s", resp.StatusCode, resp.Header, body)
	}
	if st, _, _ := b.do("GET", base+"/runs/"+rid+"/artifacts/..%2F..%2Fsystem%2Fadmin.db", nil); st != 404 {
		t.Fatalf("traversal %d", st)
	}
	if st, _, _ := ed.do("GET", base+"/runs/"+rid+"/artifacts/out.html", nil); st != 403 {
		t.Fatalf("editor downloaded artifact: %d", st)
	}
	_, list, _ := b.do("GET", base+"/jobs/"+jid+"/runs", nil)
	if items := list["items"].([]any); len(items) != 1 {
		t.Fatalf("runs %v", list)
	}
	// A run of another workspace is not reachable through this one.
	if st, _, _ := b.do("GET", "/api/v1/w/"+ids.New()+"/runs/"+rid, nil); st != 404 {
		t.Fatalf("cross-workspace run %d", st)
	}
}

func TestEgressSettingsRoutes(t *testing.T) {
	e := newEnv(t)
	ws, root, o, b, _, _ := roles(t, e)
	path := "/api/v1/w/" + ws + "/settings/egress"
	if st, _, _ := b.do("PUT", path, map[string]any{"items": []any{}}); st != 403 {
		t.Fatalf("builder changed allowlist: %d", st)
	}
	for _, bad := range []map[string]any{
		{"pattern": "http://x.com"}, {"pattern": "*"}, {"pattern": "a.com", "ports": "0"}, {"pattern": "x.com/path"},
	} {
		if st, body, _ := o.do("PUT", path, map[string]any{"items": []any{bad}}); st != 400 {
			t.Fatalf("%v accepted: %d %v", bad, st, body)
		}
	}
	if st, _, _ := o.do("PUT", path, map[string]any{"items": []any{map[string]any{"pattern": "a.com"}, map[string]any{"pattern": "A.com."}}}); st != 400 {
		t.Fatal("duplicate accepted")
	}
	st, body, _ := o.do("PUT", path, map[string]any{"items": []any{
		map[string]any{"pattern": " API.Example.com. ", "note": "rates"}, map[string]any{"pattern": "*.cdn.test", "ports": "443, 8443"},
	}})
	if st != 200 {
		t.Fatalf("%d %v", st, body)
	}
	items := body["items"].([]any)
	if items[0].(map[string]any)["pattern"] != "*.cdn.test" || items[0].(map[string]any)["ports"] != "443,8443" ||
		items[1].(map[string]any)["pattern"] != "api.example.com" || items[1].(map[string]any)["ports"] != "443" {
		t.Fatalf("%v", items)
	}
	if hosts := body["platform"].(map[string]any)["hosts"].([]any); len(hosts) != 1 || hosts[0] != "*" {
		t.Fatalf("default platform policy %v", body["platform"])
	}
	if st, _, _ := o.do("PUT", "/api/v1/admin/settings/egress", map[string]any{"hosts": []string{"*"}}); st != 403 {
		t.Fatal("owner changed platform policy")
	}
	if st, body, _ := root.do("PUT", "/api/v1/admin/settings/egress", map[string]any{"hosts": []string{"*.example.com", "bad host"}}); st != 400 {
		t.Fatalf("bad platform host %d %v", st, body)
	}
	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/egress", map[string]any{"hosts": []string{"*.Example.com"}, "allow_http": true}); st != 200 {
		t.Fatal("platform policy")
	}
	_, body, _ = o.do("GET", path, nil)
	if p := body["platform"].(map[string]any); p["allow_http"] != true || p["hosts"].([]any)[0] != "*.example.com" {
		t.Fatalf("%v", p)
	}
}

func TestOperationVisibility(t *testing.T) {
	e := newEnv(t)
	ws, _, _, b, ed, ed2 := roles(t, e)
	ctx := context.Background()
	wsID, _ := ids.ParseWorkspaceID(ws)
	l, err := e.s.deps.Workspaces.Acquire(ctx, wsID)
	if err != nil {
		t.Fatal(err)
	}
	_, me, _ := ed.do("GET", "/api/v1/auth/me", nil)
	edID := me["user"].(map[string]any)["id"].(string)
	block := make(chan struct{})
	op, err := e.s.deps.Ops.Start(ctx, l, "export", edID, func(ctx context.Context, _ *workspace.Lease, _ *ops.Progress) (any, error) {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return map[string]any{"rows": 1}, ctx.Err()
	})
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/w/" + ws + "/operations/" + op.ID
	if st, _, _ := ed.do("GET", path, nil); st != 200 {
		t.Fatalf("creator %d", st)
	}
	if st, _, _ := b.do("GET", path, nil); st != 200 {
		t.Fatalf("builder %d", st)
	}
	if st, body, _ := ed2.do("GET", path, nil); st != 404 || errCode(body) != "NOT_FOUND" {
		t.Fatalf("other editor saw the operation: %d", st)
	}
	if st, _, _ := ed2.do("POST", path+"/cancel", nil); st != 404 {
		t.Fatalf("other editor cancelled: %d", st)
	}
	if st, _, _ := ed.do("POST", path+"/cancel", nil); st != 202 {
		t.Fatalf("creator cancel %d", st)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body, _ := ed.do("GET", path, nil)
		if body["status"] == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%v", body)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(block)
}

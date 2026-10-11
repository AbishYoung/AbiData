package httpapi

import (
	"context"
	"io"
	"log/slog"
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

func TestEgressPolicyRoutes(t *testing.T) {
	e := newEnv(t)
	ws, root, o, b, ed, _ := roles(t, e)
	admPath := "/api/v1/admin/settings/egress"
	hostsPath := "/api/v1/w/" + ws + "/egress-hosts"

	// The per-workspace routes are gone.
	if st, _, _ := o.do("GET", "/api/v1/w/"+ws+"/settings/egress", nil); st != 404 && st != 405 {
		t.Fatalf("workspace egress route still present: %d", st)
	}
	// Fresh install: empty allowlist (fail closed).
	st, body, _ := root.do("GET", admPath, nil)
	if items, ok := body["allowlist"].([]any); st != 200 || !ok || len(items) != 0 {
		t.Fatalf("default policy %d %v", st, body)
	}
	if st, _, _ := o.do("PUT", admPath, map[string]any{"allowlist": []any{}}); st != 403 {
		t.Fatalf("owner changed platform policy: %d", st)
	}
	if st, _, _ := o.do("GET", admPath, nil); st != 403 {
		t.Fatalf("owner read the admin policy: %d", st)
	}
	for _, bad := range []map[string]any{
		{"pattern": "http://x.com"}, {"pattern": "*"}, {"pattern": "a.com", "ports": "0"}, {"pattern": "x.com/path"}, {"pattern": "bad host"},
	} {
		if st, body, _ := root.do("PUT", admPath, map[string]any{"allowlist": []any{bad}}); st != 400 {
			t.Fatalf("%v accepted: %d %v", bad, st, body)
		}
	}
	if st, _, _ := root.do("PUT", admPath, map[string]any{"allowlist": []any{map[string]any{"pattern": "a.com"}, map[string]any{"pattern": "A.com."}}}); st != 400 {
		t.Fatal("duplicate accepted")
	}
	st, body, _ = root.do("PUT", admPath, map[string]any{"allow_http": true, "allowlist": []any{
		map[string]any{"pattern": " API.Example.com. ", "note": "rates"}, map[string]any{"pattern": "*.cdn.test", "ports": "443, 8443"},
	}})
	if st != 200 {
		t.Fatalf("%d %v", st, body)
	}
	items := body["allowlist"].([]any)
	if items[0].(map[string]any)["pattern"] != "api.example.com" || items[0].(map[string]any)["ports"] != "443" ||
		items[1].(map[string]any)["pattern"] != "*.cdn.test" || items[1].(map[string]any)["ports"] != "443,8443" || body["allow_http"] != true {
		t.Fatalf("%v", body)
	}
	// Members who can build scripts see host patterns and ports, not notes.
	for _, c := range []*client{o, b} {
		st, body, _ = c.do("GET", hostsPath, nil)
		hs, _ := body["items"].([]any)
		if st != 200 || len(hs) != 2 || hs[0].(map[string]any)["pattern"] != "api.example.com" || body["allow_http"] != true {
			t.Fatalf("hosts %d %v", st, body)
		}
		if _, leaked := hs[0].(map[string]any)["note"]; leaked {
			t.Fatalf("note leaked %v", hs[0])
		}
	}
	if st, _, _ := ed.do("GET", hostsPath, nil); st != 403 {
		t.Fatalf("editor read hosts: %d", st)
	}
	if st, _, _ := o.do("PUT", hostsPath, map[string]any{}); st != 405 && st != 404 {
		t.Fatalf("hosts route is writable: %d", st)
	}
}

func TestEgressPolicyMigration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	adm := e.s.deps.Admin
	// Fresh install: nothing stored, nothing allowed, nothing to migrate.
	if err := MigrateEgressPolicy(ctx, adm, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGlobalEgress(ctx, adm)
	if err != nil || len(g.Allowlist) != 0 {
		t.Fatalf("%+v %v", g, err)
	}
	// Old shape: "*" dropped, specific hosts kept.
	if err := adm.PutSetting(ctx, EgressSettingKey, `{"hosts":["*","*.example.com"],"allow_http":true}`); err != nil {
		t.Fatal(err)
	}
	g, err = LoadGlobalEgress(ctx, adm) // converted in memory before the startup migration ran
	if err != nil || len(g.Allowlist) != 1 || g.Allowlist[0].Pattern != "*.example.com" {
		t.Fatalf("%+v %v", g, err)
	}
	var logs strings.Builder
	if err := MigrateEgressPolicy(ctx, adm, slog.New(slog.NewTextHandler(&logs, nil))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "wildcard_dropped=true") {
		t.Fatalf("no warning logged: %s", logs.String())
	}
	raw, _ := adm.GetSetting(ctx, EgressSettingKey)
	if strings.Contains(raw, `"hosts"`) || !strings.Contains(raw, `"allowlist"`) {
		t.Fatalf("not rewritten: %s", raw)
	}
	// Idempotent.
	logs.Reset()
	if err := MigrateEgressPolicy(ctx, adm, slog.New(slog.NewTextHandler(&logs, nil))); err != nil || logs.Len() != 0 {
		t.Fatalf("second run: %v %q", err, logs.String())
	}
	if g2, _ := LoadGlobalEgress(ctx, adm); len(g2.Allowlist) != 1 || !g2.AllowHTTP {
		t.Fatalf("%+v", g2)
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

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"abidata/internal/docsrv"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

func TestBackupRoutes(t *testing.T) {
	e := newEnv(t)
	ws, root, o, b, _, _ := roles(t, e)
	ctx := context.Background()
	wsID, _ := ids.ParseWorkspaceID(ws)
	exec := func(sql string) {
		t.Helper()
		l, err := e.s.deps.Workspaces.Acquire(ctx, wsID)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Release()
		if err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error { _, err := tx.Exec(ctx, sql); return err }); err != nil {
			t.Fatal(err)
		}
	}
	count := func(id ids.WorkspaceID) (n int64) {
		t.Helper()
		l, err := e.s.deps.Workspaces.Acquire(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Release()
		if err := l.Read(ctx, func(q sqlitex.Querier) error { return q.QueryRow(ctx, `SELECT count(*) FROM t`).Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	exec(`CREATE TABLE t (x INTEGER)`)
	exec(`INSERT INTO t VALUES (1), (2)`)
	base := "/api/v1/w/" + ws + "/backups"
	if st, _, _ := b.do("POST", base, nil); st != 403 {
		t.Fatalf("builder created a backup: %d", st)
	}
	if st, _, _ := b.do("GET", base, nil); st != 403 {
		t.Fatalf("builder listed backups: %d", st)
	}
	st, bk, _ := o.do("POST", base, nil)
	if st != 201 || bk["kind"] != "manual" {
		t.Fatalf("create %d %v", st, bk)
	}
	bid := bk["id"].(string)
	_, list, _ := o.do("GET", base, nil)
	if items := list["items"].([]any); len(items) != 1 {
		t.Fatalf("%v", list)
	}
	exec(`INSERT INTO t VALUES (3)`)

	// Another workspace's owner cannot reach this backup through their workspace.
	_, other, _ := root.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Other", "owner_email": "b@example.com"})
	otherBase := "/api/v1/w/" + other["id"].(string) + "/backups/" + bid
	if st, _, _ := b.do("POST", otherBase+"/restore", nil); st != 404 {
		t.Fatalf("cross-workspace restore %d", st)
	}
	if st, _, _ := b.do("POST", otherBase+"/restore-as-new", map[string]string{"name": "Stolen"}); st != 404 {
		t.Fatalf("cross-workspace restore-as-new %d", st)
	}
	if st, _, _ := o.do("POST", base+"/not-an-id/restore", nil); st != 404 {
		t.Fatalf("bad id %d", st)
	}

	if st, body, _ := o.do("POST", base+"/"+bid+"/restore", nil); st != 204 {
		t.Fatalf("restore %d %v", st, body)
	}
	if n := count(wsID); n != 2 {
		t.Fatalf("after restore: %d rows", n)
	}
	st, nw, _ := o.do("POST", base+"/"+bid+"/restore-as-new", map[string]string{"name": "Copy"})
	if st != 201 {
		t.Fatalf("restore-as-new %d %v", st, nw)
	}
	newID, _ := ids.ParseWorkspaceID(nw["id"].(string))
	if n := count(newID); n != 2 {
		t.Fatalf("copy: %d rows", n)
	}
	// The caller owns the copy.
	if st, _, _ := o.do("GET", "/api/v1/w/"+string(newID)+"/backups", nil); st != 200 {
		t.Fatalf("owner of the copy: %d", st)
	}
	if st, _, _ := o.do("POST", base+"/"+bid+"/restore-as-new", map[string]string{"name": "Copy"}); st != 409 && st != 400 {
		t.Fatalf("duplicate name: %d", st)
	}
}

func TestAdminStatsAndBackupPolicy(t *testing.T) {
	e := newEnv(t)
	_, root, o, _, _, _ := roles(t, e)
	if st, _, _ := o.do("GET", "/api/v1/admin/stats", nil); st != 403 {
		t.Fatalf("owner read stats: %d", st)
	}
	st, stats, _ := root.do("GET", "/api/v1/admin/stats", nil)
	if st != 200 || stats["workspaces"].(float64) < 1 || stats["users"].(float64) < 5 || stats["heap_bytes"].(float64) <= 0 {
		t.Fatalf("%d %v", st, stats)
	}
	_, pol, _ := root.do("GET", "/api/v1/admin/settings/backup", nil)
	if pol["enabled"] != true || pol["hour_utc"].(float64) != 2 {
		t.Fatalf("default policy %v", pol)
	}
	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/backup", map[string]any{"enabled": true, "hour_utc": 24}); st != 422 && st != 400 {
		t.Fatalf("hour 24 accepted: %d", st)
	}
	if st, _, _ := o.do("PUT", "/api/v1/admin/settings/backup", map[string]any{"enabled": false, "hour_utc": 1}); st != 403 {
		t.Fatalf("owner changed backup policy: %d", st)
	}
	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/backup", map[string]any{"enabled": false, "hour_utc": 5}); st != 200 {
		t.Fatalf("put %d", st)
	}
	_, pol, _ = root.do("GET", "/api/v1/admin/settings/backup", nil)
	if pol["enabled"] != false || pol["hour_utc"].(float64) != 5 {
		t.Fatalf("policy %v", pol)
	}
}

func testDocs(t *testing.T) *docsrv.Site {
	t.Helper()
	s, err := docsrv.Build(fstest.MapFS{"guide/index.md": {Data: []byte("---\ntitle: Guide\n---\n# Guide\n\nHello [errors](/docs/reference/errors#not-found).\n")}}, docsrv.Generated())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDocsRoutes(t *testing.T) {
	e := newEnv(t)
	anon := e.client()
	st, idx, _ := anon.do("GET", "/api/v1/docs", nil)
	if st != 200 || len(idx["pages"].([]any)) < 3 {
		t.Fatalf("index %d %v", st, idx)
	}
	st, pg, _ := anon.do("GET", "/api/v1/docs/page?slug=reference/errors", nil)
	if st != 200 || !strings.Contains(pg["html"].(string), `id="not-found"`) {
		t.Fatalf("page %d", st)
	}
	if st, _, _ := anon.do("GET", "/api/v1/docs/page?slug=../../etc/passwd", nil); st != 404 {
		t.Fatalf("missing page %d", st)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/docs/highlight.css", nil)
	resp, err := anon.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/css") {
		t.Fatalf("css %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

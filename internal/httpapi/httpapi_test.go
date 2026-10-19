package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"abidata/internal/admin"
	"abidata/internal/artifacts"
	"abidata/internal/auth"
	"abidata/internal/backup"
	"abidata/internal/blogs"
	"abidata/internal/catalog"
	"abidata/internal/config"
	"abidata/internal/events"
	"abidata/internal/files"
	"abidata/internal/forms"
	"abidata/internal/ids"
	"abidata/internal/ingest"
	"abidata/internal/jobs"
	"abidata/internal/ops"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

const testOrigin = "http://abidata.test"

type env struct {
	t   *testing.T
	srv *httptest.Server
	s   *Server
	ad  *admin.Store
	au  *auth.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	ad, err := admin.Open(ctx, filepath.Join(dir, "system", "admin.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ad.Close() })
	au := auth.NewService(ad, auth.Keys{Session: make([]byte, 32)}, auth.Argon2Params{Memory: 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}, nil, nil)
	wm, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: catalog.Migrate,
		// Same fail-closed quota lookup as serve: regression for create-before-row.
		MaxPageCount: func(ctx context.Context, id ids.WorkspaceID) (int64, error) {
			w, err := ad.GetWorkspace(ctx, id)
			return w.Quota.MaxPageCount, err
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wm.Shutdown(ctx) })
	cfg := config.Config{PublicURL: testOrigin, DataDir: dir, PublicApps: true}
	spa := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>AbiData</title>")},
		"assets/app-1a.js":     {Data: []byte("console.log(1)")},
		"sw.js":                {Data: []byte("self.skipWaiting")},
		"manifest.webmanifest": {Data: []byte(`{"name":"AbiData"}`)},
		"icons/icon-192.png":   {Data: []byte("png")},
	}
	hub := events.NewHub(1000)
	recSvc := &records.Service{Schemas: nil, Events: hub}
	querySvc := &query.Service{Events: hub}
	scriptSvc := &scripts.Service{Events: hub}
	schemas := catalog.NewCache()
	recSvc.Schemas, querySvc.Schemas = schemas, schemas
	engine := &catalog.Engine{Validators: []catalog.Validator{query.SchemaValidator, forms.SchemaValidator, reports.SchemaValidator}, AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) {
		schemas.Put(l.ID(), s)
		hub.Publish(string(l.ID()), events.SchemaChanged, map[string]any{"schema_version": s.Version})
	}}
	runner := script.NewRunner(4, 4)
	arts := &artifacts.Store{Dir: filepath.Join(dir, "artifacts")}
	jobSvc := &jobs.Service{Scripts: scriptSvc, Events: hub}
	jobSvc.Scheduler = jobs.New(jobs.Config{Store: &jobs.SQLStore{Admin: ad, Workspaces: wm},
		Executor: &jobs.LuaExecutor{Workspaces: wm, Scripts: scriptSvc, Runner: runner, Schemas: schemas, Records: recSvc, Artifacts: arts},
		Publish:  func(id ids.WorkspaceID, r jobs.Run) { hub.Publish(string(id), events.JobRun, r) }})
	t.Cleanup(jobSvc.Scheduler.Wait)
	opsSvc := &ops.Service{Workspaces: wm, Events: hub}
	t.Cleanup(func() { _ = opsSvc.Shutdown(ctx) })
	ingestSvc := &ingest.Service{DataDir: dir, Workspaces: wm, Schemas: schemas, Catalog: engine, Records: recSvc, Queries: querySvc, Ops: opsSvc, Artifacts: arts}
	backups := &backup.Service{DataDir: dir, BackupDir: filepath.Join(dir, "backups"), Admin: ad, Workspaces: wm,
		Key: []byte(strings.Repeat("m", 32)), AfterRestore: func(_ context.Context, id ids.WorkspaceID) { schemas.Drop(id) }}
	t.Cleanup(backups.Wait)
	fileSvc := &files.Service{DataDir: dir, Quota: func(id ids.WorkspaceID) int64 {
		w, err := ad.GetWorkspace(context.Background(), id)
		if err != nil {
			return 0
		}
		return w.Quota.MaxFileBytes
	}}
	backups.Files = fileSvc
	blogSvc := &blogs.Provisioner{Admin: ad, Workspaces: wm, Catalog: engine, Schemas: schemas}
	engine.Guard = blogSvc.Guard
	s := New(Deps{Config: cfg, Blogs: blogSvc, Files: fileSvc, Ingest: ingestSvc, Backups: backups, Logs: events.NewBus(100), Docs: testDocs(t), Admin: ad, Auth: au, Workspaces: wm, Version: "test", SPA: spa,
		Schemas: schemas, Catalog: engine, Events: hub, Records: recSvc,
		Queries: querySvc, Scripts: scriptSvc, Jobs: jobSvc, Ops: opsSvc, Artifacts: arts,
		Reports: &reports.Service{Schemas: schemas, Records: recSvc, Queries: querySvc, Scripts: scriptSvc, Runner: script.NewRunner(4, 4), Events: hub},
		Forms:   &forms.Service{Schemas: schemas, Records: recSvc, Queries: querySvc, Scripts: scriptSvc, Runner: script.NewRunner(4, 4), Events: hub}})
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, s: s, ad: ad, au: au}
}

type client struct {
	e    *env
	http *http.Client
	csrf string
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar}}
}

type opt func(*http.Request)

func noCSRF(r *http.Request)   { r.Header.Del("X-CSRF-Token") }
func withOrigin(o string) opt  { return func(r *http.Request) { r.Header.Set("Origin", o) } }
func withCSRF(v string) opt    { return func(r *http.Request) { r.Header.Set("X-CSRF-Token", v) } }
func noOrigin(r *http.Request) { r.Header.Del("Origin") }

func (c *client) do(method, path string, body any, opts ...opt) (int, map[string]any, http.Header) {
	c.e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case rawJSON:
		rd = strings.NewReader(string(b))
	default:
		enc, _ := json.Marshal(b)
		rd = strings.NewReader(string(enc))
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", testOrigin)
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, resp.Header
}

func (e *env) user(email string, platformAdmin bool) {
	e.t.Helper()
	if _, err := e.au.CreateUser(context.Background(), email, email, "long-enough-pw", platformAdmin); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) login(email string) *client {
	e.t.Helper()
	c := e.client()
	st, body, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "long-enough-pw"})
	if st != 200 {
		e.t.Fatalf("login %s: %d %v", email, st, body)
	}
	c.csrf = body["csrf_token"].(string)
	return c
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func TestLoginLogoutAndCSRF(t *testing.T) {
	e := newEnv(t)
	e.user("ada@example.com", true)
	c := e.client()

	// Wrong password: envelope with request ID, no hint about the email.
	st, body, hdr := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "ada@example.com", "password": "nope-nope-nope"})
	if st != 401 || errCode(body) != "UNAUTHENTICATED" {
		t.Fatalf("%d %v", st, body)
	}
	if rid := body["error"].(map[string]any)["request_id"]; rid == "" || rid != hdr.Get("X-Request-ID") {
		t.Fatalf("request id mismatch: %v vs %s", rid, hdr.Get("X-Request-ID"))
	}
	// Login with a wrong Origin is rejected (login CSRF).
	if st, body, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "ada@example.com", "password": "long-enough-pw"},
		withOrigin("http://evil.test")); st != 403 || errCode(body) != "FORBIDDEN" {
		t.Fatalf("wrong origin login: %d %v", st, body)
	}

	c = e.login("ada@example.com")
	// Cookie attributes.
	_, _, h := c.do("GET", "/api/v1/auth/me", nil)
	if h.Get("Cache-Control") != "no-store" {
		t.Error("me not no-store")
	}
	u, _ := http.NewRequest("GET", e.srv.URL, nil)
	cookies := c.http.Jar.Cookies(u.URL)
	if len(cookies) != 1 || cookies[0].Name != "abidata_session" {
		t.Fatalf("cookies %v", cookies)
	}

	ws := map[string]string{"name": "Sales"}
	// Missing CSRF token → 403.
	if st, body, _ := c.do("POST", "/api/v1/admin/workspaces", ws, noCSRF); st != 403 || errCode(body) != "FORBIDDEN" {
		t.Fatalf("missing csrf: %d %v", st, body)
	}
	// Wrong CSRF token → 403.
	if st, _, _ := c.do("POST", "/api/v1/admin/workspaces", ws, withCSRF("wrong")); st != 403 {
		t.Fatalf("wrong csrf: %d", st)
	}
	// Wrong Origin → 403; missing Origin → 403.
	if st, _, _ := c.do("POST", "/api/v1/admin/workspaces", ws, withOrigin("https://abidata.test")); st != 403 {
		t.Fatalf("wrong origin: %d", st)
	}
	if st, _, _ := c.do("POST", "/api/v1/admin/workspaces", ws, noOrigin); st != 403 {
		t.Fatalf("missing origin: %d", st)
	}
	// Correct → 201.
	if st, body, _ := c.do("POST", "/api/v1/admin/workspaces", ws); st != 201 {
		t.Fatalf("create: %d %v", st, body)
	}
	// Logout, then the session is gone.
	if st, _, _ := c.do("POST", "/api/v1/auth/logout", nil); st != 204 {
		t.Fatalf("logout %d", st)
	}
	if st, _, _ := c.do("GET", "/api/v1/auth/me", nil); st != 401 {
		t.Fatalf("me after logout: %d", st)
	}
}

func TestWorkspaceIsolationAndTraversal(t *testing.T) {
	e := newEnv(t)
	e.user("root@example.com", true)
	e.user("a@example.com", false)
	e.user("b@example.com", false)
	root := e.login("root@example.com")
	mk := func(name, owner string) string {
		st, body, _ := root.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": name, "owner_email": owner})
		if st != 201 {
			t.Fatalf("%d %v", st, body)
		}
		return body["id"].(string)
	}
	wa := mk("A", "a@example.com")
	wb := mk("B", "b@example.com")

	a := e.login("a@example.com")
	if st, body, _ := a.do("GET", "/api/v1/w/"+wa, nil); st != 200 || body["role"] != "owner" {
		t.Fatalf("own ws: %d %v", st, body)
	}
	// IDOR: member of A asking for B gets the same 404 as a nonexistent workspace.
	stB, bodyB, _ := a.do("GET", "/api/v1/w/"+wb, nil)
	stX, bodyX, _ := a.do("GET", "/api/v1/w/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	if stB != 404 || stX != 404 || errCode(bodyB) != "NOT_FOUND" || bodyB["error"].(map[string]any)["message"] != bodyX["error"].(map[string]any)["message"] {
		t.Fatalf("IDOR: %d %v / %d %v", stB, bodyB, stX, bodyX)
	}
	if st, _, _ := a.do("GET", "/api/v1/w/"+wb+"/members", nil); st != 404 {
		t.Fatalf("members of B: %d", st)
	}
	// Platform admins are not implicitly members.
	if st, _, _ := root.do("GET", "/api/v1/w/"+wa, nil); st != 404 {
		t.Fatalf("platform admin without membership: %d", st)
	}
	// Path traversal and malformed IDs → 404, never a file access.
	for _, p := range []string{"..%2Fx", "%2e%2e%2f%2e%2e%2fetc%2fpasswd", "..", "01arz3ndektsv4rrffq69g5fav", wa + "%00", "01ARZ3NDEKTSV4RRFFQ69G5FA%2F"} {
		st, body, _ := a.do("GET", "/api/v1/w/"+p, nil)
		if st != 404 {
			t.Errorf("%s: %d %v", p, st, body)
		}
	}
	// Non-admins cannot reach admin routes.
	if st, _, _ := a.do("GET", "/api/v1/admin/workspaces", nil); st != 403 {
		t.Fatalf("admin route as user: %d", st)
	}
}

func TestMembersRoleGuard(t *testing.T) {
	e := newEnv(t)
	e.user("root@example.com", true)
	e.user("o@example.com", false)
	e.user("v@example.com", false)
	root := e.login("root@example.com")
	_, body, _ := root.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "W", "owner_email": "o@example.com"})
	ws := body["id"].(string)
	o := e.login("o@example.com")
	if st, body, _ := o.do("POST", "/api/v1/w/"+ws+"/members", map[string]string{"email": "v@example.com", "role": "viewer"}); st != 201 {
		t.Fatalf("%d %v", st, body)
	}
	v := e.login("v@example.com")
	if st, _, _ := v.do("GET", "/api/v1/w/"+ws, nil); st != 200 {
		t.Fatal("viewer cannot read workspace")
	}
	if st, body, _ := v.do("GET", "/api/v1/w/"+ws+"/members", nil); st != 403 || errCode(body) != "FORBIDDEN" {
		t.Fatalf("viewer listed members: %d", st)
	}
	// The last owner cannot be removed or demoted.
	_, me, _ := o.do("GET", "/api/v1/auth/me", nil)
	oid := me["user"].(map[string]any)["id"].(string)
	if st, body, _ := o.do("PATCH", "/api/v1/w/"+ws+"/members/"+oid, map[string]string{"role": "editor"}); st != 400 ||
		!strings.Contains(body["error"].(map[string]any)["message"].(string), "at least one owner") {
		t.Fatalf("demoted last owner: %d %v", st, body)
	}
	if st, _, _ := o.do("DELETE", "/api/v1/w/"+ws+"/members/"+oid, nil); st != 400 {
		t.Fatalf("removed last owner: %d", st)
	}
}

func TestHeadersSPAAndValidation(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	resp, err := c.http.Get(e.srv.URL + "/some/deep/link")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "AbiData") || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("spa fallback: %d %q %v", resp.StatusCode, b, resp.Header)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "X-Request-ID"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("CSP wrong")
	}
	resp, _ = c.http.Get(e.srv.URL + "/assets/app-1a.js")
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset cache: %v", resp.Header.Get("Cache-Control"))
	}
	// Unknown API path → envelope 404.
	if st, body, _ := c.do("GET", "/api/v1/nope", nil); st != 404 || errCode(body) != "NOT_FOUND" {
		t.Fatalf("%d %v", st, body)
	}
	// Malformed body → BAD_REQUEST envelope (not huma's default shape).
	if st, body, _ := c.do("POST", "/api/v1/auth/login", map[string]any{"email": 5}); st != 400 || errCode(body) != "BAD_REQUEST" {
		t.Fatalf("%d %v", st, body)
	}
	// Health endpoints.
	for _, p := range []string{"/healthz", "/readyz"} {
		resp, _ := c.http.Get(e.srv.URL + p)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
	e.s.SetDraining()
	resp, _ = c.http.Get(e.srv.URL + "/readyz")
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Error("readyz not 503 while draining")
	}
}

func TestPWAStaticHeaders(t *testing.T) {
	e := newEnv(t)
	type res struct {
		StatusCode int
		Header     http.Header
	}
	get := func(p string) res {
		resp, err := http.Get(e.srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return res{resp.StatusCode, resp.Header}
	}
	if r := get("/manifest.webmanifest"); r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/manifest+json" || r.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("manifest: %d %v", r.StatusCode, r.Header)
	}
	if r := get("/sw.js"); r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-cache" || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/javascript") {
		t.Errorf("sw.js: %d %v", r.StatusCode, r.Header)
	}
	if r := get("/icons/icon-192.png"); r.StatusCode != 200 || !strings.Contains(r.Header.Get("Cache-Control"), "max-age=86400") {
		t.Errorf("icon: %d %v", r.StatusCode, r.Header)
	}
	if r := get("/"); r.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("index cache: %v", r.Header)
	}
	if r := get("/assets/app-1a.js"); !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset cache: %v", r.Header)
	}
	csp := get("/").Header.Get("Content-Security-Policy")
	for _, d := range []string{"manifest-src 'self'", "worker-src 'self'", "default-src 'self'"} {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP lacks %q: %s", d, csp)
		}
	}
	// Missing build artifacts are 404, never the SPA shell.
	for _, p := range []string{"/offline.html", "/favicon.svg", "/icons/nope.png", "/assets/nope.js"} {
		if r := get(p); r.StatusCode != 404 {
			t.Errorf("%s: %d, want 404", p, r.StatusCode)
		}
	}
}

func TestBootstrapSetupFlow(t *testing.T) {
	e := newEnv(t)
	tok, err := e.au.PrepareBootstrap(context.Background(), "first@example.com")
	if err != nil || tok == "" {
		t.Fatal(err)
	}
	c := e.client()
	if st, body, _ := c.do("GET", "/api/v1/auth/setup", nil); st != 200 || body["pending"] != true {
		t.Fatalf("%d %v", st, body)
	}
	st, body, _ := c.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok, "display_name": "First", "password": "long-enough-pw"})
	if st != 200 || body["user"].(map[string]any)["is_platform_admin"] != true {
		t.Fatalf("%d %v", st, body)
	}
	c.csrf = body["csrf_token"].(string)
	if st, _, _ := c.do("GET", "/api/v1/admin/users", nil); st != 200 {
		t.Fatal("setup did not sign in as admin")
	}
}

// rawJSON is sent verbatim (to test number precision).
type rawJSON string

func urlEscape(s string) string { return url.QueryEscape(s) }

// Every registered schema must have a unique name (huma panics lazily otherwise).
func TestOpenAPIDocumentBuilds(t *testing.T) {
	e := newEnv(t)
	b, err := json.Marshal(e.s.API().OpenAPI())
	if err != nil || len(b) < 1000 {
		t.Fatalf("%d bytes, %v", len(b), err)
	}
}

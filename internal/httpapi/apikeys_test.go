package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/auth"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// bearer talks to the API the way an external bot does: no cookie jar, no
// Origin, no CSRF token, only an Authorization header.
type bearer struct {
	e     *env
	token string
}

func (b bearer) do(method, path string, body any, hdr ...string) (int, map[string]any, http.Header) {
	b.e.t.Helper()
	var rd io.Reader
	if body != nil {
		enc, _ := json.Marshal(body)
		rd = strings.NewReader(string(enc))
	}
	req, _ := http.NewRequest(method, b.e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, resp.Header
}

type keyEnv struct {
	*env
	ws, other string
	owner     *client
	builder   *client
	table     string
	col       string
}

// newKeyEnv builds two workspaces (A with an owner and a table, B with its own
// owner) and returns helpers to mint keys.
func newKeyEnv(t *testing.T) *keyEnv {
	t.Helper()
	e := newEnv(t)
	ws, root, owner, b, _, _ := roles(t, e)
	e.user("o2@example.com", false)
	_, body, _ := root.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Other", "owner_email": "o2@example.com"})
	k := &keyEnv{env: e, ws: ws, other: body["id"].(string), owner: owner, builder: b}
	_, res, _ := b.do("POST", "/api/v1/w/"+ws+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Notes", "track_history": true, "columns": []map[string]any{{"display_name": "Text", "field_type": "short_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	k.table = tbl["id"].(string)
	k.col = tbl["columns"].([]any)[1].(map[string]any)["id"].(string)
	return k
}

func (k *keyEnv) rows() string { return "/api/v1/w/" + k.ws + "/tables/" + k.table + "/rows" }

// mint creates a key through the owner's session and returns its id and token.
func (k *keyEnv) mint(name, role string, extra map[string]any) (string, string) {
	k.t.Helper()
	body := map[string]any{"name": name, "role": role}
	for a, v := range extra {
		body[a] = v
	}
	st, out, _ := k.owner.do("POST", "/api/v1/w/"+k.ws+"/api-keys", body)
	if st != 201 {
		k.t.Fatalf("create key: %d %v", st, out)
	}
	return out["id"].(string), out["token"].(string)
}

func TestAPIKeyLifecycleAndAttribution(t *testing.T) {
	k := newKeyEnv(t)
	id, tok := k.mint("Inventory bot", "editor", map[string]any{"expires_in_days": 30})
	if !strings.HasPrefix(tok, "abd_"+id+"_") {
		t.Fatalf("token shape %q", tok)
	}
	// Listing never carries the secret or the hash.
	rawList := func() string {
		req, _ := http.NewRequest("GET", k.srv.URL+"/api/v1/w/"+k.ws+"/api-keys", nil)
		resp, err := k.owner.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	list := rawList()
	secret := tok[len("abd_"+id+"_"):]
	if strings.Contains(list, secret) || strings.Contains(strings.ToLower(list), "hash") || strings.Contains(list, "token") {
		t.Fatalf("list leaks secret material: %s", list)
	}
	st, lb, _ := k.owner.do("GET", "/api/v1/w/"+k.ws+"/api-keys", nil)
	it := lb["items"].([]any)[0].(map[string]any)
	if st != 200 || it["name"] != "Inventory bot" || it["role"] != "editor" || it["revoked"] != false ||
		it["expires_at"] == nil || it["created_by_name"] == "" || it["last_used_at"] != nil {
		t.Fatalf("list %d %v", st, lb)
	}

	bot := bearer{k.env, tok}
	// Read the schema, insert and read a row: no Origin, no CSRF.
	if st, b, _ := bot.do("GET", "/api/v1/w/"+k.ws+"/schema", nil); st != 200 {
		t.Fatalf("schema %d %v", st, b)
	}
	st, row, h := bot.do("POST", k.rows(), map[string]any{"fields": map[string]any{k.col: "from bot"}}, "Idempotency-Key", "k1")
	if st != 201 || h.Get("ETag") != `"1"` {
		t.Fatalf("insert %d %v", st, row)
	}
	rid := fmt.Sprint(row["id"])
	// Update with If-Match, then a stale update gets 409.
	if st, b, _ := bot.do("PATCH", k.rows()+"/"+rid, map[string]any{"fields": map[string]any{k.col: "v2"}}, "If-Match", `"1"`); st != 200 {
		t.Fatalf("patch %d %v", st, b)
	}
	if st, b, _ := bot.do("PATCH", k.rows()+"/"+rid, map[string]any{"fields": map[string]any{k.col: "v3"}}, "If-Match", `"1"`); st != 409 {
		t.Fatalf("stale patch %d %v", st, b)
	}
	// Row history attributes the writes to the key, with its name.
	st, hist, _ := k.owner.do("GET", k.rows()+"/"+rid+"/history", nil)
	items := hist["items"].([]any)
	first := items[0].(map[string]any)
	if st != 200 || len(items) != 2 || first["user_id"] != "apikey:"+id || first["user_name"] != `API key "Inventory bot"` {
		t.Fatalf("history %d %v", st, hist)
	}
	// last_used_at is recorded once, not per request.
	ks, _ := k.ad.ListAPIKeys(context.Background(), ids.WorkspaceID(k.ws))
	if ks[0].LastUsedAt == nil {
		t.Fatal("last_used_at not set")
	}
	first1 := *ks[0].LastUsedAt
	for range 3 {
		bot.do("GET", k.rows(), nil)
	}
	ks, _ = k.ad.ListAPIKeys(context.Background(), ids.WorkspaceID(k.ws))
	if !ks[0].LastUsedAt.Equal(first1) {
		t.Fatalf("last_used_at rewritten within a minute: %v -> %v", first1, ks[0].LastUsedAt)
	}
	// Audit: create and revoke are recorded.
	if st, _, _ := k.owner.do("DELETE", "/api/v1/w/"+k.ws+"/api-keys/"+id, nil); st != 204 {
		t.Fatalf("revoke %d", st)
	}
	if st, _, _ := k.owner.do("DELETE", "/api/v1/w/"+k.ws+"/api-keys/"+id, nil); st != 204 {
		t.Fatalf("revoke twice %d", st)
	}
	au, _ := k.ad.ListAudit(context.Background(), 20)
	var sawCreate, sawRevoke bool
	for _, a := range au {
		sawCreate = sawCreate || (a.Action == "apikey.create" && a.ObjectID == id)
		sawRevoke = sawRevoke || (a.Action == "apikey.revoke" && a.ObjectID == id)
	}
	if !sawCreate || !sawRevoke {
		t.Fatalf("audit %v", au)
	}
	// After revocation: 401 immediately, and history still names the key.
	if st, b, _ := bot.do("GET", k.rows(), nil); st != 401 || errCode(b) != "UNAUTHENTICATED" {
		t.Fatalf("revoked key %d %v", st, b)
	}
	_, hist, _ = k.owner.do("GET", k.rows()+"/"+rid+"/history", nil)
	if hist["items"].([]any)[0].(map[string]any)["user_name"] != `API key "Inventory bot"` {
		t.Fatalf("history after revoke %v", hist)
	}
}

func TestAPIKeyRoleCapAndRouteScope(t *testing.T) {
	k := newKeyEnv(t)
	base := "/api/v1/w/" + k.ws
	_, viewer := k.mint("v", "viewer", nil)
	_, editor := k.mint("e", "editor", nil)
	_, builder := k.mint("b", "builder", nil)
	row := map[string]any{"fields": map[string]any{k.col: "x"}}

	// The permissions matrix applies unchanged.
	if st, _, _ := (bearer{k.env, viewer}).do("GET", k.rows(), nil); st != 200 {
		t.Fatalf("viewer read %d", st)
	}
	if st, b, _ := (bearer{k.env, viewer}).do("POST", k.rows(), row); st != 403 {
		t.Fatalf("viewer write %d %v", st, b)
	}
	if st, _, _ := (bearer{k.env, editor}).do("POST", k.rows(), row); st != 201 {
		t.Fatalf("editor write %d", st)
	}
	ops := map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{"display_name": "More",
		"columns": []map[string]any{{"display_name": "A", "field_type": "short_text"}}}}}}
	if st, _, _ := (bearer{k.env, editor}).do("POST", base+"/schema/apply", ops); st != 403 {
		t.Fatalf("editor schema %d", st)
	}
	if st, b, _ := (bearer{k.env, builder}).do("POST", base+"/schema/apply", ops); st != 200 {
		t.Fatalf("builder schema %d %v", st, b)
	}

	// Owner-only routes are out of reach for every key role.
	for _, tok := range []string{viewer, editor, builder} {
		for _, c := range [][2]string{{"GET", base + "/members"}, {"POST", base + "/members"}, {"GET", base + "/backups"},
			{"POST", base + "/backups"}, {"GET", base + "/api-keys"}, {"POST", base + "/api-keys"}} {
			var body any
			if c[0] == "POST" {
				body = map[string]any{"name": "x", "role": "viewer", "email": "a@b.co"}
			}
			if st, _, _ := (bearer{k.env, tok}).do(c[0], c[1], body); st != 403 {
				t.Errorf("%s %s with key: %d", c[0], c[1], st)
			}
		}
	}
	// A key cannot mint or revoke keys (even a builder's own).
	id, _ := k.mint("victim", "viewer", nil)
	if st, _, _ := (bearer{k.env, builder}).do("DELETE", base+"/api-keys/"+id, nil); st != 403 {
		t.Fatalf("key revoked a key: %d", st)
	}

	// Admin, auth, metrics and the log stream never accept a key.
	for _, p := range []string{"/api/v1/admin/workspaces", "/api/v1/admin/api-keys", "/api/v1/admin/users", "/api/v1/auth/me", "/api/v1/auth/logout"} {
		method := "GET"
		if p == "/api/v1/auth/logout" {
			method = "POST"
		}
		if st, _, _ := (bearer{k.env, builder}).do(method, p, nil); st != 403 {
			t.Errorf("%s with key: %d", p, st)
		}
	}
	for _, p := range []string{"/metrics", "/api/v1/admin/logs/stream"} {
		if st, _, _ := (bearer{k.env, builder}).do("GET", p, nil); st != 401 {
			t.Errorf("%s with key: %d", p, st)
		}
	}
	// Even a public route refuses a key (login must not run without Origin checks).
	if st, _, _ := (bearer{k.env, builder}).do("POST", "/api/v1/auth/login", map[string]string{"email": "o@example.com", "password": "long-enough-pw"}); st != 403 {
		t.Errorf("login with key: %d", st)
	}

	// Another workspace answers exactly like it does for a non-member.
	other := "/api/v1/w/" + k.other
	st1, b1, _ := (bearer{k.env, builder}).do("GET", other, nil)
	st2, b2, _ := (bearer{k.env, builder}).do("GET", "/api/v1/w/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil)
	if st1 != 404 || st2 != 404 || b1["error"].(map[string]any)["message"] != b2["error"].(map[string]any)["message"] {
		t.Fatalf("other workspace %d %v / %d %v", st1, b1, st2, b2)
	}
	if st, _, _ := (bearer{k.env, builder}).do("GET", other+"/schema", nil); st != 404 {
		t.Fatalf("other schema %d", st)
	}
	// The role stored for a key can never be owner.
	if st, _, _ := k.owner.do("POST", base+"/api-keys", map[string]any{"name": "o", "role": "owner"}); st != 422 && st != 400 {
		t.Fatalf("owner key accepted: %d", st)
	}
}

func TestAPIKeyBadCredentialsAreIndistinguishable(t *testing.T) {
	k := newKeyEnv(t)
	id, tok := k.mint("a", "viewer", nil)
	revID, revTok := k.mint("r", "viewer", nil)
	expID, expTok := k.mint("x", "viewer", nil)
	if st, _, _ := k.owner.do("DELETE", "/api/v1/w/"+k.ws+"/api-keys/"+revID, nil); st != 204 {
		t.Fatal("revoke")
	}
	// Expire a key by rewriting its expiry in the past.
	if err := k.ad.Write(context.Background(), func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE _adm_api_keys SET expires_at = '2020-01-01T00:00:00.000Z' WHERE id = ?`, expID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	secret := tok[len("abd_"+id+"_"):]
	wrongSecret := "abd_" + id + "_" + strings.Repeat("A", len(secret))
	unknown := "abd_01ARZ3NDEKTSV4RRFFQ69G5FAV_" + secret
	cases := map[string]string{"wrong secret": wrongSecret, "unknown id": unknown, "revoked": revTok, "expired": expTok,
		"malformed": "abd_nonsense", "wrong prefix": "xyz_" + id + "_" + secret, "empty": "", "truncated": tok[:len(tok)-1], "extra": tok + "A"}
	var msg string
	for name, bad := range cases {
		req, _ := http.NewRequest("GET", k.srv.URL+k.rows(), nil)
		req.Header.Set("Authorization", "Bearer "+bad)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != 401 || errCode(body) != "UNAUTHENTICATED" {
			t.Errorf("%s: %d %v", name, resp.StatusCode, body)
			continue
		}
		m := body["error"].(map[string]any)["message"].(string)
		if msg == "" {
			msg = m
		} else if m != msg {
			t.Errorf("%s: message differs: %q vs %q", name, m, msg)
		}
	}
	// Other schemes and a lowercase scheme.
	for _, h := range []string{"Basic " + tok, tok, "Bearer", "Token " + tok} {
		req, _ := http.NewRequest("GET", k.srv.URL+k.rows(), nil)
		req.Header.Set("Authorization", h)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%q: %d", h, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", k.srv.URL+k.rows(), nil)
	req.Header.Set("Authorization", "bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("lowercase scheme: %d", resp.StatusCode)
	}
	_ = expID
}

func TestAPIKeyHeaderOverridesCookies(t *testing.T) {
	k := newKeyEnv(t)
	_, tok := k.mint("a", "viewer", nil)
	// The owner's valid session cookie must not rescue a bad key.
	req := func(c *client, token string) int {
		r, _ := http.NewRequest("GET", k.srv.URL+k.rows(), nil)
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := c.http.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if st := req(k.owner, "abd_garbage"); st != 401 {
		t.Fatalf("cookie rescued a bad key: %d", st)
	}
	if st := req(k.owner, tok); st != 200 {
		t.Fatalf("valid key with cookie: %d", st)
	}
	// A valid key for workspace A, sent by an owner of workspace B, is still
	// scoped to A: the session's memberships do not widen it.
	o2 := k.login("o2@example.com")
	r, _ := http.NewRequest("GET", k.srv.URL+"/api/v1/w/"+k.other, nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := o2.http.Do(r)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("session membership widened a key: %d", resp.StatusCode)
	}
}

// A proxy in front of the whole site may add Basic auth; that is not an API
// key attempt and must not disturb cookie sessions.
func TestNonBearerAuthorizationLeavesCookiesAlone(t *testing.T) {
	k := newKeyEnv(t)
	r, _ := http.NewRequest("GET", k.srv.URL+k.rows(), nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	resp, err := k.owner.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("cookie session with Basic header: %d", resp.StatusCode)
	}
	// It does not unlock anything either: unsafe requests still need CSRF.
	if st, _, _ := k.owner.do("POST", k.rows(), map[string]any{"fields": map[string]any{k.col: "x"}}, noCSRF,
		func(r *http.Request) { r.Header.Set("Authorization", "Basic dXNlcjpwYXNz") }); st != 403 {
		t.Fatalf("Basic header skipped CSRF: %d", st)
	}
}

func TestAPIKeyCSRFAndCORS(t *testing.T) {
	k := newKeyEnv(t)
	_, tok := k.mint("a", "editor", nil)
	row := map[string]any{"fields": map[string]any{k.col: "x"}}
	bot := bearer{k.env, tok}
	// Bearer writes need no Origin/CSRF token (and tolerate a foreign Origin: it is not consulted).
	if st, b, _ := bot.do("POST", k.rows(), row); st != 201 {
		t.Fatalf("no origin %d %v", st, b)
	}
	if st, b, _ := bot.do("POST", k.rows(), row, "Origin", "http://evil.test"); st != 201 {
		t.Fatalf("foreign origin %d %v", st, b)
	}
	// Cookie sessions are unchanged: CSRF token and Origin still required.
	if st, _, _ := k.owner.do("POST", k.rows(), row, noCSRF); st != 403 {
		t.Fatalf("cookie without csrf %d", st)
	}
	if st, _, _ := k.owner.do("POST", k.rows(), row, withOrigin("http://evil.test")); st != 403 {
		t.Fatalf("cookie with foreign origin %d", st)
	}
	if st, _, _ := k.owner.do("POST", k.rows(), row); st != 201 {
		t.Fatalf("cookie ok %d", st)
	}
	// No CORS headers anywhere: a browser page on another origin cannot even
	// preflight an Authorization header.
	for _, c := range []struct{ method, path string }{{"OPTIONS", k.rows()}, {"GET", k.rows()}, {"OPTIONS", "/api/v1/auth/login"}} {
		r, _ := http.NewRequest(c.method, k.srv.URL+c.path, nil)
		r.Header.Set("Origin", "http://evil.test")
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
		r.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		for h := range resp.Header {
			if strings.HasPrefix(strings.ToLower(h), "access-control-") {
				t.Errorf("%s %s sends %s", c.method, c.path, h)
			}
		}
	}
}

func TestAPIKeyWorkspaceStates(t *testing.T) {
	k := newKeyEnv(t)
	_, tok := k.mint("a", "editor", nil)
	bot := bearer{k.env, tok}
	ctx := context.Background()
	set := func(s admin.WorkspaceStatus) {
		if err := k.ad.UpdateWorkspace(ctx, ids.WorkspaceID(k.ws), admin.WorkspacePatch{Status: &s}); err != nil {
			t.Fatal(err)
		}
	}
	set(admin.StatusSuspended)
	st, b, _ := bot.do("GET", k.rows(), nil)
	stSess, bSess, _ := k.owner.do("GET", k.rows(), nil)
	if st != 403 || stSess != 403 || b["error"].(map[string]any)["message"] != bSess["error"].(map[string]any)["message"] {
		t.Fatalf("suspended: key %d %v, session %d %v", st, b, stSess, bSess)
	}
	set(admin.StatusRestoring)
	st, _, h := bot.do("GET", k.rows(), nil)
	if st != 503 || h.Get("Retry-After") == "" {
		t.Fatalf("restoring %d %v", st, h)
	}
	set(admin.StatusDeleted)
	if st, _, _ := bot.do("GET", k.rows(), nil); st != 404 {
		t.Fatalf("deleted %d", st)
	}
}

func TestAPIKeyRateLimits(t *testing.T) {
	k := newKeyEnv(t)
	_, tok := k.mint("a", "viewer", nil)
	_, tok2 := k.mint("b", "viewer", nil)
	k.au.KeyRatePerMinute = 60 // burst 10, 1 per second
	bot := bearer{k.env, tok}
	limited := 0
	var retry string
	for range 14 {
		st, b, h := bot.do("GET", k.rows(), nil)
		if st == 429 {
			limited++
			retry = h.Get("Retry-After")
			if errCode(b) != "RATE_LIMITED" {
				t.Fatalf("429 body %v", b)
			}
		}
	}
	if limited == 0 || retry == "" {
		t.Fatalf("no per-key limit (limited %d, retry %q)", limited, retry)
	}
	// Budgets are per key.
	if st, _, _ := (bearer{k.env, tok2}).do("GET", k.rows(), nil); st != 200 {
		t.Fatalf("other key limited too: %d", st)
	}

	// Failed attempts throttle the client address, valid key or not.
	bad := bearer{k.env, "abd_01ARZ3NDEKTSV4RRFFQ69G5FAV_" + strings.Repeat("a", 43)}
	var last int
	var lastHdr http.Header
	for range auth.KeyFailMax + 5 {
		last, _, lastHdr = bad.do("GET", k.rows(), nil)
	}
	if last != 429 || lastHdr.Get("Retry-After") == "" {
		t.Fatalf("failed attempts not throttled: %d", last)
	}
	if st, _, _ := (bearer{k.env, tok2}).do("GET", k.rows(), nil); st != 429 {
		t.Fatalf("throttled address still served: %d", st)
	}
}

func TestAPIKeyCapAndOwnerRemoval(t *testing.T) {
	k := newKeyEnv(t)
	for i := range admin.MaxActiveAPIKeys {
		k.mint(fmt.Sprintf("k%d", i), "viewer", nil)
	}
	if st, b, _ := k.owner.do("POST", "/api/v1/w/"+k.ws+"/api-keys", map[string]any{"name": "one too many", "role": "viewer"}); st != 400 {
		t.Fatalf("cap %d %v", st, b)
	}
	// Revoking frees a slot.
	_, lb, _ := k.owner.do("GET", "/api/v1/w/"+k.ws+"/api-keys", nil)
	first := lb["items"].([]any)[0].(map[string]any)["id"].(string)
	k.owner.do("DELETE", "/api/v1/w/"+k.ws+"/api-keys/"+first, nil)
	if st, _, _ := k.owner.do("POST", "/api/v1/w/"+k.ws+"/api-keys", map[string]any{"name": "fits", "role": "viewer"}); st != 201 {
		t.Fatalf("after revoke %d", st)
	}
	// Another workspace's owner cannot see or revoke these keys.
	o2 := k.login("o2@example.com")
	if st, _, _ := o2.do("DELETE", "/api/v1/w/"+k.other+"/api-keys/"+lb["items"].([]any)[1].(map[string]any)["id"].(string), nil); st != 404 {
		t.Fatalf("cross-workspace revoke %d", st)
	}
	if st, _, _ := o2.do("GET", "/api/v1/w/"+k.ws+"/api-keys", nil); st != 404 {
		t.Fatalf("cross-workspace list %d", st)
	}
	// Non-owners cannot manage keys.
	if st, _, _ := k.builder.do("GET", "/api/v1/w/"+k.ws+"/api-keys", nil); st != 403 {
		t.Fatalf("builder listed keys %d", st)
	}
}

func TestAPIKeysSurviveOwnerRemovalAndAdminCanRevoke(t *testing.T) {
	k := newKeyEnv(t)
	id, tok := k.mint("durable", "viewer", nil)
	// Promote the builder, then remove the original owner.
	_, me, _ := k.owner.do("GET", "/api/v1/auth/me", nil)
	oid := me["user"].(map[string]any)["id"].(string)
	_, bme, _ := k.builder.do("GET", "/api/v1/auth/me", nil)
	bid := bme["user"].(map[string]any)["id"].(string)
	if st, b, _ := k.owner.do("PATCH", "/api/v1/w/"+k.ws+"/members/"+bid, map[string]string{"role": "owner"}); st != 200 {
		t.Fatalf("promote %d %v", st, b)
	}
	if st, b, _ := k.builder.do("DELETE", "/api/v1/w/"+k.ws+"/members/"+oid, nil); st != 204 {
		t.Fatalf("remove %d %v", st, b)
	}
	if st, _, _ := (bearer{k.env, tok}).do("GET", k.rows(), nil); st != 200 {
		t.Fatalf("key died with its creator's membership: %d", st)
	}
	// A platform admin lists and revokes any workspace's keys.
	root := k.login("root@example.com")
	st, lb, _ := root.do("GET", "/api/v1/admin/api-keys?workspace="+k.ws, nil)
	items := lb["items"].([]any)
	if st != 200 || len(items) != 1 || items[0].(map[string]any)["workspace_name"] != "Jobs" {
		t.Fatalf("admin list %d %v", st, lb)
	}
	if st, _, _ := root.do("GET", "/api/v1/admin/api-keys?workspace=bad", nil); st != 400 {
		t.Fatalf("bad workspace %d", st)
	}
	if st, _, _ := k.builder.do("GET", "/api/v1/admin/api-keys", nil); st != 403 {
		t.Fatalf("non-admin list %d", st)
	}
	if st, _, _ := root.do("DELETE", "/api/v1/admin/api-keys/"+id, nil); st != 204 {
		t.Fatalf("admin revoke %d", st)
	}
	if st, _, _ := (bearer{k.env, tok}).do("GET", k.rows(), nil); st != 401 {
		t.Fatalf("revoked by admin: %d", st)
	}
	if st, _, _ := root.do("DELETE", "/api/v1/admin/api-keys/nope", nil); st != 404 {
		t.Fatalf("unknown %d", st)
	}
}

func TestAPIKeyEventsStreamAndRevocation(t *testing.T) {
	k := newKeyEnv(t)
	k.s.keyRecheck = 100 * time.Millisecond
	id, tok := k.mint("streamer", "viewer", nil)
	base := "/api/v1/w/" + k.ws

	// Without a valid key: 401, and cookies are ignored when a header is present.
	for _, token := range []string{"abd_garbage"} {
		r, _ := http.NewRequest("GET", k.srv.URL+base+"/events", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := k.owner.http.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("bad key on events (with cookie): %d", resp.StatusCode)
		}
	}
	// A key for another workspace gets 404.
	_, otherTok := k.mint("x", "viewer", nil)
	r, _ := http.NewRequest("GET", k.srv.URL+"/api/v1/w/"+k.other+"/events", nil)
	r.Header.Set("Authorization", "Bearer "+otherTok)
	foreign, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Body.Close()
	if foreign.StatusCode != 404 {
		t.Fatalf("events of other workspace: %d", foreign.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", k.srv.URL+base+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed by the defer below
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("events %d", resp.StatusCode)
	}
	got := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if ev, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				got <- ev
			}
		}
	}()
	select {
	case ev := <-got:
		if ev != "hello" {
			t.Fatalf("first event %q", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no hello")
	}
	// A write by the owner reaches the key's stream.
	k.owner.do("POST", k.rows(), map[string]any{"fields": map[string]any{k.col: "live"}})
	select {
	case ev := <-got:
		if ev == "" || ev == "hello" {
			t.Fatalf("event %q", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	// Revocation ends the stream at the next recheck.
	if st, _, _ := k.owner.do("DELETE", base+"/api-keys/"+id, nil); st != 204 {
		t.Fatalf("revoke %d", st)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream survived revocation")
	}
}

func TestAPIKeyBuilderRunsJobAttributedToKey(t *testing.T) {
	k := newKeyEnv(t)
	id, tok := k.mint("ci", "builder", nil)
	bot := bearer{k.env, tok}
	base := "/api/v1/w/" + k.ws
	st, sc, _ := bot.do("POST", base+"/scripts", map[string]any{"name": "Hello", "kind": "job", "source": `log.info("hi")`})
	if st != 201 {
		t.Fatalf("script %d %v", st, sc)
	}
	st, job, _ := bot.do("POST", base+"/jobs", map[string]any{"name": "Hello", "script_id": sc["id"]})
	if st != 201 {
		t.Fatalf("job %d %v", st, job)
	}
	st, run, _ := bot.do("POST", base+"/jobs/"+job["id"].(string)+"/run", nil)
	if st != 202 || run["triggered_by"] != "apikey:"+id {
		t.Fatalf("run %d %v", st, run)
	}
}

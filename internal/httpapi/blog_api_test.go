package httpapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// blogEnv signs in a user with a blog and returns their client and blog workspace.
func blogEnv(t *testing.T) (*env, *client, string) {
	t.Helper()
	e := newEnv(t)
	e.user("ann@example.com", false)
	c := e.login("ann@example.com")
	_, me, _ := c.do("GET", "/api/v1/auth/me", nil)
	m := blogMembership(t, me)
	if m == nil {
		t.Fatalf("no blog: %v", me)
	}
	return e, c, m["workspace_id"].(string)
}

func TestBlogAuthoringAPI(t *testing.T) {
	e, c, ws := blogEnv(t)
	base := "/api/v1/w/" + ws + "/blog"

	st, info, _ := c.do("GET", base, nil)
	if st != 200 || info["username"] != "ann" || info["published"] != false || info["public_url"] != testOrigin+"/b/ann" || info["public_blogs_enabled"] != false {
		t.Fatalf("info %d %v", st, info)
	}
	if st, info, _ := c.do("PATCH", base, map[string]any{"title": "Ann writes", "intro": "Hello.\r\n\r\nSecond."}); st != 200 || info["title"] != "Ann writes" || info["intro"] != "Hello.\n\nSecond." {
		t.Fatalf("patch %d %v", st, info)
	}

	// New post: a draft with a slug from the title.
	st, p, _ := c.do("POST", base+"/posts", map[string]any{"title": "Hello, World", "body_md": "# Hi\n\n<script>x</script>"})
	if st != 201 || p["status"] != "draft" || p["slug"] != "hello-world" || p["chip"] != "draft" || p["public_url"] != testOrigin+"/b/ann/hello-world" {
		t.Fatalf("create %d %v", st, p)
	}
	id := int64(p["id"].(float64))
	ver := int64(p["_version"].(float64))
	postURL := fmt.Sprintf("%s/posts/%d", base, id)

	if st, chk, _ := c.do("GET", base+"/slug-check?slug=Hello%20World", nil); st != 200 || chk["slug"] != "hello-world-2" || chk["available"] != false {
		t.Fatalf("slug check %d %v", st, chk)
	}
	if st, chk, _ := c.do("GET", fmt.Sprintf("%s/slug-check?slug=hello-world&id=%d", base, id), nil); st != 200 || chk["available"] != true {
		t.Fatalf("own slug %d %v", st, chk)
	}

	// Saves need the current _version.
	st, p, _ = c.do("PATCH", postURL, map[string]any{"_version": ver, "summary": "A greeting.", "slug": "Greetings!"})
	if st != 200 || p["summary"] != "A greeting." || p["slug"] != "greetings" {
		t.Fatalf("update %d %v", st, p)
	}
	if st, body, _ := c.do("PATCH", postURL, map[string]any{"_version": ver, "summary": "stale"}); st != 409 || errCode(body) != "CONCURRENT_MODIFICATION" {
		t.Fatalf("stale save %d %v", st, body)
	}
	ver = int64(p["_version"].(float64))

	// The preview uses the public pipeline and its own no-script CSP.
	st, pv, _ := c.do("POST", base+"/preview", map[string]any{"title": "<b>T</b>", "body_md": "**bold** <script>alert(1)</script> ![x](attachment:01J0000000000000000000000A)"})
	html, _ := pv["html"].(string)
	if st != 200 || !strings.Contains(html, "<strong>bold</strong>") || strings.Contains(html, "<script>") || !strings.Contains(html, "default-src 'none'") ||
		!strings.Contains(html, "&lt;b&gt;T&lt;/b&gt;") || !strings.Contains(html, `/api/v1/w/`+ws+`/files/01J0000000000000000000000A/content`) {
		t.Fatalf("preview %d %s", st, html)
	}

	// Publish: published_at is set.
	st, p, _ = c.do("PUT", postURL+"/status", map[string]any{"_version": ver, "status": "published"})
	if st != 200 || p["status"] != "published" || p["published_at"] == nil {
		t.Fatalf("publish %d %v", st, p)
	}
	ver = int64(p["_version"].(float64))
	if st, info, _ := c.do("PUT", base+"/published", map[string]any{"published": true}); st != 200 || info["published"] != true || info["published_at"] == nil {
		t.Fatalf("blog on %d %v", st, info)
	}

	// The list shows chips; a row the renderer would refuse needs attention.
	_, schema, _ := c.do("GET", "/api/v1/w/"+ws+"/schema", nil)
	var postsTable, slugCol, titleCol string
	for _, tb := range schema["tables"].([]any) {
		tb := tb.(map[string]any)
		if tb["physical_name"] == "posts" {
			postsTable = tb["id"].(string)
			for _, col := range tb["columns"].([]any) {
				col := col.(map[string]any)
				switch col["physical_name"] {
				case "slug":
					slugCol = col["id"].(string)
				case "title":
					titleCol = col["id"].(string)
				}
			}
		}
	}
	// Parity: a write through the records API gets the same post rules.
	st, row, _ := c.do("POST", "/api/v1/w/"+ws+"/tables/"+postsTable+"/rows", map[string]any{"fields": map[string]any{titleCol: "From the Datasheet", slugCol: "Greetings"}})
	if st != 201 || row["fields"].(map[string]any)[slugCol] != "greetings-2" {
		t.Fatalf("records insert %d %v", st, row)
	}
	st, list, _ := c.do("GET", base+"/posts", nil)
	items, _ := list["items"].([]any)
	if st != 200 || len(items) != 2 || items[0].(map[string]any)["title"] != "From the Datasheet" || items[1].(map[string]any)["chip"] != "published" {
		t.Fatalf("list %d %v", st, list)
	}
	if st, list, _ := c.do("GET", base+"/posts?status=draft&q=datasheet", nil); st != 200 || len(list["items"].([]any)) != 1 {
		t.Fatalf("filtered list %d %v", st, list)
	}

	// Unpublish, then delete.
	st, p, _ = c.do("PUT", postURL+"/status", map[string]any{"_version": ver, "status": "draft"})
	if st != 200 || p["status"] != "draft" {
		t.Fatalf("unpublish %d %v", st, p)
	}
	ver = int64(p["_version"].(float64))
	if st, body, _ := c.do("DELETE", fmt.Sprintf("%s?version=%d", postURL, ver), nil); st != 204 {
		t.Fatalf("delete %d %v", st, body)
	}
	if st, _, _ := c.do("GET", postURL, nil); st != 404 {
		t.Fatalf("deleted post %d", st)
	}

	audit, _ := e.ad.ListAudit(context.Background(), 20)
	found := false
	for _, a := range audit {
		found = found || a.Action == "blog.publish"
	}
	if !found {
		t.Fatal("no blog.publish audit row")
	}
}

func TestBlogAPIAccess(t *testing.T) {
	e, ann, ws := blogEnv(t)
	base := "/api/v1/w/" + ws + "/blog"
	e.user("bob@example.com", true)
	bob := e.login("bob@example.com")
	// Another user, even a platform admin, gets the normal not-found.
	if st, _, _ := bob.do("GET", base, nil); st != 404 {
		t.Fatalf("other user %d", st)
	}
	// A workspace that is not a blog has no blog routes.
	st, w, _ := bob.do("POST", "/api/v1/admin/workspaces", map[string]any{"name": "Plain"})
	if st != 201 {
		t.Fatalf("%d %v", st, w)
	}
	if st, _, _ := bob.do("GET", "/api/v1/w/"+w["id"].(string)+"/blog", nil); st != 404 {
		t.Fatalf("plain workspace %d", st)
	}
	// API keys are never the owner, so the blog API refuses them.
	st, k, _ := ann.do("POST", "/api/v1/w/"+ws+"/api-keys", map[string]any{"name": "bot", "role": "builder"})
	if st != 201 {
		t.Fatalf("key %d %v", st, k)
	}
	key := bearer{e: e, token: k["token"].(string)}
	for _, r := range []struct{ method, path string }{{"GET", base}, {"PUT", base + "/published"}, {"PUT", base + "/posts/1/status"}} {
		if st, _, _ := key.do(r.method, r.path, map[string]any{"published": true, "status": "published", "_version": 1}); st != 403 {
			t.Errorf("key %s %s: %d", r.method, r.path, st)
		}
	}

	// At most 10 publish changes an hour.
	codes := []int{}
	for i := 0; i < 11; i++ {
		st, _, h := ann.do("PUT", base+"/published", map[string]any{"published": i%2 == 0})
		codes = append(codes, st)
		if i == 10 && h.Get("Retry-After") == "" {
			t.Error("no Retry-After")
		}
	}
	if codes[9] != 200 || codes[10] != 429 {
		t.Fatalf("rate limit: %v", codes)
	}
}

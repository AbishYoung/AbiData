package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/apps"
	"abidata/internal/blogs"
)

// visitor fetches a public blog URL with a fresh client: no cookies, no
// redirects followed. extra headers (a cookie, a bearer) must change nothing.
func (e *env) visit(path string, extra map[string]string) (int, string, http.Header) {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

// publicBlog gives ann a live blog with one published post, one unlisted and one draft.
func publicBlog(t *testing.T) (*env, *client, string, *client) {
	e, ann, ws := blogEnv(t)
	e.user("root@example.com", true)
	root := e.login("root@example.com")
	base := "/api/v1/w/" + ws + "/blog"
	if st, _, _ := ann.do("PATCH", base, map[string]any{"title": "Ann writes", "intro": "First paragraph.\n\nSecond <b>paragraph</b>."}); st != 200 {
		t.Fatal("patch blog")
	}
	post := func(title, body, status string) {
		t.Helper()
		st, p, _ := ann.do("POST", base+"/posts", map[string]any{"title": title, "body_md": body, "summary": "About " + title})
		if st != 201 {
			t.Fatalf("create %d %v", st, p)
		}
		if status != "draft" {
			if st, p, _ := ann.do("PUT", fmt.Sprintf("%s/posts/%v/status", base, p["id"]), map[string]any{"_version": p["_version"], "status": status}); st != 200 {
				t.Fatalf("status %d %v", st, p)
			}
		}
	}
	post("Hello World", "Some **bold** text.\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1)) ![i](attachment:01J0000000000000000000000A)", "published")
	post("Quiet One", "Unlisted body.", "unlisted")
	post("Secret Draft", "Draft body.", "draft")
	if st, _, _ := ann.do("PUT", base+"/published", map[string]any{"published": true}); st != 200 {
		t.Fatal("blog on")
	}
	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/public-blogs", map[string]any{"enabled": true}); st != 200 {
		t.Fatal("switch on")
	}
	return e, ann, ws, root
}

func TestPublicBlogPages(t *testing.T) {
	e, _, _, _ := publicBlog(t)

	st, home, h := e.visit("/b/ann", nil)
	if st != 200 || !strings.Contains(home, "<h1>Ann writes</h1>") || !strings.Contains(home, `href="/b/ann/hello-world"`) ||
		!strings.Contains(home, "Second &lt;b&gt;paragraph&lt;/b&gt;.") || strings.Contains(home, "Quiet One") || strings.Contains(home, "Secret Draft") {
		t.Fatalf("home %d %s", st, home)
	}
	if h.Get("Content-Security-Policy") != blogs.CSP || h.Get("X-Frame-Options") != "DENY" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Referrer-Policy") != "strict-origin-when-cross-origin" || h.Get("Cache-Control") != blogCacheControl || h.Get("ETag") == "" || h.Get("Set-Cookie") != "" {
		t.Fatalf("home headers %v", h)
	}
	if !strings.Contains(home, `<link rel="canonical" href="`+testOrigin+`/b/ann">`) || !strings.Contains(home, `<meta name="robots" content="index,follow">`) {
		t.Fatalf("home meta %s", home)
	}
	if st, _, _ := e.visit("/b/ann", map[string]string{"If-None-Match": h.Get("ETag")}); st != 304 {
		t.Fatalf("conditional %d", st)
	}

	// A post: sanitised, with its images on the public file route.
	st, page, h := e.visit("/b/ann/hello-world", nil)
	if st != 200 || !strings.Contains(page, "<strong>bold</strong>") || strings.Contains(page, "<script") || strings.Contains(page, "javascript:") ||
		!strings.Contains(page, `src="/b/ann/_files/01J0000000000000000000000A"`) || !strings.Contains(page, `<meta property="og:type" content="article">`) ||
		!strings.Contains(page, `content="About Hello World"`) || h.Get("Content-Security-Policy") != blogs.CSP {
		t.Fatalf("post %d %s", st, page)
	}
	// Unlisted: served, but not indexed.
	st, page, h = e.visit("/b/ann/quiet-one", nil)
	if st != 200 || !strings.Contains(page, `content="noindex"`) || h.Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("unlisted %d %v", st, h)
	}
	// The stylesheet is the page's only subresource.
	st, css, h := e.visit(blogs.StylesheetPath, nil)
	if st != 200 || css != blogs.Stylesheet || !strings.HasPrefix(h.Get("Content-Type"), "text/css") {
		t.Fatalf("css %d %v", st, h)
	}
	// A trailing slash and another letter case go to the canonical address.
	if st, _, h := e.visit("/b/ann/", nil); st != 301 || h.Get("Location") != "/b/ann" {
		t.Fatalf("slash %d %v", st, h)
	}
	if st, _, h := e.visit("/b/ANN/hello-world", nil); st != 301 || h.Get("Location") != "/b/ann/hello-world" {
		t.Fatalf("case %d %v", st, h)
	}
}

// TestPublicBlogNotFoundIsUniform: every hidden case gives the same answer,
// and a session cookie or an API key changes nothing.
func TestPublicBlogNotFoundIsUniform(t *testing.T) {
	e, ann, ws, root := publicBlog(t)
	base := "/api/v1/w/" + ws + "/blog"
	check := func(name, path string, extra map[string]string) {
		t.Helper()
		st, body, h := e.visit(path, extra)
		if st != 404 || !bytes.Equal([]byte(body), blogs.NotFoundPage) || h.Get("Cache-Control") != "no-store" || h.Get("Content-Security-Policy") != blogs.CSP || h.Get("Set-Cookie") != "" {
			t.Errorf("%s: %d %v %q", name, st, h, body)
		}
	}
	check("unknown user", "/b/nobody", nil)
	check("malformed user", "/b/a%20b", nil)
	check("unknown post", "/b/ann/nothing-here", nil)
	check("draft", "/b/ann/secret-draft", nil)
	check("bad slug", "/b/ann/Not_A_Slug", nil)

	// The owner's own session does not reveal a draft, nor does an API key.
	var cookie string
	for _, c := range ann.http.Jar.Cookies(mustURL(t, e.srv.URL)) {
		cookie += c.Name + "=" + c.Value + "; "
	}
	if cookie == "" {
		t.Fatal("no session cookie")
	}
	check("draft with the owner's cookie", "/b/ann/secret-draft", map[string]string{"Cookie": cookie})
	st, k, _ := ann.do("POST", "/api/v1/w/"+ws+"/api-keys", map[string]any{"name": "bot", "role": "builder"})
	if st != 201 {
		t.Fatalf("key %d", st)
	}
	check("draft with an API key", "/b/ann/secret-draft", map[string]string{"Authorization": "Bearer " + k["token"].(string)})

	// The blog off, then the install switch off: everything is the same NOT_FOUND.
	if st, _, _ := ann.do("PUT", base+"/published", map[string]any{"published": false}); st != 200 {
		t.Fatal("blog off")
	}
	check("blog off", "/b/ann", nil)
	check("blog off, post", "/b/ann/hello-world", map[string]string{"Cookie": cookie})
	if st, _, _ := ann.do("PUT", base+"/published", map[string]any{"published": true}); st != 200 {
		t.Fatal("blog on")
	}
	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/public-blogs", map[string]any{"enabled": false}); st != 200 {
		t.Fatal("switch off")
	}
	check("switch off", "/b/ann", nil)
}

func TestPublicBlogRedirects(t *testing.T) {
	e, ann, ws, _ := publicBlog(t)
	base := "/api/v1/w/" + ws + "/blog"
	_, list, _ := ann.do("GET", base+"/posts?status=published", nil)
	p := list["items"].([]any)[0].(map[string]any)
	_, full, _ := ann.do("GET", fmt.Sprintf("%s/posts/%v", base, p["id"]), nil)
	if st, _, _ := ann.do("PATCH", fmt.Sprintf("%s/posts/%v", base, p["id"]), map[string]any{"_version": full["_version"], "slug": "greetings"}); st != 200 {
		t.Fatal("rename")
	}
	if st, _, h := e.visit("/b/ann/hello-world", nil); st != 301 || h.Get("Location") != "/b/ann/greetings" {
		t.Fatalf("old slug %d %v", st, h)
	}
	// An old username moves the whole address while it is held.
	if st, body, _ := ann.do("PUT", "/api/v1/auth/username", map[string]any{"username": "anna"}); st != 200 {
		t.Fatalf("rename user %d %v", st, body)
	}
	if st, _, h := e.visit("/b/ann/greetings", nil); st != 301 || h.Get("Location") != "/b/anna/greetings" {
		t.Fatalf("old username %d %v", st, h)
	}
	if st, _, h := e.visit("/b/ann", nil); st != 301 || h.Get("Location") != "/b/anna" {
		t.Fatalf("old username home %d %v", st, h)
	}
	if st, _, _ := e.visit("/b/anna/greetings", nil); st != 200 {
		t.Fatalf("new address %d", st)
	}
}

// TestPublicBlogImages: a served post's images are public, and kept by the
// file sweep; other files of the blog workspace stay private.
func TestPublicBlogImages(t *testing.T) {
	e, ann, ws, _ := publicBlog(t)
	base := "/api/v1/w/" + ws
	upload := func(name string, data []byte) string {
		t.Helper()
		r := e.send(ann, "", "POST", base+"/files", data, "X-File-Name", name, "Content-Type", "application/octet-stream")
		if r.status != 201 {
			t.Fatalf("upload %d %s", r.status, r.body)
		}
		return r.json()["id"].(string)
	}
	shown, draftOnly, unused := upload("a.png", testPNG(8, 8)), upload("b.png", testPNG(9, 9)), upload("c.png", testPNG(7, 7))
	notImage := upload("notes.txt", []byte("plain text"))
	st, p, _ := ann.do("POST", base+"/blog/posts", map[string]any{"title": "Pictures",
		"body_md": "![a](attachment:" + shown + ") ![t](attachment:" + notImage + ") ![gone](attachment:01J0000000000000000000000Z)"})
	if st != 201 {
		t.Fatalf("create %d %v", st, p)
	}
	if st, _, _ := ann.do("PUT", fmt.Sprintf("%s/blog/posts/%v/status", base, p["id"]), map[string]any{"_version": p["_version"], "status": "published"}); st != 200 {
		t.Fatal("publish")
	}
	if st, _, _ := ann.do("POST", base+"/blog/posts", map[string]any{"title": "Draft pictures", "body_md": "![b](attachment:" + draftOnly + ")"}); st != 201 {
		t.Fatal("draft")
	}

	st, body, h := e.visit("/b/ann/_files/"+shown, nil)
	if st != 200 || h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Content-Security-Policy") != fileCSP || len(body) == 0 || h.Get("Set-Cookie") != "" {
		t.Fatalf("image %d %v", st, h)
	}
	for name, id := range map[string]string{"draft's image": draftOnly, "unused file": unused, "not an image": notImage, "malformed id": "x"} {
		if st, body, _ := e.visit("/b/ann/_files/"+id, nil); st != 404 || body != string(blogs.NotFoundPage) {
			t.Errorf("%s: %d", name, st)
		}
	}

	// The post's images column holds the files it shows (existing ones only), so they are attached.
	_, meta, _ := ann.do("GET", base+"/files/"+shown, nil)
	if meta["state"] != "attached" {
		t.Fatalf("file state %v", meta)
	}
	if _, meta, _ := ann.do("GET", base+"/files/"+unused, nil); meta["state"] != "pending" {
		t.Fatalf("unused file %v", meta)
	}
}

func TestPublicBlogJSON(t *testing.T) {
	e, _, _, _ := publicBlog(t)
	anon := e.client()
	st, home, h := anon.do("GET", "/api/v1/blogs/ANN", nil)
	posts, _ := home["posts"].([]any)
	if st != 200 || home["username"] != "ann" || home["title"] != "Ann writes" || len(posts) != 1 || h.Get("Cache-Control") != blogCacheControl {
		t.Fatalf("home %d %v %v", st, home, h)
	}
	st, p, _ := anon.do("GET", "/api/v1/blogs/ann/posts/hello-world", nil)
	if st != 200 || !strings.Contains(p["html"].(string), "<strong>bold</strong>") || strings.Contains(p["html"].(string), "<script") || p["url"] != testOrigin+"/b/ann/hello-world" {
		t.Fatalf("post %d %v", st, p)
	}
	for _, path := range []string{"/api/v1/blogs/ann/posts/secret-draft", "/api/v1/blogs/nobody", "/api/v1/blogs/ann/posts/Bad_Slug"} {
		if st, body, h := anon.do("GET", path, nil); st != 404 || errCode(body) != "NOT_FOUND" || h.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %v", path, st, body)
		}
	}
}

// TestBlogAccessReadsNoCredentials: every operation marked access "blog"
// answers the same with a broken cookie and a broken bearer token as without,
// so it reads neither (ADR 0017).
func TestBlogAccessReadsNoCredentials(t *testing.T) {
	e, _, _, _ := publicBlog(t)
	n := 0
	for path, item := range e.s.API().OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil || metaString(op, metaAccess) != "blog" {
				continue
			}
			n++
			url := strings.NewReplacer("{username}", "ann", "{slug}", "hello-world").Replace(path)
			plain := e.send(nil, "", op.Method, url, nil)
			junk := e.send(nil, "not-a-key", op.Method, url, nil, "Cookie", "abidata_session=forged; __Host-abidata_session=forged")
			if plain.status != junk.status || plain.status == 401 || plain.status == 403 || !bytes.Equal(plain.body, junk.body) {
				t.Errorf("%s %s: %d without credentials, %d with junk ones", op.Method, path, plain.status, junk.status)
			}
		}
	}
	if n < 2 {
		t.Fatalf("only %d blog operations", n)
	}
}

func TestPublicBlogRateLimit(t *testing.T) {
	e, _, _, _ := publicBlog(t)
	saved := blogReads
	blogReads = apps.Budget{PerMinute: 1, Burst: 2}
	t.Cleanup(func() { blogReads = saved })
	codes := []int{}
	var h http.Header
	for range 3 {
		var st int
		st, _, h = e.visit("/b/ann", nil)
		codes = append(codes, st)
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 || h.Get("Retry-After") == "" {
		t.Fatalf("codes %v %v", codes, h)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

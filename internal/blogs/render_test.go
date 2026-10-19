package blogs

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"abidata/internal/admin"
)

// forbidden are elements that must never appear on a public page.
var forbidden = map[atom.Atom]bool{
	atom.Script: true, atom.Iframe: true, atom.Object: true, atom.Embed: true, atom.Form: true, atom.Svg: true,
	atom.Math: true, atom.Style: true, atom.Base: true, atom.Frame: true, atom.Frameset: true, atom.Input: true, atom.Button: true,
}

// checkPage parses a whole page and fails on anything that could run code or
// leave the blog: script-like elements, event or style attributes, refresh
// tags and URLs other than the blog's own pages, files and plain links.
func checkPage(t *testing.T, in string, page []byte) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		t.Fatalf("%q: page does not parse: %v", in, err)
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if forbidden[n.DataAtom] || n.Namespace != "" {
				t.Errorf("element <%s> on the page for %q", n.Data, in)
			}
			for _, a := range n.Attr {
				k := strings.ToLower(a.Key)
				if strings.HasPrefix(k, "on") || k == "style" || k == "http-equiv" || k == "srcdoc" || k == "formaction" {
					t.Errorf("attribute %s on <%s> for %q", a.Key, n.Data, in)
				}
				if k == "href" || k == "src" {
					u := strings.ToLower(strings.TrimSpace(a.Val))
					ok := false
					for _, p := range []string{"/b/", "https://", "http://", "mailto:"} {
						ok = ok || strings.HasPrefix(u, p)
					}
					if !ok || strings.HasPrefix(u, "//") {
						t.Errorf("URL %q on <%s> for %q", a.Val, n.Data, in)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

// TestXSSCorpusPages puts every corpus entry into every user-written field of
// both pages: the blog title and intro, and a post's title, summary and body.
func TestXSSCorpusPages(t *testing.T) {
	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	for _, in := range xssCorpus {
		b := admin.Blog{ID: "B1", Username: "ann", Title: in, Intro: in + "\n\n" + in, UpdatedAt: at}
		p := Post{ID: 1, Version: 1, Title: in, Slug: "post", Summary: in, Body: in, Status: StatusPublished, PublishedAt: &at}
		home, err := RenderHome("https://blog.example", b, []Post{p})
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		checkPage(t, in, home)
		page, err := RenderPost("https://blog.example", b, p)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		checkPage(t, in, page)
	}
	checkPage(t, "not found", NotFoundPage)
}

func TestRenderPages(t *testing.T) {
	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	b := admin.Blog{ID: "B1", Username: "ann", Intro: "One.\r\n\r\nTwo.", UpdatedAt: at}
	p := Post{ID: 7, Version: 3, Title: "Hi", Slug: "hi", Summary: "S", Body: "# Top\n\ntext", Status: StatusUnlisted, PublishedAt: &at}
	home, err := RenderHome("https://blog.example/", b, []Post{p})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>@ann</title>", `<p class="intro">One.</p>`, `<p class="intro">Two.</p>`, `href="https://blog.example/b/ann"`,
		`<time datetime="2026-05-01T09:00:00Z">1 May 2026</time>`, `href="` + StylesheetPath + `"`} {
		if !strings.Contains(string(home), want) {
			t.Errorf("home lacks %q:\n%s", want, home)
		}
	}
	page, err := RenderPost("https://blog.example", b, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>Hi · @ann</title>", `content="noindex"`, "<h2>Top</h2>", `href="https://blog.example/b/ann/hi"`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("post lacks %q:\n%s", want, page)
		}
	}
	// ETags follow content: a new version or a blog edit changes them.
	if PostETag(b, p) == PostETag(b, Post{ID: 7, Version: 4}) {
		t.Error("post ETag ignores the version")
	}
	b2 := b
	b2.UpdatedAt = at.Add(time.Second)
	if HomeETag(b, []Post{p}) == HomeETag(b2, []Post{p}) || PostETag(b, p) == PostETag(b2, p) {
		t.Error("ETag ignores the blog's settings")
	}
	if got := clip("one two three four", 9); got != "one two…" {
		t.Errorf("clip %q", got)
	}

	c := NewPageCache(2)
	c.Put("B1", "a", []byte("1"))
	c.Put("B2", "b", []byte("2"))
	c.Purge("B1")
	if _, ok := c.Get("a"); ok {
		t.Error("purged page served")
	}
	if v, ok := c.Get("b"); !ok || string(v) != "2" {
		t.Error("other blog purged")
	}
}

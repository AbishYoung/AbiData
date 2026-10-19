package blogs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"abidata/internal/admin"
)

// The public pages (ADR 0017): server-rendered HTML from html/template, one
// external stylesheet and no script. The CSP the server sends allows nothing
// else, so even a sanitiser bypass cannot run code.

// CSP is the Content-Security-Policy of every public blog response.
const CSP = "default-src 'none'; style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// StylesheetPath is where the public stylesheet is served; the query string
// changes with its content, so it can be cached for a long time.
var StylesheetPath = func() string {
	sum := sha256.Sum256([]byte(Stylesheet))
	return "/b/_assets/blog.css?v=" + hex.EncodeToString(sum[:6])
}()

// pageVersion changes when the templates do, so old ETags stop matching.
const pageVersion = "1"

// PublicImageURL is where a served post's images are: the public file route,
// which applies the same visibility rules as the post.
func PublicImageURL(username string) ImageURL {
	return func(id string) string { return "/b/" + username + "/_files/" + id }
}

// Paragraphs splits plain text on blank lines (the blog intro).
func Paragraphs(s string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// clip shortens s to at most n runes on a word boundary, with an ellipsis.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)[:n]
	if i := strings.LastIndexByte(string(r), ' '); i > n/2 {
		return string(r)[:i] + "…"
	}
	return string(r) + "…"
}

type pageMeta struct {
	Title       string
	Description string
	Canonical   string
	Robots      string
	OGType      string
	SiteName    string
	Stylesheet  string
}

type homeData struct {
	Meta  pageMeta
	Blog  admin.Blog
	Name  string
	Intro []string
	Posts []postItem
}

type postItem struct {
	Title   string
	URL     string
	Date    string
	ISODate string
	Summary string
}

type postData struct {
	Meta     pageMeta
	Name     string
	HomePath string
	Title    string
	Date     string
	ISODate  string
	Body     template.HTML
}

const head = `{{define "head"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
{{with .Description}}<meta name="description" content="{{.}}">{{end}}
<meta name="robots" content="{{.Robots}}">
<link rel="canonical" href="{{.Canonical}}">
<meta property="og:type" content="{{.OGType}}">
<meta property="og:title" content="{{.Title}}">
{{with .Description}}<meta property="og:description" content="{{.}}">{{end}}
<meta property="og:url" content="{{.Canonical}}">
<meta property="og:site_name" content="{{.SiteName}}">
<meta name="twitter:card" content="summary">
<link rel="stylesheet" href="{{.Stylesheet}}">
</head>{{end}}`

var pages = template.Must(template.New("pages").Parse(head + `
{{define "home"}}{{template "head" .Meta}}
<body>
<header class="site-header"><a href="/b/{{.Blog.Username}}">{{.Name}}</a></header>
<main>
<h1>{{.Name}}</h1>
{{range .Intro}}<p class="intro">{{.}}</p>{{end}}
{{if .Posts}}<ul class="posts">
{{range .Posts}}<li><h2><a href="{{.URL}}">{{.Title}}</a></h2>{{if .Date}}<p class="meta"><time datetime="{{.ISODate}}">{{.Date}}</time></p>{{end}}{{with .Summary}}<p>{{.}}</p>{{end}}</li>
{{end}}</ul>{{else}}<p class="meta">No posts yet.</p>{{end}}
</main>
<footer class="site-footer">@{{.Blog.Username}}</footer>
</body></html>{{end}}
{{define "post"}}{{template "head" .Meta}}
<body>
<header class="site-header"><a href="{{.HomePath}}">{{.Name}}</a></header>
<main><article>
<h1>{{.Title}}</h1>
{{if .Date}}<p class="meta"><time datetime="{{.ISODate}}">{{.Date}}</time></p>{{end}}
{{.Body}}
</article></main>
<footer class="site-footer"><a href="{{.HomePath}}">More from {{.Name}}</a></footer>
</body></html>{{end}}
{{define "notfound"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Not found</title><meta name="robots" content="noindex"><link rel="stylesheet" href="{{.}}"></head>
<body><main><h1>Not found</h1><p>There is no page at this address.</p></main></body></html>{{end}}`))

func blogName(b admin.Blog) string {
	if strings.TrimSpace(b.Title) != "" {
		return b.Title
	}
	return "@" + b.Username
}

func dates(t *time.Time) (human, iso string) {
	if t == nil {
		return "", ""
	}
	u := t.UTC()
	return u.Format("2 January 2006"), u.Format(time.RFC3339)
}

// RenderHome renders a blog's home page. base is ABIDATA_PUBLIC_URL.
func RenderHome(base string, b admin.Blog, posts []Post) ([]byte, error) {
	home := strings.TrimRight(base, "/") + "/b/" + b.Username
	intro := Paragraphs(b.Intro)
	desc := ""
	if len(intro) > 0 {
		desc = clip(intro[0], 200)
	}
	d := homeData{Blog: b, Name: blogName(b), Intro: intro,
		Meta: pageMeta{Title: blogName(b), Description: desc, Canonical: home, Robots: "index,follow", OGType: "website", SiteName: blogName(b), Stylesheet: StylesheetPath}}
	for _, p := range posts {
		human, iso := dates(p.PublishedAt)
		d.Posts = append(d.Posts, postItem{Title: p.Title, URL: "/b/" + b.Username + "/" + p.Slug, Date: human, ISODate: iso, Summary: p.Summary})
	}
	var buf bytes.Buffer
	err := pages.ExecuteTemplate(&buf, "home", d)
	return buf.Bytes(), err
}

// RenderPost renders one post. Unlisted posts are marked noindex.
func RenderPost(base string, b admin.Blog, p Post) ([]byte, error) {
	body, err := RenderMarkdown(p.Body, PublicImageURL(b.Username))
	if err != nil {
		return nil, err
	}
	robots := "index,follow"
	if p.Status == StatusUnlisted {
		robots = "noindex"
	}
	human, iso := dates(p.PublishedAt)
	d := postData{Name: blogName(b), HomePath: "/b/" + b.Username, Title: p.Title, Date: human, ISODate: iso,
		Body: template.HTML(body), //nolint:gosec // sanitised by RenderMarkdown
		Meta: pageMeta{Title: p.Title + " · " + blogName(b), Description: clip(p.Summary, 300), Robots: robots, OGType: "article", SiteName: blogName(b),
			Canonical: strings.TrimRight(base, "/") + "/b/" + b.Username + "/" + p.Slug, Stylesheet: StylesheetPath}}
	var buf bytes.Buffer
	err = pages.ExecuteTemplate(&buf, "post", d)
	return buf.Bytes(), err
}

// NotFoundPage is the one body every hidden blog address answers with.
var NotFoundPage = func() []byte {
	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, "notfound", StylesheetPath); err != nil {
		panic(err)
	}
	return buf.Bytes()
}()

// HomeETag identifies a home page's content: the blog's settings and the
// listed posts' versions.
func HomeETag(b admin.Blog, posts []Post) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%d|", pageVersion, b.ID, b.Username, b.UpdatedAt.UnixNano())
	for _, p := range posts {
		_, _ = fmt.Fprintf(h, "%d:%d,", p.ID, p.Version)
	}
	return `W/"h-` + hex.EncodeToString(h.Sum(nil)[:12]) + `"`
}

// PostETag identifies a post page: the post's id and _version, and the blog's settings.
func PostETag(b admin.Blog, p Post) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%s|%d|%d:%d", pageVersion, b.ID, b.Username, b.UpdatedAt.UnixNano(), p.ID, p.Version))
	return `W/"p-` + hex.EncodeToString(h[:12]) + `"`
}

// PageCache keeps rendered pages by ETag, bounded. Purge drops a blog's pages
// (a take-down), although a changed ETag already misses on its own.
type PageCache struct {
	mu    sync.Mutex
	max   int
	pages map[string]cachedPage
}

type cachedPage struct {
	blogID string
	html   []byte
}

// NewPageCache holds at most max pages.
func NewPageCache(max int) *PageCache {
	return &PageCache{max: max, pages: map[string]cachedPage{}}
}

// Get returns the cached page for etag.
func (c *PageCache) Get(etag string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pages[etag]
	return p.html, ok
}

// Put stores a page; when full, it starts over (pages re-render cheaply).
func (c *PageCache) Put(blogID, etag string, html []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pages) >= c.max {
		c.pages = map[string]cachedPage{}
	}
	c.pages[etag] = cachedPage{blogID: blogID, html: html}
}

// Purge drops every page of one blog.
func (c *PageCache) Purge(blogID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, p := range c.pages {
		if p.blogID == blogID {
			delete(c.pages, k)
		}
	}
}

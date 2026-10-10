// Package docsrv renders the documentation (docs/content/**.md plus generated
// reference pages) once at startup into an in-memory site: per page its HTML,
// headings and internal links, and a search index for the in-app docs panel
// (brief <docs_system>).
package docsrv

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
)

// Heading is one heading of a page.
type Heading struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

// Page is a rendered page.
type Page struct {
	Slug     string    `json:"slug"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary,omitempty"`
	Order    int       `json:"order"`
	Section  string    `json:"section"`
	HTML     string    `json:"html"`
	Headings []Heading `json:"headings"`
	// Links are the page's internal links (/docs/<slug>[#anchor]), for the integrity check.
	Links []string `json:"-"`
	// Text is the plain text, for search.
	Text string `json:"-"`
}

// Source is a markdown page supplied in code (generated references).
type Source struct {
	Slug     string
	Markdown string
}

// Site is the rendered documentation.
type Site struct {
	Pages map[string]*Page
	order []string
}

// LinkPrefix is how pages link to each other: /docs/<slug>#anchor.
const LinkPrefix = "/docs/"

func newMarkdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM, meta.Meta,
			highlighting.NewHighlighting(highlighting.WithFormatOptions(chromahtml.WithClasses(true)))),
		// Raw HTML stays disabled (no html.WithUnsafe).
		goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithAttribute()),
		goldmark.WithRendererOptions(html.WithXHTML()),
	)
}

// Build renders every .md file under fsys (slug = path without .md; a
// directory's index.md is the directory's slug) plus the generated sources.
func Build(fsys fs.FS, generated []Source) (*Site, error) {
	md := newMarkdown()
	s := &Site{Pages: map[string]*Page{}}
	add := func(slug string, src []byte) error {
		if _, dup := s.Pages[slug]; dup {
			return fmt.Errorf("docs: duplicate page %q", slug)
		}
		p, err := render(md, slug, src)
		if err != nil {
			return fmt.Errorf("docs: %s: %w", slug, err)
		}
		s.Pages[slug] = p
		return nil
	}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		slug := strings.TrimSuffix(p, ".md")
		if path.Base(slug) == "index" {
			slug = path.Dir(slug)
		}
		return add(slug, b)
	})
	if err != nil {
		return nil, err
	}
	for _, g := range generated {
		if err := add(g.Slug, []byte(g.Markdown)); err != nil {
			return nil, err
		}
	}
	for slug := range s.Pages {
		s.order = append(s.order, slug)
	}
	sort.Slice(s.order, func(i, j int) bool {
		a, b := s.Pages[s.order[i]], s.Pages[s.order[j]]
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Slug < b.Slug
	})
	return s, nil
}

func render(md goldmark.Markdown, slug string, src []byte) (*Page, error) {
	ctx := parser.NewContext()
	doc := md.Parser().Parse(text.NewReader(src), parser.WithContext(ctx))
	p := &Page{Slug: slug, Section: strings.SplitN(slug, "/", 2)[0], Headings: []Heading{}}
	fm := meta.Get(ctx)
	if t, ok := fm["title"].(string); ok {
		p.Title = t
	}
	if t, ok := fm["summary"].(string); ok {
		p.Summary = t
	}
	if o, ok := fm["order"].(int); ok {
		p.Order = o
	}
	var plain strings.Builder
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch x := n.(type) {
		case *ast.Heading:
			h := Heading{Level: x.Level, Text: nodeText(x, src)}
			if id, ok := x.AttributeString("id"); ok {
				if b, ok := id.([]byte); ok {
					h.ID = string(b)
				}
			}
			p.Headings = append(p.Headings, h)
		case *ast.Link:
			if dst := string(x.Destination); strings.HasPrefix(dst, LinkPrefix) {
				p.Links = append(p.Links, dst)
			}
		case *ast.Text:
			plain.Write(x.Segment.Value(src))
			plain.WriteByte(' ')
		case *ast.CodeSpan, *ast.FencedCodeBlock, *ast.CodeBlock:
			plain.WriteString(nodeText(n, src))
			plain.WriteByte(' ')
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return nil, err
	}
	p.HTML = buf.String()
	p.Text = strings.Join(strings.Fields(plain.String()), " ")
	return p, nil
}

func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	if n.Type() == ast.TypeBlock && n.Lines().Len() > 0 && n.Kind() != ast.KindHeading {
		for i := 0; i < n.Lines().Len(); i++ {
			seg := n.Lines().At(i)
			b.Write(seg.Value(src))
		}
		return b.String()
	}
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := c.(*ast.Text); ok {
				b.Write(t.Segment.Value(src))
			} else if s, ok := c.(*ast.String); ok {
				b.Write(s.Value)
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// Summary is a page listing entry.
type Summary struct {
	Slug     string    `json:"slug"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary,omitempty"`
	Section  string    `json:"section"`
	Headings []Heading `json:"headings"`
	Text     string    `json:"text" doc:"Plain text for client-side search (minisearch)."`
}

// Index lists every page in reading order, with search text.
func (s *Site) Index() []Summary {
	out := make([]Summary, 0, len(s.order))
	for _, slug := range s.order {
		p := s.Pages[slug]
		out = append(out, Summary{Slug: p.Slug, Title: p.Title, Summary: p.Summary, Section: p.Section, Headings: p.Headings, Text: p.Text})
	}
	return out
}

// Problems reports integrity issues: missing titles, and internal links whose
// page or anchor does not exist.
func (s *Site) Problems() []string {
	var out []string
	for _, slug := range s.order {
		p := s.Pages[slug]
		if strings.TrimSpace(p.Title) == "" {
			out = append(out, slug+": no title")
		}
		for _, l := range p.Links {
			target, anchor, _ := strings.Cut(strings.TrimPrefix(l, LinkPrefix), "#")
			if u, err := url.PathUnescape(target); err == nil {
				target = u
			}
			tp, ok := s.Pages[strings.TrimSuffix(target, "/")]
			if !ok {
				out = append(out, fmt.Sprintf("%s: link %s: no such page", slug, l))
				continue
			}
			if anchor != "" && !tp.HasAnchor(anchor) {
				out = append(out, fmt.Sprintf("%s: link %s: no such anchor", slug, l))
			}
		}
	}
	return out
}

// HasAnchor reports whether the page has a heading with id.
func (p *Page) HasAnchor(id string) bool {
	for _, h := range p.Headings {
		if h.ID == id {
			return true
		}
	}
	return false
}

// HighlightCSS returns the syntax-highlighting stylesheet for code blocks
// (chroma classes), with a dark variant under prefers-color-scheme.
func HighlightCSS() (string, error) {
	f := chromahtml.New(chromahtml.WithClasses(true))
	var light, dark bytes.Buffer
	if err := f.WriteCSS(&light, styles.Get("github")); err != nil {
		return "", err
	}
	if err := f.WriteCSS(&dark, styles.Get("monokai")); err != nil {
		return "", err
	}
	// Follow the app's theme: the system preference unless the user forced one
	// with [data-theme] on <html> (same rules as web/src/styles/tokens.css).
	return scope(light.String(), `:root:not([data-theme="dark"])`) +
		"\n@media (prefers-color-scheme: dark) {\n" + scope(dark.String(), `:root:not([data-theme="light"])`) + "}\n" +
		scope(dark.String(), `:root[data-theme="dark"]`), nil
}

// scope prefixes every selector of chroma's flat stylesheet with prefix.
func scope(css, prefix string) string {
	var b strings.Builder
	for _, line := range strings.Split(css, "\n") {
		sel, rest, ok := strings.Cut(line, "{")
		if !ok || strings.TrimSpace(sel) == "" {
			b.WriteString(line + "\n")
			continue
		}
		// Keep chroma's leading comment ("/* Background */ .bg").
		comment := ""
		if i := strings.Index(sel, "*/"); i >= 0 {
			comment, sel = sel[:i+2]+" ", sel[i+2:]
		}
		parts := strings.Split(sel, ",")
		for i, p := range parts {
			parts[i] = prefix + " " + strings.TrimSpace(p)
		}
		b.WriteString(comment + strings.Join(parts, ", ") + " {" + rest + "\n")
	}
	return b.String()
}

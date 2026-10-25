package main

import (
	"bytes"
	"fmt"
	"html/template"
	"image"
	_ "image/jpeg" // image.DecodeConfig for img width/height
	_ "image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"abidata/internal/docsrv"
	_ "abidata/internal/script/modules/all" // registers the Lua modules the generated API reference lists
)

// Config says where to read and write.
type Config struct {
	DocsDir   string
	ImagesDir string
	SiteDir   string
	OutDir    string
	Base      string // "/AbiData/": always starts and ends with a slash
}

const repoURL = "https://github.com/AbishYoung/AbiData"

var sectionTitles = map[string]string{"guide": "User guide", "operations": "Operations", "reference": "Reference"}

type navItem struct {
	Title, URL string
	Current    bool
}

type navSection struct {
	Title string
	Items []navItem
}

type tocItem struct {
	Text, ID string
	Level    int
}

type pageData struct {
	Base, Title, Summary, URL, Section string
	Content                            template.HTML
	Nav                                []navSection
	TOC                                []tocItem
	Prev, Next                         *navItem
	EditURL                            string
	RepoURL                            string
	Root                               bool // the docs landing page
}

var (
	reDocHref = regexp.MustCompile(`href="/docs/([^"#]*)(#[^"]*)?"`)
	reImgSrc  = regexp.MustCompile(`src="(?:\.\./)+images/([^"]+)"`)
	reImgTag  = regexp.MustCompile(`<img [^>]*>`)
	reSrcAttr = regexp.MustCompile(`src="[^"]*assets/img/([^"/]+)"`)
	reSrcset  = regexp.MustCompile(`srcset="[^"]*assets/img/([^"/\s]+)"`)
)

// Generate writes the whole site to cfg.OutDir.
func Generate(cfg Config) error {
	if !strings.HasPrefix(cfg.Base, "/") || !strings.HasSuffix(cfg.Base, "/") {
		return fmt.Errorf("base %q must start and end with /", cfg.Base)
	}
	site, err := docsrv.Build(os.DirFS(cfg.DocsDir), docsrv.Generated())
	if err != nil {
		return err
	}
	if p := site.Problems(); len(p) > 0 {
		return fmt.Errorf("documentation problems:\n  %s", strings.Join(p, "\n  "))
	}
	if err := os.RemoveAll(cfg.OutDir); err != nil {
		return err
	}
	g := &gen{cfg: cfg, site: site, images: map[string]bool{}}
	return g.run()
}

type gen struct {
	cfg    Config
	site   *docsrv.Site
	images map[string]bool
}

func (g *gen) write(rel string, b []byte) error {
	p := filepath.Join(g.cfg.OutDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

func (g *gen) docURL(slug string) string { return g.cfg.Base + "docs/" + slug + "/" }

func (g *gen) run() error {
	idx := g.site.Index()
	tmplSrc, err := os.ReadFile(filepath.Join(g.cfg.SiteDir, "docs.tmpl.html"))
	if err != nil {
		return err
	}
	tmpl, err := template.New("docs").Parse(string(tmplSrc))
	if err != nil {
		return err
	}

	// Sidebar groups in reading order.
	var sections []navSection
	for _, s := range idx {
		if len(sections) == 0 || sections[len(sections)-1].Title != g.sectionTitle(s.Section) {
			sections = append(sections, navSection{Title: g.sectionTitle(s.Section)})
		}
		last := &sections[len(sections)-1]
		last.Items = append(last.Items, navItem{Title: s.Title, URL: g.docURL(s.Slug)})
	}
	navFor := func(cur string) []navSection {
		out := make([]navSection, len(sections))
		for i, s := range sections {
			out[i] = navSection{Title: s.Title, Items: append([]navItem(nil), s.Items...)}
			for j := range out[i].Items {
				out[i].Items[j].Current = out[i].Items[j].URL == cur
			}
		}
		return out
	}

	render := func(rel string, d pageData) error {
		d.Base, d.RepoURL, d.Nav = g.cfg.Base, repoURL, navFor(d.URL)
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, d); err != nil {
			return err
		}
		return g.write(rel, g.addImageSizes(buf.Bytes()))
	}

	for i, s := range idx {
		p := g.site.Pages[s.Slug]
		d := pageData{Title: p.Title, Summary: p.Summary, URL: g.docURL(p.Slug), Section: g.sectionTitle(p.Section),
			Content: template.HTML(g.rewrite(p.HTML)), EditURL: g.editURL(p.Slug)} //nolint:gosec // rendered by goldmark with raw HTML disabled
		for _, h := range p.Headings {
			if h.Level >= 2 && h.Level <= 3 && h.ID != "" {
				d.TOC = append(d.TOC, tocItem{Text: h.Text, ID: h.ID, Level: h.Level})
			}
		}
		if i > 0 {
			d.Prev = &navItem{Title: idx[i-1].Title, URL: g.docURL(idx[i-1].Slug)}
		}
		if i < len(idx)-1 {
			d.Next = &navItem{Title: idx[i+1].Title, URL: g.docURL(idx[i+1].Slug)}
		}
		if err := render("docs/"+p.Slug+"/index.html", d); err != nil {
			return fmt.Errorf("%s: %w", p.Slug, err)
		}
	}

	// Docs landing page: a card list per section.
	var landing strings.Builder
	for _, sec := range sections {
		fmt.Fprintf(&landing, "<h2 id=%q>%s</h2>\n<ul class=\"doc-cards\">\n", strings.ToLower(strings.ReplaceAll(sec.Title, " ", "-")), template.HTMLEscapeString(sec.Title))
		for _, it := range sec.Items {
			sum := ""
			for _, s := range idx {
				if g.docURL(s.Slug) == it.URL {
					sum = s.Summary
				}
			}
			fmt.Fprintf(&landing, "<li><a href=%q><strong>%s</strong><span>%s</span></a></li>\n", it.URL, template.HTMLEscapeString(it.Title), template.HTMLEscapeString(sum))
		}
		landing.WriteString("</ul>\n")
	}
	if err := render("docs/index.html", pageData{Title: "Documentation", URL: g.cfg.Base + "docs/", Root: true,
		Summary: "Everything about building with and running AbiData.", Content: template.HTML(landing.String())}); err != nil { //nolint:gosec // built above from escaped text
		return err
	}

	// Home page.
	home, err := os.ReadFile(filepath.Join(g.cfg.SiteDir, "index.html"))
	if err != nil {
		return err
	}
	home = []byte(strings.ReplaceAll(string(home), "{{BASE}}", g.cfg.Base))
	for _, re := range []*regexp.Regexp{reSrcAttr, reSrcset} {
		for _, m := range re.FindAllStringSubmatch(string(home), -1) {
			g.images[m[1]] = true
		}
	}
	if err := g.write("index.html", g.addImageSizes(home)); err != nil {
		return err
	}
	if err := g.write("404.html", g.notFound()); err != nil {
		return err
	}
	if err := g.write(".nojekyll", nil); err != nil {
		return err
	}

	// Static assets: site/assets/** and the highlight stylesheet.
	if err := g.copyTree(filepath.Join(g.cfg.SiteDir, "assets"), "assets"); err != nil {
		return err
	}
	css, err := docsrv.HighlightCSS()
	if err != nil {
		return err
	}
	if err := g.write("assets/highlight.css", []byte(css)); err != nil {
		return err
	}
	names := make([]string, 0, len(g.images))
	for n := range g.images {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(g.cfg.ImagesDir, n))
		if err != nil {
			return fmt.Errorf("image %s: %w", n, err)
		}
		if err := g.write("assets/img/"+n, b); err != nil {
			return err
		}
	}
	return nil
}

func (g *gen) sectionTitle(s string) string {
	if t, ok := sectionTitles[s]; ok {
		return t
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (g *gen) editURL(slug string) string {
	for _, c := range []string{slug + ".md", slug + "/index.md"} {
		if _, err := os.Stat(filepath.Join(g.cfg.DocsDir, filepath.FromSlash(c))); err == nil {
			return repoURL + "/blob/main/docs/content/" + c
		}
	}
	return ""
}

// rewrite points in-docs links and images at their static locations.
func (g *gen) rewrite(h string) string {
	h = reDocHref.ReplaceAllStringFunc(h, func(m string) string {
		sub := reDocHref.FindStringSubmatch(m)
		return `href="` + g.cfg.Base + "docs/" + strings.TrimSuffix(sub[1], "/") + "/" + sub[2] + `"`
	})
	return reImgSrc.ReplaceAllStringFunc(h, func(m string) string {
		name := reImgSrc.FindStringSubmatch(m)[1]
		g.images[name] = true
		return `src="` + g.cfg.Base + "assets/img/" + name + `" loading="lazy" decoding="async"`
	})
}

// addImageSizes gives every <img> without dimensions its intrinsic size, which
// avoids layout shift for lazy-loaded screenshots.
func (g *gen) addImageSizes(b []byte) []byte {
	return reImgTag.ReplaceAllFunc(b, func(tag []byte) []byte {
		s := string(tag)
		if strings.Contains(s, " width=") {
			return tag
		}
		m := reSrcAttr.FindStringSubmatch(s)
		if m == nil {
			return tag
		}
		f, err := os.Open(filepath.Join(g.cfg.ImagesDir, m[1]))
		if err != nil {
			return tag
		}
		defer func() { _ = f.Close() }()
		c, _, err := image.DecodeConfig(f)
		if err != nil {
			return tag
		}
		return []byte(strings.Replace(s, "<img ", fmt.Sprintf(`<img width="%d" height="%d" `, c.Width, c.Height), 1))
	})
}

func (g *gen) copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return g.write(path.Join(dst, filepath.ToSlash(rel)), b)
	})
}

func (g *gen) notFound() []byte {
	b := g.cfg.Base
	return []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>Page not found - AbiData</title><link rel="stylesheet" href="` + b + `assets/site.css"></head><body class="nf"><main id="main" class="nf-main">` +
		`<h1>Page not found</h1><p>That page does not exist.</p><p><a class="btn btn-primary" href="` + b + `">Go to the home page</a> ` +
		`<a class="btn" href="` + b + `docs/">Read the documentation</a></p></main></body></html>`)
}

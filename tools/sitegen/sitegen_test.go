package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"abidata/internal/docsrv"
	_ "abidata/internal/script/modules/all"
)

var (
	reAttr = regexp.MustCompile(`\b(?:href|src|srcset)="([^"]*)"`)
	reID   = regexp.MustCompile(`\bid="([^"]+)"`)
)

func generate(t *testing.T, base string) string {
	t.Helper()
	out := t.TempDir()
	err := Generate(Config{DocsDir: "../../docs/content", ImagesDir: "../../docs/images", SiteDir: "../../site", OutDir: filepath.Join(out, "site"), Base: base})
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(out, "site")
}

func TestEveryDocsPageRenders(t *testing.T) {
	out := generate(t, "/AbiData/")
	site, err := docsrv.Build(os.DirFS("../../docs/content"), docsrv.Generated())
	if err != nil {
		t.Fatal(err)
	}
	if len(site.Pages) < 20 {
		t.Fatalf("only %d pages built", len(site.Pages))
	}
	for slug, p := range site.Pages {
		b, err := os.ReadFile(filepath.Join(out, "docs", filepath.FromSlash(slug), "index.html"))
		if err != nil {
			t.Fatalf("page %s not written: %v", slug, err)
		}
		s := string(b)
		for _, want := range []string{"<title>" + p.Title, `aria-label="Documentation"`, `href="/AbiData/assets/site.css"`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: missing %q", slug, want)
			}
		}
	}
	for _, f := range []string{"index.html", "404.html", "docs/index.html", "assets/site.css", "assets/site.js", "assets/highlight.css", "assets/favicon.svg"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

// TestLinksResolve parses every generated page and checks that each link and asset
// stays under the base path, exists in the output, and that anchors exist.
func TestLinksResolve(t *testing.T) {
	for _, base := range []string{"/AbiData/", "/x/y/"} {
		t.Run(base, func(t *testing.T) {
			out := generate(t, base)
			pages := 0
			err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
					return err
				}
				pages++
				rel, _ := filepath.Rel(out, p)
				pageURL := base + filepath.ToSlash(rel)
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				for _, m := range reAttr.FindAllStringSubmatch(string(b), -1) {
					checkLink(t, out, base, pageURL, m[1])
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if pages < 25 {
				t.Errorf("only %d html files", pages)
			}
		})
	}
}

func checkLink(t *testing.T, out, base, pageURL, link string) {
	t.Helper()
	switch {
	case link == "", strings.HasPrefix(link, "http://"), strings.HasPrefix(link, "https://"), strings.HasPrefix(link, "mailto:"), strings.HasPrefix(link, "data:"):
		return
	}
	// srcset is "url" or "url 2x"; only the first field matters here.
	link = strings.Fields(link)[0]
	target, anchor, _ := strings.Cut(link, "#")
	if target == "" {
		target = pageURL // same-page anchor
	} else if strings.HasPrefix(target, "/") {
		if !strings.HasPrefix(target, base) {
			t.Errorf("%s: link %q escapes base %s", pageURL, link, base)
			return
		}
	} else {
		target = path.Join(path.Dir(pageURL), target)
	}
	file := filepath.Join(out, filepath.FromSlash(strings.TrimPrefix(target, base)))
	if strings.HasSuffix(target, "/") || target == strings.TrimSuffix(base, "/") {
		file = filepath.Join(file, "index.html")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Errorf("%s: broken link %q (%v)", pageURL, link, err)
		return
	}
	if anchor == "" || !strings.HasSuffix(file, ".html") {
		return
	}
	for _, m := range reID.FindAllStringSubmatch(string(b), -1) {
		if m[1] == anchor {
			return
		}
	}
	// The home page links to sections by id too; both are covered by the loop above.
	t.Errorf("%s: link %q: no anchor #%s in %s", pageURL, link, anchor, target)
}

func TestNoRootRelativeLeaks(t *testing.T) {
	out := generate(t, "/AbiData/")
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		b, _ := os.ReadFile(p)
		for _, bad := range []string{`href="/docs/`, `src="../../images/`, `{{BASE}}`} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s still contains %s", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBaseMustBeSlashed(t *testing.T) {
	if err := Generate(Config{Base: "AbiData"}); err == nil {
		t.Fatal("expected an error")
	}
}

package docsrv_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"abidata/docs"
	"abidata/internal/docsrv"
	"abidata/internal/fault"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
)

func site(t *testing.T) *docsrv.Site {
	t.Helper()
	content, err := fs.Sub(docs.Content, "content")
	if err != nil {
		t.Fatal(err)
	}
	s, err := docsrv.Build(content, docsrv.Generated())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Gate 7: every internal link and anchor resolves, every manifest function and
// error code has a section, and every page has a title.
func TestDocsIntegrity(t *testing.T) {
	s := site(t)
	for _, p := range s.Problems() {
		t.Error(p)
	}
	lua := s.Pages[docsrv.LuaAPISlug]
	for _, m := range script.Manifest() {
		for _, f := range m.Funcs {
			if !lua.HasAnchor(docsrv.Anchor(docsrv.FuncName(m, f))) {
				t.Errorf("Lua API page has no section for %s", docsrv.FuncName(m, f))
			}
		}
	}
	errs := s.Pages[docsrv.ErrorsSlug]
	for _, c := range fault.Codes() {
		if !errs.HasAnchor(docsrv.ErrorAnchor(c)) {
			t.Errorf("error reference has no section for %s", c)
		}
	}
	if len(s.Index()) < 3 {
		t.Fatalf("only %d pages", len(s.Index()))
	}
}

// Function names repeat across modules (csv.parse, datetime.parse), so section
// ids are qualified and must be unique on the generated pages.
func TestGeneratedHeadingIDsAreUnique(t *testing.T) {
	s := site(t)
	for _, slug := range []string{docsrv.LuaAPISlug, docsrv.ErrorsSlug} {
		seen := map[string]bool{}
		for _, h := range s.Pages[slug].Headings {
			if seen[h.ID] {
				t.Errorf("%s: duplicate heading id %q", slug, h.ID)
			}
			seen[h.ID] = true
		}
	}
	lua := s.Pages[docsrv.LuaAPISlug]
	for _, id := range []string{"db-query", "csv-parse", "datetime-parse", "json-encode", "csv-encode", "fetch", "module-fetch", "record-get", "records-get"} {
		if !lua.HasAnchor(id) {
			t.Errorf("Lua API page has no section %q", id)
		}
	}
}

func TestRenderingRules(t *testing.T) {
	fsys := fstest.MapFS{
		"guide/a.md": {Data: []byte("---\ntitle: A\n---\n# A\n\n## Part one\n\n<script>alert(1)</script>\n\n[ok](/docs/guide/b#target) [bad](/docs/guide/missing) [anchor](/docs/guide/b#nope)\n\n```lua\nlocal x = 1\n```\n")},
		"guide/b.md": {Data: []byte("---\ntitle: B\n---\n# B\n\n## Target {#target}\n")},
		"guide/c.md": {Data: []byte("# No front matter\n")},
	}
	s, err := docsrv.Build(fsys, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Pages["guide/a"]
	if strings.Contains(a.HTML, "<script>") {
		t.Fatal("raw HTML rendered")
	}
	if !a.HasAnchor("part-one") || !strings.Contains(a.HTML, `class="chroma"`) {
		t.Fatalf("headings/highlighting: %+v\n%s", a.Headings, a.HTML)
	}
	probs := strings.Join(s.Problems(), "\n")
	for _, want := range []string{"guide/c: no title", "guide/missing: no such page", "guide/b#nope: no such anchor"} {
		if !strings.Contains(probs, want) {
			t.Errorf("missing problem %q in:\n%s", want, probs)
		}
	}
	if strings.Contains(probs, "#target") {
		t.Errorf("valid anchor reported:\n%s", probs)
	}
}

package blogs

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const fileID = "01J0000000000000000000000A"

func testImages(id string) string { return "/b/ann/_files/" + id }

func TestRenderMarkdown(t *testing.T) {
	for in, want := range map[string]string{
		"# Title\n\nHello *world*.":             "<h2>Title</h2>\n<p>Hello <em>world</em>.</p>\n",
		"[x](https://example.com/a?b=1)":        `<p><a href="https://example.com/a?b=1" rel="nofollow noopener ugc">x</a></p>` + "\n",
		"[m](mailto:a@example.com)":             `<p><a href="mailto:a@example.com" rel="nofollow noopener ugc">m</a></p>` + "\n",
		"![cat](attachment:" + fileID + ")":     `<p><img src="/b/ann/_files/` + fileID + `" alt="cat" loading="lazy"/></p>` + "\n",
		"![remote](https://evil.example/t.gif)": "<p>remote</p>\n",
		"```go\nx := 1\n```":                    `<pre><code class="language-go">x := 1` + "\n</code></pre>\n",
		"~~gone~~":                              "<p><del>gone</del></p>\n",
		"3. three\n4. four":                     "<ol start=\"3\">\n<li>three</li>\n<li>four</li>\n</ol>\n",
		"> quoted":                              "<blockquote>\n<p>quoted</p>\n</blockquote>\n",
	} {
		got, err := RenderMarkdown(in, testImages)
		if err != nil || got != want {
			t.Errorf("%q:\n got %q\nwant %q (%v)", in, got, want, err)
		}
	}
	got, _ := RenderMarkdown("| a | b |\n|---|--:|\n| 1 | 2 |", testImages)
	if !strings.Contains(got, "<table>") || !strings.Contains(got, "<td") || strings.Contains(got, "style") {
		t.Errorf("table: %s", got)
	}
}

// xssCorpus is pushed through the whole pipeline; nothing in the output may
// run script, load a foreign resource or carry a dangerous URL.
var xssCorpus = []string{
	`<script>alert(1)</script>`,
	`<img src=x onerror=alert(1)>`,
	`<svg onload=alert(1)>`,
	`<iframe src="javascript:alert(1)"></iframe>`,
	`[click](javascript:alert(1))`,
	`[click](JaVaScRiPt:alert(1))`,
	`[click](  javascript:alert(1))`,
	`[click](java&#x09;script:alert(1))`,
	`[click](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)`,
	`[click](vbscript:msgbox(1))`,
	`![x](javascript:alert(1))`,
	`![x](data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=)`,
	`![x](attachment:../../etc/passwd)`,
	`![x](attachment:` + fileID + `" onerror="alert(1))`,
	`<a href="https://ok.example" onclick="alert(1)">x</a>`,
	`<div style="background:url(javascript:alert(1))">x</div>`,
	`<p class="x" id="y" style="color:red">x</p>`,
	`<form action="https://evil.example"><input name=p></form>`,
	`<math><mi xlink:href="javascript:alert(1)">x</mi></math>`,
	`<object data="javascript:alert(1)"></object>`,
	`<base href="https://evil.example/">`,
	`<meta http-equiv="refresh" content="0;url=https://evil.example">`,
	`<link rel=stylesheet href=https://evil.example/x.css>`,
	"<!--><script>alert(1)</script>-->",
	"<noscript><p title=\"</noscript><img src=x onerror=alert(1)>\">",
	"`<script>alert(1)</script>`",
	"[a](https://x.example \"t\\\" onmouseover=alert(1)\")",
	"<<script>script>alert(1)<</script>/script>",
	"[x]: javascript:alert(1)\n\n[link][x]",
	"<a href=\"&#106;avascript:alert(1)\">x</a>",
	"<img src=\"attachment:" + fileID + "\" srcset=\"https://evil.example/x 1x\">",
	"<details open ontoggle=alert(1)>",
	"<video><source onerror=alert(1)></video>",
	"<template><script>alert(1)</script></template>",
	"<table><tr><td background=\"javascript:alert(1)\">x</td></tr></table>",
}

var safeAttrs = map[string]bool{"href": true, "src": true, "alt": true, "title": true, "rel": true, "loading": true, "class": true, "start": true, "align": true}

// checkSafe parses the output as a browser would and fails on any element
// outside the allow-list, any other attribute (event handlers, style, srcset)
// and any URL that is not http(s), mailto or the blog's own file route.
func checkSafe(t *testing.T, in, out string) {
	t.Helper()
	nodes, err := html.ParseFragment(strings.NewReader(out), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		t.Fatalf("%q: output does not parse: %v", in, err)
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if _, ok := allowed[n.DataAtom]; !ok {
				t.Errorf("element <%s> in output for %q:\n%s", n.Data, in, out)
			}
			for _, a := range n.Attr {
				if !safeAttrs[a.Key] || a.Namespace != "" {
					t.Errorf("attribute %s on <%s> for %q:\n%s", a.Key, n.Data, in, out)
				}
				if a.Key == "href" || a.Key == "src" {
					u := strings.ToLower(a.Val)
					if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "mailto:") && !strings.HasPrefix(u, "/b/ann/_files/") {
						t.Errorf("URL %q for %q", a.Val, in)
					}
				}
			}
		}
		if n.Type == html.CommentNode {
			t.Errorf("comment in output for %q", in)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
}

func TestXSSCorpus(t *testing.T) {
	for _, in := range xssCorpus {
		out, err := RenderMarkdown(in, testImages)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		checkSafe(t, in, out)
		// The sanitiser alone must hold too, whatever HTML reaches it.
		out, err = Sanitize(in, testImages)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		checkSafe(t, in, out)
	}
}

func FuzzRenderMarkdown(f *testing.F) {
	for _, s := range xssCorpus {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, err := RenderMarkdown(in, testImages)
		if err != nil {
			return
		}
		checkSafe(t, in, out)
		if out2, err := Sanitize(in, testImages); err == nil {
			checkSafe(t, in, out2)
		}
	})
}

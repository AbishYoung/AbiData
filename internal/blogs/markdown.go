package blogs

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"abidata/internal/ids"
)

// The Markdown pipeline (ADR 0017): CommonMark with GFM tables and
// strikethrough, raw HTML disabled, then an allow-list sanitiser over the
// rendered HTML. It runs on every render, so a sanitiser fix applies to old
// posts too. The public pages and the editor's preview both use it.

var md = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough))

// ImageURL maps a workspace file id to the URL an image is served from: the
// public file route on blog pages, the authenticated one in the preview.
type ImageURL func(fileID string) string

// FileScheme is how a post refers to one of the blog's files:
// ![alt](attachment:ID). (goldmark treats file: URLs as dangerous.)
const FileScheme = "attachment:"

// RenderMarkdown renders a post body to sanitised HTML.
func RenderMarkdown(src string, images ImageURL) (string, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	return Sanitize(buf.String(), images)
}

// allowed elements and, for each, the attributes kept. Anything else is
// dropped: unknown elements are unwrapped (their text kept), and the
// elements in dropWhole are removed with their content.
var allowed = map[atom.Atom][]string{
	atom.H2: nil, atom.H3: nil, atom.H4: nil, atom.H5: nil, atom.H6: nil,
	atom.P: nil, atom.Br: nil, atom.Hr: nil,
	atom.Strong: nil, atom.Em: nil, atom.Del: nil, atom.Code: {"class"}, atom.Pre: nil,
	atom.Blockquote: nil, atom.Ul: nil, atom.Ol: {"start"}, atom.Li: nil,
	atom.Table: nil, atom.Thead: nil, atom.Tbody: nil, atom.Tr: nil, atom.Th: {"align"}, atom.Td: {"align"},
	atom.A: {"href", "title"}, atom.Img: {"src", "alt", "title"},
}

var dropWhole = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Iframe: true, atom.Object: true, atom.Embed: true,
	atom.Svg: true, atom.Math: true, atom.Form: true, atom.Template: true, atom.Noscript: true, atom.Textarea: true,
	atom.Select: true, atom.Title: true, atom.Head: true, atom.Frameset: true, atom.Frame: true,
}

var (
	codeClass = regexp.MustCompile(`^language-[a-z0-9+#-]{1,30}$`)
	olStart   = regexp.MustCompile(`^[0-9]{1,6}$`)
	aligns    = map[string]bool{"left": true, "center": true, "right": true}
)

// Sanitize keeps only the allow-listed elements and attributes of an HTML
// fragment. Links keep http, https and mailto URLs and get
// rel="nofollow noopener ugc"; images keep only file: references to the
// blog's own files, rewritten through images (external images are refused:
// tracking and mixed content). An h1 becomes an h2, since the page title is
// the h1.
func Sanitize(fragment string, images ImageURL) (string, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	for _, n := range nodes {
		for _, c := range clean(n, images) {
			if err := html.Render(&out, c); err != nil {
				return "", err
			}
		}
	}
	return out.String(), nil
}

// clean returns the sanitised replacement nodes for n (none, n itself, or its children).
func clean(n *html.Node, images ImageURL) []*html.Node {
	switch n.Type {
	case html.TextNode:
		return []*html.Node{{Type: html.TextNode, Data: n.Data}}
	case html.ElementNode:
	default: // comments, doctypes
		return nil
	}
	if dropWhole[n.DataAtom] {
		return nil
	}
	var kids []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		kids = append(kids, clean(c, images)...)
	}
	a := n.DataAtom
	if a == atom.H1 {
		a = atom.H2
	}
	keep, ok := allowed[a]
	if !ok {
		return kids // unwrap
	}
	el := &html.Node{Type: html.ElementNode, Data: a.String(), DataAtom: a}
	for _, at := range n.Attr {
		if at.Namespace != "" || !contains(keep, at.Key) {
			continue
		}
		v, ok := cleanAttr(a, at.Key, at.Val, images)
		if ok {
			el.Attr = append(el.Attr, html.Attribute{Key: at.Key, Val: v})
		}
	}
	switch a {
	case atom.A:
		if attr(el, "href") == "" {
			return kids // a link without a safe target is plain text
		}
		el.Attr = append(el.Attr, html.Attribute{Key: "rel", Val: "nofollow noopener ugc"})
	case atom.Img:
		if attr(el, "src") == "" {
			if alt := attr(el, "alt"); alt != "" {
				return []*html.Node{{Type: html.TextNode, Data: alt}}
			}
			return nil
		}
		el.Attr = append(el.Attr, html.Attribute{Key: "loading", Val: "lazy"})
		return []*html.Node{el}
	case atom.Br, atom.Hr:
		return []*html.Node{el}
	}
	for _, k := range kids {
		el.AppendChild(k)
	}
	return []*html.Node{el}
}

func cleanAttr(a atom.Atom, key, val string, images ImageURL) (string, bool) {
	switch key {
	case "href":
		return safeLink(val)
	case "src":
		id, ok := strings.CutPrefix(strings.TrimSpace(val), FileScheme)
		if !ok || !ids.Valid(id) || images == nil {
			return "", false
		}
		u := images(id)
		return u, u != ""
	case "class":
		return val, a == atom.Code && codeClass.MatchString(val)
	case "start":
		return val, olStart.MatchString(val)
	case "align":
		return val, aligns[val]
	case "alt", "title":
		return val, len(val) <= 300
	}
	return "", false
}

// safeLink keeps absolute http, https and mailto URLs only.
func safeLink(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return "", false
		}
	case "mailto":
		if u.Opaque == "" {
			return "", false
		}
	default:
		return "", false
	}
	return u.String(), true
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

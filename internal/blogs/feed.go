package blogs

import (
	"bytes"
	"encoding/xml"
	"strings"
	"time"

	"abidata/internal/admin"
)

// FeedPosts is how many posts the Atom feed holds.
const FeedPosts = 20

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomText struct {
	Type string `xml:"type,attr,omitempty"`
	Text string `xml:",chardata"`
}

type atomEntry struct {
	Title     string    `xml:"title"`
	Link      atomLink  `xml:"link"`
	ID        string    `xml:"id"`
	Published string    `xml:"published,omitempty"`
	Updated   string    `xml:"updated"`
	Summary   *atomText `xml:"summary,omitempty"`
}

type atomFeed struct {
	XMLName xml.Name   `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string     `xml:"title"`
	Links   []atomLink `xml:"link"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
	Author  struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Entries []atomEntry `xml:"entry"`
}

// RenderFeed renders the Atom feed of a blog's newest published posts:
// titles and summaries only, never the body. base is the blog base URL.
func RenderFeed(base string, b admin.Blog, posts []Post) ([]byte, error) {
	home := strings.TrimRight(base, "/") + "/b/" + b.Username
	f := atomFeed{Title: blogName(b), ID: home, Links: []atomLink{{Href: home}, {Href: home + "/feed.xml", Rel: "self", Type: "application/atom+xml"}}}
	f.Author.Name = "@" + b.Username
	updated := b.UpdatedAt
	for _, p := range posts {
		at := b.UpdatedAt
		if p.PublishedAt != nil {
			at = *p.PublishedAt
		}
		if at.After(updated) {
			updated = at
		}
		e := atomEntry{Title: p.Title, Link: atomLink{Href: home + "/" + p.Slug}, ID: home + "/" + p.Slug, Updated: at.UTC().Format(time.RFC3339)}
		if p.PublishedAt != nil {
			e.Published = p.PublishedAt.UTC().Format(time.RFC3339)
		}
		if p.Summary != "" {
			e.Summary = &atomText{Type: "text", Text: p.Summary}
		}
		f.Entries = append(f.Entries, e)
	}
	f.Updated = updated.UTC().Format(time.RFC3339)
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

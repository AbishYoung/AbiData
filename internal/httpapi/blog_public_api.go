package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/blogs"
	"abidata/internal/fault"
)

// The JSON form of the public blog pages (ADR 0017), under /api/v1/blogs.
// Operations with access "blog" never read cookies or Authorization (see
// authenticate), so every reader gets the same answer.

// PublicBlogPost is one post of a public blog, as listed on its home.
type PublicBlogPost struct {
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Summary     string     `json:"summary"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	URL         string     `json:"url"`
}

// PublicBlog is a public blog's home.
type PublicBlog struct {
	Username string           `json:"username" doc:"The canonical username (after an old name or another letter case)."`
	Title    string           `json:"title"`
	Intro    []string         `json:"intro" doc:"Plain-text paragraphs."`
	URL      string           `json:"url"`
	Posts    []PublicBlogPost `json:"posts" doc:"The newest published posts; unlisted posts are not listed."`
}

// PublicBlogPostPage is one public post with its rendered body.
type PublicBlogPostPage struct {
	PublicBlogPost
	Username string `json:"username"`
	HTML     string `json:"html" doc:"The body through the public Markdown pipeline and sanitiser."`
	Unlisted bool   `json:"unlisted"`
}

// publicBlogErr maps the reader's answers onto the API: hidden is the plain NOT_FOUND.
func publicBlogErr(err error) error {
	if errors.Is(err, blogs.ErrHidden) {
		return fault.E(fault.NotFound)
	}
	return err
}

// followBlog resolves a username, following an old name or another letter case once.
func (s *Server) followBlog(ctx context.Context, rd *blogs.Reader, user string) (admin.Blog, error) {
	b, err := rd.Blog(ctx, user)
	var mv *blogs.Moved
	if errors.As(err, &mv) {
		b, err = rd.Blog(ctx, mv.Username)
	}
	return b, publicBlogErr(err)
}

func (s *Server) blogAPILimited(ctx context.Context, user string) error {
	ok, wait := s.appLimits.Allow("blog:"+clientIP(ctx), blogReads)
	if ok {
		ok, wait = s.appLimits.Allow("blog-all:"+strings.ToLower(user), blogBlogReads)
	}
	if !ok {
		return &fault.Error{Code: fault.RateLimited, RetryAfter: wait}
	}
	return nil
}

type publicBlogOut struct {
	CacheControl string `header:"Cache-Control"`
	Body         PublicBlog
}

type publicBlogPostOut struct {
	CacheControl string `header:"Cache-Control"`
	Body         PublicBlogPostPage
}

func (s *Server) registerPublicBlogs(api huma.API) {
	meta := map[string]any{metaAccess: "blog"}
	tags := []string{"public blogs"}
	base := func() string { return strings.TrimRight(s.deps.Config.PublicURL, "/") }
	postOf := func(user string, p blogs.Post) PublicBlogPost {
		return PublicBlogPost{Title: p.Title, Slug: p.Slug, Summary: p.Summary, PublishedAt: p.PublishedAt, URL: base() + "/b/" + user + "/" + p.Slug}
	}

	huma.Register(api, huma.Operation{OperationID: "publicBlog", Method: http.MethodGet, Path: "/api/v1/blogs/{username}", Tags: tags, Metadata: meta,
		Security: []map[string][]string{}, Summary: "A public blog's home and its newest posts", Errors: []int{404, 429}},
		func(ctx context.Context, in *struct {
			Username string `path:"username" maxLength:"64"`
		}) (*publicBlogOut, error) {
			if err := s.blogAPILimited(ctx, in.Username); err != nil {
				return nil, err
			}
			rd := s.blogReader()
			b, err := s.followBlog(ctx, rd, in.Username)
			if err != nil {
				return nil, err
			}
			posts, err := rd.Home(ctx, b, blogs.HomePosts)
			if err != nil {
				return nil, publicBlogErr(err)
			}
			out := PublicBlog{Username: b.Username, Title: b.Title, Intro: blogs.Paragraphs(b.Intro), Posts: []PublicBlogPost{}, URL: base() + "/b/" + b.Username}
			if out.Intro == nil {
				out.Intro = []string{}
			}
			for _, p := range posts {
				out.Posts = append(out.Posts, postOf(b.Username, p))
			}
			return &publicBlogOut{CacheControl: blogCacheControl, Body: out}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "publicBlogPost", Method: http.MethodGet, Path: "/api/v1/blogs/{username}/posts/{slug}", Tags: tags, Metadata: meta,
		Security: []map[string][]string{}, Summary: "One published or unlisted post, rendered", Errors: []int{404, 429}},
		func(ctx context.Context, in *struct {
			Username string `path:"username" maxLength:"64"`
			Slug     string `path:"slug" maxLength:"100"`
		}) (*publicBlogPostOut, error) {
			if err := s.blogAPILimited(ctx, in.Username); err != nil {
				return nil, err
			}
			rd := s.blogReader()
			b, err := s.followBlog(ctx, rd, in.Username)
			if err != nil {
				return nil, err
			}
			p, err := rd.Post(ctx, b, in.Slug)
			var mv *blogs.Moved
			if errors.As(err, &mv) {
				p, err = rd.Post(ctx, b, mv.Slug)
			}
			if err != nil {
				return nil, publicBlogErr(err)
			}
			html, err := blogs.RenderMarkdown(p.Body, blogs.PublicImageURL(b.Username))
			if err != nil {
				return nil, err
			}
			return &publicBlogPostOut{CacheControl: blogCacheControl, Body: PublicBlogPostPage{PublicBlogPost: postOf(b.Username, p), Username: b.Username,
				HTML: html, Unlisted: p.Status == blogs.StatusUnlisted}}, nil
		})
}

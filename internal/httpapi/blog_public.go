package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"abidata/internal/apps"
	"abidata/internal/blogs"
	"abidata/internal/fault"
	"abidata/internal/files"
)

// Public blog pages (ADR 0017): server-rendered HTML at /b/{username} and
// /b/{username}/{slug}. These handlers never read cookies or Authorization:
// a reader's session cannot change what is shown, and nothing is set.

// Read budgets of the public blog pages, per process.
var (
	blogReads     = apps.Budget{PerMinute: 240, Burst: 120}   // per client address
	blogBlogReads = apps.Budget{PerMinute: 3000, Burst: 1000} // per blog, all addresses together
)

const blogCacheControl = "public, max-age=60, stale-while-revalidate=300"

func (s *Server) mountPublicBlogs(r chi.Router) {
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		r.MethodFunc(m, "/b/_assets/blog.css", s.blogStylesheet)
		r.MethodFunc(m, "/b/{user}", s.blogHome)
		r.MethodFunc(m, "/b/{user}/", func(w http.ResponseWriter, r *http.Request) {
			s.blogFail(w, r, &blogs.Moved{Username: chi.URLParam(r, "user")})
		})
		r.MethodFunc(m, "/b/{user}/{slug}", s.blogPost)
		r.MethodFunc(m, "/b/{user}/_files/{id}", s.blogFile)
	}
}

// blogFile serves an image of a served post. The file must be referred to by
// a published or unlisted post that is served; other workspace files, images
// of drafts and anything that is not a safe raster image are the NOT_FOUND.
func (s *Server) blogFile(w http.ResponseWriter, r *http.Request) {
	user, id := chi.URLParam(r, "user"), chi.URLParam(r, "id")
	if s.blogLimited(w, r, strings.ToLower(user)) {
		return
	}
	if s.deps.Files == nil {
		s.blogFail(w, r, blogs.ErrHidden)
		return
	}
	rd := s.blogReader()
	b, err := rd.Blog(r.Context(), user)
	var mv *blogs.Moved
	if errors.As(err, &mv) {
		mv.Slug = "_files/" + id
	}
	if err == nil {
		err = rd.FileServed(r.Context(), b, id)
	}
	if err != nil {
		s.blogFail(w, r, err)
		return
	}
	l, err := s.deps.Workspaces.Acquire(r.Context(), b.WorkspaceID)
	if err != nil {
		s.blogFail(w, r, err)
		return
	}
	f, meta, err := s.deps.Files.Open(r.Context(), l, id)
	l.Release()
	if err != nil {
		if fault.Classify(err).Code == fault.NotFound {
			err = blogs.ErrHidden
		}
		s.blogFail(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	if !files.Inline(meta.ContentType) {
		s.blogFail(w, r, blogs.ErrHidden)
		return
	}
	h := w.Header()
	h.Set("Content-Type", files.ServeType(meta.ContentType))
	h.Set("Content-Disposition", "inline")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", fileCSP)
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	// Short-lived: a take-down or unpublish must reach readers soon.
	h.Set("Cache-Control", blogCacheControl)
	h.Set("ETag", `"`+meta.SHA256+`"`)
	http.ServeContent(w, r, "", meta.CreatedAt, f)
}

// blogReader reads with the install switch as it stands now.
func (s *Server) blogReader() *blogs.Reader {
	return &blogs.Reader{Admin: s.deps.Admin, Workspaces: s.deps.Workspaces, Schemas: s.deps.Schemas, Switch: s.blogSwitch(),
		TakenDown: s.deps.Admin.TakenDown}
}

func blogHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", blogs.CSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

func (s *Server) blogStylesheet(w http.ResponseWriter, r *http.Request) {
	blogHeaders(w)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(blogs.Stylesheet))
}

// blogLimited spends one read for the client and one for the blog.
func (s *Server) blogLimited(w http.ResponseWriter, r *http.Request, key string) bool {
	ok, wait := s.appLimits.Allow("blog:"+clientIP(r.Context()), blogReads)
	if ok {
		ok, wait = s.appLimits.Allow("blog-all:"+key, blogBlogReads)
	}
	if ok {
		return false
	}
	blogHeaders(w)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(wait.Seconds()+0.999))))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte("Too many requests. Try again in a moment.\n"))
	return true
}

// blogFail answers a redirect, the one NOT_FOUND page, or a server error.
func (s *Server) blogFail(w http.ResponseWriter, r *http.Request, err error) {
	blogHeaders(w)
	var mv *blogs.Moved
	switch {
	case errors.As(err, &mv):
		to := "/b/" + mv.Username
		if mv.Slug != "" {
			to += "/" + mv.Slug
		}
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.Redirect(w, r, to, http.StatusMovedPermanently)
	case errors.Is(err, blogs.ErrHidden):
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if r.Method != http.MethodHead {
			_, _ = w.Write(blogs.NotFoundPage)
		}
	default:
		s.log.ErrorContext(r.Context(), "blog page failed", "path", r.URL.Path, "err", err)
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "The page could not be shown. Try again later.", http.StatusInternalServerError)
	}
}

// blogPage writes a rendered page, answering 304 when the reader has it.
func (s *Server) blogPage(w http.ResponseWriter, r *http.Request, blogID, etag string, unlisted bool, render func() ([]byte, error)) {
	blogHeaders(w)
	h := w.Header()
	h.Set("Cache-Control", blogCacheControl)
	h.Set("ETag", etag)
	if unlisted {
		h.Set("X-Robots-Tag", "noindex")
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, ok := s.blogPages.Get(etag)
	if !ok {
		var err error
		if body, err = render(); err != nil {
			h.Del("ETag")
			s.blogFail(w, r, err)
			return
		}
		s.blogPages.Put(blogID, etag, body)
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body) //nolint:gosec // html/template output with a sanitised body, served under a no-script CSP
}

func etagMatches(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		if t = strings.TrimSpace(t); t == "*" || strings.TrimPrefix(t, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

func (s *Server) blogHome(w http.ResponseWriter, r *http.Request) {
	user := chi.URLParam(r, "user")
	if s.blogLimited(w, r, strings.ToLower(user)) {
		return
	}
	rd := s.blogReader()
	b, err := rd.Blog(r.Context(), user)
	if err != nil {
		s.blogFail(w, r, err)
		return
	}
	posts, err := rd.Home(r.Context(), b, blogs.HomePosts)
	if err != nil {
		s.blogFail(w, r, err)
		return
	}
	s.blogPage(w, r, b.ID, blogs.HomeETag(b, posts), false, func() ([]byte, error) {
		return blogs.RenderHome(s.deps.Config.PublicURL, b, posts)
	})
}

func (s *Server) blogPost(w http.ResponseWriter, r *http.Request) {
	user, slug := chi.URLParam(r, "user"), chi.URLParam(r, "slug")
	if s.blogLimited(w, r, strings.ToLower(user)) {
		return
	}
	rd := s.blogReader()
	b, err := rd.Blog(r.Context(), user)
	var mv *blogs.Moved
	if errors.As(err, &mv) {
		// An old username keeps the rest of the address.
		mv.Slug = slug
	}
	if err != nil {
		s.blogFail(w, r, err)
		return
	}
	p, err := rd.Post(r.Context(), b, slug)
	if err != nil {
		s.blogFail(w, r, err)
		return
	}
	s.blogPage(w, r, b.ID, blogs.PostETag(b, p), p.Status == blogs.StatusUnlisted, func() ([]byte, error) {
		return blogs.RenderPost(s.deps.Config.PublicURL, b, p)
	})
}

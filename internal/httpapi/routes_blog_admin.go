package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/apps"
	"abidata/internal/blogs"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// Blog moderation (ADR 0017): administrators list blogs, take a blog or a
// post down and restore it; readers report a page.

// reportBudget limits reports per client address (only a hash of it is kept).
var reportBudget = apps.Budget{PerMinute: 1, Burst: 5}

// AdminBlogSummary is one row of the admin blog list.
type AdminBlogSummary struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	Title           string     `json:"title"`
	WorkspaceID     string     `json:"workspace_id"`
	Published       bool       `json:"published"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	PublicURL       string     `json:"public_url"`
	ActiveTakedowns int        `json:"active_takedowns"`
	OpenReports     int        `json:"open_reports"`
}

// AdminBlogPost is a published or unlisted post as an administrator sees it.
type AdminBlogPost struct {
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	URL         string     `json:"url"`
	TakenDown   bool       `json:"taken_down"`
}

// AdminTakedown is one take-down, active or restored.
type AdminTakedown struct {
	ID          string     `json:"id"`
	PostSlug    string     `json:"post_slug,omitempty" doc:"Empty for the whole blog."`
	ReasonCode  string     `json:"reason_code"`
	Note        string     `json:"note"`
	TakenDownBy string     `json:"taken_down_by"`
	TakenDownAt time.Time  `json:"taken_down_at"`
	RestoredBy  string     `json:"restored_by,omitempty"`
	RestoredAt  *time.Time `json:"restored_at,omitempty"`
}

// AdminBlogReport is a reader's report.
type AdminBlogReport struct {
	ID         string     `json:"id"`
	PostSlug   string     `json:"post_slug,omitempty"`
	ReasonCode string     `json:"reason_code"`
	Message    string     `json:"message"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// AdminBlog is one blog's moderation view: published content only. Drafts
// stay behind the audited workspace access.
type AdminBlog struct {
	AdminBlogSummary
	Intro     string            `json:"intro"`
	Posts     []AdminBlogPost   `json:"posts"`
	Takedowns []AdminTakedown   `json:"takedowns"`
	Reports   []AdminBlogReport `json:"reports"`
	Damaged   bool              `json:"damaged" doc:"The posts table is missing or changed, so nothing is served."`
}

// TakedownRequest takes a blog (no post_slug) or one post down.
type TakedownRequest struct {
	PostSlug   string `json:"post_slug,omitempty" maxLength:"80" doc:"Empty to take down the whole blog."`
	ReasonCode string `json:"reason_code" enum:"spam,phishing,malware,illegal,harassment,copyright,other"`
	Note       string `json:"note,omitempty" maxLength:"1000" doc:"Shown to the blog's owner."`
}

// BlogReportRequest is a reader's report of a blog or one of its posts.
type BlogReportRequest struct {
	PostSlug   string `json:"post_slug,omitempty" maxLength:"80"`
	ReasonCode string `json:"reason_code" enum:"spam,phishing,malware,illegal,harassment,copyright,other"`
	Message    string `json:"message,omitempty" maxLength:"2000"`
}

func (s *Server) adminBlogSummary(b admin.BlogSummary) AdminBlogSummary {
	return AdminBlogSummary{ID: b.ID, Username: b.Username, Title: b.Title, WorkspaceID: string(b.WorkspaceID), Published: b.Published,
		PublishedAt: b.PublishedAt, PublicURL: strings.TrimRight(s.deps.Config.PublicURL, "/") + "/b/" + b.Username,
		ActiveTakedowns: b.ActiveTakedowns, OpenReports: b.OpenReports}
}

func (s *Server) adminBlog(ctx context.Context, id string) (AdminBlog, error) {
	b, err := s.deps.Admin.GetBlog(ctx, id)
	if errors.Is(err, admin.ErrNotFound) {
		return AdminBlog{}, fault.E(fault.NotFound)
	} else if err != nil {
		return AdminBlog{}, err
	}
	tds, err := s.deps.Admin.Takedowns(ctx, b.ID)
	if err != nil {
		return AdminBlog{}, err
	}
	reps, err := s.deps.Admin.BlogReports(ctx, b.ID)
	if err != nil {
		return AdminBlog{}, err
	}
	sum := admin.BlogSummary{Blog: b}
	out := AdminBlog{Intro: b.Intro, Posts: []AdminBlogPost{}, Takedowns: []AdminTakedown{}, Reports: []AdminBlogReport{}}
	down := map[string]bool{}
	for _, td := range tds {
		if td.RestoredAt == nil {
			sum.ActiveTakedowns++
			down[td.PostSlug] = true
		}
		out.Takedowns = append(out.Takedowns, AdminTakedown{ID: td.ID, PostSlug: td.PostSlug, ReasonCode: td.ReasonCode, Note: td.Note,
			TakenDownBy: td.TakenDownBy, TakenDownAt: td.TakenDownAt, RestoredBy: td.RestoredBy, RestoredAt: td.RestoredAt})
	}
	for _, r := range reps {
		if r.ResolvedAt == nil {
			sum.OpenReports++
		}
		out.Reports = append(out.Reports, AdminBlogReport{ID: r.ID, PostSlug: r.PostSlug, ReasonCode: r.ReasonCode, Message: r.Message,
			CreatedAt: r.CreatedAt, ResolvedAt: r.ResolvedAt})
	}
	out.AdminBlogSummary = s.adminBlogSummary(sum)
	posts, err := s.blogReader().PublicPosts(ctx, b)
	if errors.Is(err, blogs.ErrHidden) {
		out.Damaged = true
	} else if err != nil {
		return AdminBlog{}, err
	}
	for _, p := range posts {
		out.Posts = append(out.Posts, AdminBlogPost{Title: p.Title, Slug: p.Slug, Status: p.Status, PublishedAt: p.PublishedAt,
			URL: out.PublicURL + "/" + p.Slug, TakenDown: down[p.Slug]})
	}
	return out, nil
}

func (s *Server) registerAdminBlogModeration(api huma.API) {
	tags := []string{"admin"}
	adminOnly := map[string]any{metaAccess: "platform_admin", metaSessionOnly: true}
	type blogOut = struct{ Body AdminBlog }
	type idIn struct {
		ID string `path:"id" maxLength:"64"`
	}

	huma.Register(api, huma.Operation{OperationID: "adminListBlogs", Method: http.MethodGet, Path: "/api/v1/admin/blogs", Tags: tags, Metadata: adminOnly,
		Summary: "Blogs by username, optionally only those taken down or reported", Errors: []int{401, 403}},
		func(ctx context.Context, in *struct {
			Q      string `query:"q" maxLength:"64" doc:"Part of a username."`
			Filter string `query:"filter" enum:"taken_down,reported," doc:"Only blogs with an active take-down, or with open reports."`
		}) (*struct {
			Body struct {
				Items []AdminBlogSummary `json:"items"`
			}
		}, error) {
			list, err := s.deps.Admin.ListBlogs(ctx, in.Q, in.Filter == "taken_down", in.Filter == "reported", 500)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items []AdminBlogSummary `json:"items"`
				}
			}{}
			out.Body.Items = make([]AdminBlogSummary, 0, len(list))
			for _, b := range list {
				out.Body.Items = append(out.Body.Items, s.adminBlogSummary(b))
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "adminGetBlog", Method: http.MethodGet, Path: "/api/v1/admin/blogs/{id}", Tags: tags, Metadata: adminOnly,
		Summary: "A blog's published posts, take-downs and reports (drafts only through workspace access)", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *idIn) (*blogOut, error) {
			b, err := s.adminBlog(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			return &blogOut{Body: b}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "adminTakeDownBlog", Method: http.MethodPost, Path: "/api/v1/admin/blogs/{id}/take-down", Tags: tags, Metadata: adminOnly,
		Summary: "Take a blog or one post down; it is not served until restored", Errors: []int{400, 401, 403, 404, 409}},
		func(ctx context.Context, in *struct {
			ID   string `path:"id" maxLength:"64"`
			Body TakedownRequest
		}) (*blogOut, error) {
			b, err := s.deps.Admin.GetBlog(ctx, in.ID)
			if errors.Is(err, admin.ErrNotFound) {
				return nil, fault.E(fault.NotFound)
			} else if err != nil {
				return nil, err
			}
			if in.Body.PostSlug != "" && !blogs.ValidSlug(in.Body.PostSlug) {
				return nil, fault.New(fault.BadRequest, "That is not a post slug.")
			}
			if !slices.Contains(admin.BlogReasons, in.Body.ReasonCode) {
				return nil, fault.New(fault.BadRequest, "Choose a reason.")
			}
			p, _ := PrincipalFrom(ctx)
			err = s.deps.Admin.TakeDown(ctx, admin.BlogTakedown{ID: ids.New(), BlogID: b.ID, PostSlug: in.Body.PostSlug, ReasonCode: in.Body.ReasonCode,
				Note: strings.TrimSpace(in.Body.Note), TakenDownBy: p.UserID, TakenDownAt: time.Now().UTC()})
			if errors.Is(err, admin.ErrConflict) {
				return nil, fault.New(fault.DuplicateValue, "It is already taken down.")
			} else if err != nil {
				return nil, err
			}
			s.blogPages.Purge(b.ID)
			s.audit(ctx, "blog.takedown", "blog", b.ID, map[string]any{"post_slug": in.Body.PostSlug, "reason_code": in.Body.ReasonCode, "username": b.Username})
			out, err := s.adminBlog(ctx, b.ID)
			if err != nil {
				return nil, err
			}
			return &blogOut{Body: out}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "adminRestoreBlog", Method: http.MethodPost, Path: "/api/v1/admin/blogs/{id}/restore", Tags: tags, Metadata: adminOnly,
		Summary: "End the active take-down of a blog or one post", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *struct {
			ID   string `path:"id" maxLength:"64"`
			Body struct {
				PostSlug string `json:"post_slug,omitempty" maxLength:"80" doc:"Empty for the whole blog."`
			}
		}) (*blogOut, error) {
			b, err := s.deps.Admin.GetBlog(ctx, in.ID)
			if errors.Is(err, admin.ErrNotFound) {
				return nil, fault.E(fault.NotFound)
			} else if err != nil {
				return nil, err
			}
			p, _ := PrincipalFrom(ctx)
			err = s.deps.Admin.Restore(ctx, b.ID, in.Body.PostSlug, p.UserID, time.Now().UTC())
			if errors.Is(err, admin.ErrNotFound) {
				return nil, fault.New(fault.NotFound, "Nothing is taken down there.")
			} else if err != nil {
				return nil, err
			}
			s.blogPages.Purge(b.ID)
			s.audit(ctx, "blog.restore", "blog", b.ID, map[string]any{"post_slug": in.Body.PostSlug, "username": b.Username})
			out, err := s.adminBlog(ctx, b.ID)
			if err != nil {
				return nil, err
			}
			return &blogOut{Body: out}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "adminResolveBlogReports", Method: http.MethodPost, Path: "/api/v1/admin/blogs/{id}/reports/resolve", Tags: tags,
		Metadata: adminOnly, Summary: "Mark a blog's open reports as handled", Errors: []int{401, 403, 404}},
		func(ctx context.Context, in *idIn) (*blogOut, error) {
			b, err := s.deps.Admin.GetBlog(ctx, in.ID)
			if errors.Is(err, admin.ErrNotFound) {
				return nil, fault.E(fault.NotFound)
			} else if err != nil {
				return nil, err
			}
			p, _ := PrincipalFrom(ctx)
			if err := s.deps.Admin.ResolveBlogReports(ctx, b.ID, p.UserID, time.Now().UTC()); err != nil {
				return nil, err
			}
			s.audit(ctx, "blog.reports_resolved", "blog", b.ID, nil)
			out, err := s.adminBlog(ctx, b.ID)
			if err != nil {
				return nil, err
			}
			return &blogOut{Body: out}, nil
		})

	// Readers report a page. No account; the limiter sees only a hash of the address.
	huma.Register(api, huma.Operation{OperationID: "publicBlogReport", Method: http.MethodPost, Path: "/api/v1/blogs/{username}/report", Tags: []string{"public blogs"},
		Metadata: map[string]any{metaAccess: "blog"}, Security: []map[string][]string{}, DefaultStatus: http.StatusAccepted,
		Summary: "Report a blog or one of its posts to the administrators", Errors: []int{400, 404, 429}},
		func(ctx context.Context, in *struct {
			Username string `path:"username" maxLength:"64"`
			Body     BlogReportRequest
		}) (*struct{}, error) {
			sum := sha256.Sum256([]byte(clientIP(ctx)))
			if ok, wait := s.appLimits.Allow("blog-report:"+hex.EncodeToString(sum[:8]), reportBudget); !ok {
				return nil, &fault.Error{Code: fault.RateLimited, RetryAfter: wait}
			}
			rd := s.blogReader()
			b, err := s.followBlog(ctx, rd, in.Username)
			if err != nil {
				return nil, err
			}
			slug := in.Body.PostSlug
			if slug != "" {
				p, err := rd.Post(ctx, b, slug)
				var mv *blogs.Moved
				if errors.As(err, &mv) {
					slug, err = mv.Slug, nil
				} else if err == nil {
					slug = p.Slug
				}
				if err != nil {
					return nil, publicBlogErr(err)
				}
			}
			if !slices.Contains(admin.BlogReasons, in.Body.ReasonCode) {
				return nil, fault.New(fault.BadRequest, "Choose a reason.")
			}
			err = s.deps.Admin.CreateBlogReport(ctx, admin.BlogReport{ID: ids.New(), BlogID: b.ID, PostSlug: slug, ReasonCode: in.Body.ReasonCode,
				Message: strings.TrimSpace(in.Body.Message), CreatedAt: time.Now().UTC()})
			return nil, err
		})
}

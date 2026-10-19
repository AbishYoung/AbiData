package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/blogs"
	"abidata/internal/fault"
)

// ownBlog returns the blog the request's workspace holds. Only the blog's
// owner reaches it: any other workspace, or caller, gets the normal
// workspace NOT_FOUND (ADR 0017).
func (s *Server) ownBlog(ctx context.Context) (admin.Blog, error) {
	m, ok := MembershipFrom(ctx)
	p, _ := PrincipalFrom(ctx)
	if !ok || s.deps.BlogAPI == nil || m.Workspace.SystemKind != admin.KindBlog || m.Role != admin.RoleOwner || p.IsAPIKey() {
		return admin.Blog{}, errNotFound
	}
	b, err := s.deps.Admin.GetBlogByWorkspace(ctx, m.Workspace.ID)
	if errors.Is(err, admin.ErrNotFound) || (err == nil && b.UserID != p.UserID) {
		return admin.Blog{}, errNotFound
	}
	return b, err
}

// blogSwitch is the install-wide switch for public blogs (ADR 0017).
func (s *Server) blogSwitch() blogs.InstallSwitch {
	return blogs.InstallSwitch{Admin: s.deps.Admin, Allowed: s.deps.Config.PublicBlogs}
}

// PublicBlogsSetting is the install-wide switch for public blogs.
type PublicBlogsSetting struct {
	Enabled bool `json:"enabled" doc:"Off by default: no blog is served until an administrator turns this on."`
	Forced  bool `json:"forced_off" readOnly:"true" doc:"ABIDATA_PUBLIC_BLOGS=false turned public blogs off; the setting cannot turn them on."`
}

func (s *Server) registerAdminBlogs(api huma.API) {
	tags := []string{"admin"}
	adminOnly := map[string]any{metaAccess: "platform_admin", metaSessionOnly: true}
	type out = struct{ Body PublicBlogsSetting }
	current := func(ctx context.Context) *out {
		on, forced := s.blogSwitch().On(ctx)
		return &out{Body: PublicBlogsSetting{Enabled: on, Forced: forced}}
	}
	huma.Register(api, huma.Operation{OperationID: "adminGetPublicBlogs", Method: http.MethodGet, Path: "/api/v1/admin/settings/public-blogs", Tags: tags, Metadata: adminOnly,
		Summary: "Whether user blogs are served", Errors: []int{401, 403}},
		func(ctx context.Context, _ *struct{}) (*out, error) { return current(ctx), nil })
	huma.Register(api, huma.Operation{OperationID: "adminPutPublicBlogs", Method: http.MethodPut, Path: "/api/v1/admin/settings/public-blogs", Tags: tags, Metadata: adminOnly,
		Summary: "Turn public blogs on or off for the whole server; off makes every blog address answer as unknown", Errors: []int{400, 401, 403}},
		func(ctx context.Context, in *struct{ Body PublicBlogsSetting }) (*out, error) {
			if in.Body.Enabled && !s.deps.Config.PublicBlogs {
				return nil, fault.New(fault.BadRequest, "ABIDATA_PUBLIC_BLOGS=false turns public blogs off on this server. Change the environment setting first.")
			}
			v := "false"
			if in.Body.Enabled {
				v = "true"
			}
			if err := s.deps.Admin.PutSetting(ctx, blogs.SettingPublicBlogs, v); err != nil {
				return nil, err
			}
			s.audit(ctx, "settings.public_blogs", "settings", blogs.SettingPublicBlogs, map[string]any{"enabled": in.Body.Enabled})
			return current(ctx), nil
		})
}

type blogInfoOut struct {
	Body blogs.BlogInfo
}

type blogPostOut struct {
	Body blogs.BlogPost
}

func (s *Server) registerBlog(api huma.API) {
	tags := []string{"blog"}
	// Every route needs the blog's owner; API keys can never act as owner,
	// and the publish switches are marked session-only as well (ADR 0009).
	owner := map[string]any{metaRole: string(admin.RoleOwner)}
	ownerSession := map[string]any{metaRole: string(admin.RoleOwner), metaSessionOnly: true}
	asOwner := func(ctx context.Context, fn func(b admin.Blog) error) error {
		b, err := s.ownBlog(ctx)
		if err != nil {
			return err
		}
		return fn(b)
	}

	huma.Register(api, huma.Operation{
		OperationID: "getBlog", Method: http.MethodGet, Path: "/api/v1/w/{ws}/blog", Tags: tags, Metadata: owner,
		Summary: "The blog home: title, intro, switches and public address", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, _ *struct {
		WS string `path:"ws"`
	}) (*blogInfoOut, error) {
		b, err := s.ownBlog(ctx)
		if err != nil {
			return nil, err
		}
		info, err := s.deps.BlogAPI.Info(ctx, b)
		if err != nil {
			return nil, err
		}
		return &blogInfoOut{Body: info}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateBlog", Method: http.MethodPatch, Path: "/api/v1/w/{ws}/blog", Tags: tags, Metadata: owner,
		Summary: "Change the blog's title and intro", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body struct {
			Title *string `json:"title,omitempty" maxLength:"400"`
			Intro *string `json:"intro,omitempty" maxLength:"8000"`
		}
	}) (*blogInfoOut, error) {
		b, err := s.ownBlog(ctx)
		if err != nil {
			return nil, err
		}
		info, err := s.deps.BlogAPI.UpdateInfo(ctx, b, in.Body.Title, in.Body.Intro)
		if err != nil {
			return nil, err
		}
		return &blogInfoOut{Body: info}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "setBlogPublished", Method: http.MethodPut, Path: "/api/v1/w/{ws}/blog/published", Tags: tags, Metadata: ownerSession,
		Summary: "Turn the blog on or off (session only; at most 10 publish changes an hour)", Errors: []int{400, 401, 403, 404, 429},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body struct {
			Published bool `json:"published"`
		}
	}) (*blogInfoOut, error) {
		b, err := s.ownBlog(ctx)
		if err != nil {
			return nil, err
		}
		p, _ := PrincipalFrom(ctx)
		info, err := s.deps.BlogAPI.SetPublished(ctx, b, p.UserID, in.Body.Published)
		if err != nil {
			return nil, err
		}
		return &blogInfoOut{Body: info}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listBlogPosts", Method: http.MethodGet, Path: "/api/v1/w/{ws}/blog/posts", Tags: tags, Metadata: owner,
		Summary: "The post list, last edited first, with each post's chip", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS     string `path:"ws"`
		Status string `query:"status" enum:"draft,published,unlisted,taken_down,needs_attention," doc:"Filter by chip; empty for all."`
		Q      string `query:"q" maxLength:"200" doc:"Matches titles, ignoring case."`
		Cursor string `query:"cursor" maxLength:"20"`
	}) (*struct{ Body blogs.BlogPostList }, error) {
		var out blogs.BlogPostList
		err := asOwner(ctx, func(b admin.Blog) error {
			l, err := s.lease(ctx)
			if err != nil {
				return err
			}
			defer l.Release()
			out, err = s.deps.BlogAPI.ListPosts(ctx, l, b, in.Status, in.Q, in.Cursor)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &struct{ Body blogs.BlogPostList }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createBlogPost", Method: http.MethodPost, Path: "/api/v1/w/{ws}/blog/posts", Tags: tags, Metadata: owner,
		Summary: "Create a draft post", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409, 422, 507},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body blogs.PostInput
	}) (*blogPostOut, error) {
		var out blogs.BlogPost
		err := asOwner(ctx, func(b admin.Blog) error {
			l, err := s.lease(ctx)
			if err != nil {
				return err
			}
			defer l.Release()
			out, err = s.deps.BlogAPI.CreatePost(ctx, l, b, actor(ctx), in.Body)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &blogPostOut{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getBlogPost", Method: http.MethodGet, Path: "/api/v1/w/{ws}/blog/posts/{id}", Tags: tags, Metadata: owner,
		Summary: "One post, with its body", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS string `path:"ws"`
		ID int64  `path:"id"`
	}) (*blogPostOut, error) {
		var out blogs.BlogPost
		err := asOwner(ctx, func(b admin.Blog) error {
			l, err := s.lease(ctx)
			if err != nil {
				return err
			}
			defer l.Release()
			out, err = s.deps.BlogAPI.GetPost(ctx, l, b, in.ID)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &blogPostOut{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateBlogPost", Method: http.MethodPatch, Path: "/api/v1/w/{ws}/blog/posts/{id}", Tags: tags, Metadata: owner,
		Summary: "Save a post's title, slug, summary, body or publish date (needs _version)", Errors: []int{400, 401, 403, 404, 409, 422, 507},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		ID   int64  `path:"id"`
		Body struct {
			Version int64 `json:"_version" minimum:"1"`
			blogs.PostInput
		}
	}) (*blogPostOut, error) {
		var out blogs.BlogPost
		err := asOwner(ctx, func(b admin.Blog) error {
			l, err := s.lease(ctx)
			if err != nil {
				return err
			}
			defer l.Release()
			out, err = s.deps.BlogAPI.UpdatePost(ctx, l, b, actor(ctx), in.ID, in.Body.Version, in.Body.PostInput)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &blogPostOut{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "setBlogPostStatus", Method: http.MethodPut, Path: "/api/v1/w/{ws}/blog/posts/{id}/status", Tags: tags, Metadata: ownerSession,
		Summary: "Publish, unlist or unpublish a post (session only; at most 10 publish changes an hour)", Errors: []int{400, 401, 403, 404, 409, 429},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		ID   int64  `path:"id"`
		Body struct {
			Version int64  `json:"_version" minimum:"1"`
			Status  string `json:"status" enum:"draft,published,unlisted"`
		}
	}) (*blogPostOut, error) {
		var out blogs.BlogPost
		err := asOwner(ctx, func(b admin.Blog) error {
			l, err := s.lease(ctx)
			if err != nil {
				return err
			}
			defer l.Release()
			out, err = s.deps.BlogAPI.SetStatus(ctx, l, b, actor(ctx), in.ID, in.Body.Version, in.Body.Status)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &blogPostOut{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "deleteBlogPost", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/blog/posts/{id}", Tags: tags, Metadata: owner,
		Summary: "Delete a post", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404, 409},
	}, func(ctx context.Context, in *struct {
		WS      string `path:"ws"`
		ID      int64  `path:"id"`
		Version int64  `query:"version" minimum:"1"`
	}) (*struct{}, error) {
		err := asOwner(ctx, func(admin.Blog) error {
			l, err := s.lease(ctx)
			if err != nil {
				return err
			}
			defer l.Release()
			return s.deps.BlogAPI.DeletePost(ctx, l, actor(ctx), in.ID, in.Version)
		})
		return nil, err
	})

	huma.Register(api, huma.Operation{
		OperationID: "previewBlogPost", Method: http.MethodPost, Path: "/api/v1/w/{ws}/blog/preview", Tags: tags, Metadata: owner,
		Summary: "Render Markdown through the public pipeline into a preview document; nothing is stored", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body struct {
			Title  string `json:"title,omitempty" maxLength:"400"`
			BodyMD string `json:"body_md" maxLength:"204800"`
		}
	}) (*struct {
		CacheControl string `header:"Cache-Control"`
		Body         struct {
			HTML string `json:"html" doc:"A complete document for <iframe sandbox srcdoc>."`
		}
	}, error) {
		b, err := s.ownBlog(ctx)
		if err != nil {
			return nil, err
		}
		doc, err := blogs.Preview(in.Body.Title, in.Body.BodyMD, blogs.PreviewImageURL(b.WorkspaceID))
		if err != nil {
			return nil, fault.Classify(err)
		}
		out := &struct {
			CacheControl string `header:"Cache-Control"`
			Body         struct {
				HTML string `json:"html" doc:"A complete document for <iframe sandbox srcdoc>."`
			}
		}{CacheControl: "no-store"}
		out.Body.HTML = doc
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "checkBlogSlug", Method: http.MethodGet, Path: "/api/v1/w/{ws}/blog/slug-check", Tags: tags, Metadata: owner,
		Summary: "Normalise a slug and say whether it is free", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Slug string `query:"slug" maxLength:"200"`
		ID   int64  `query:"id" doc:"The post being edited (0 for a new post)."`
	}) (*struct{ Body blogs.SlugCheck }, error) {
		var out blogs.SlugCheck
		err := asOwner(ctx, func(admin.Blog) error {
			l, err := s.lease(ctx)
			if err != nil {
				return err
			}
			defer l.Release()
			out, err = s.deps.BlogAPI.CheckSlug(ctx, l, in.Slug, in.ID)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &struct{ Body blogs.SlugCheck }{Body: out}, nil
	})
}

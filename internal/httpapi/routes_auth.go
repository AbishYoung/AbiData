package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/auth"
	"abidata/internal/fault"
)

var (
	errUnauth    = fault.E(fault.Unauthenticated)
	errForbidden = fault.E(fault.Forbidden)
	errNotFound  = fault.E(fault.NotFound)
)

// UserDTO is the public view of a user.
type UserDTO struct {
	ID              string `json:"id"`
	Email           string `json:"email"`
	DisplayName     string `json:"display_name"`
	IsPlatformAdmin bool   `json:"is_platform_admin"`
}

// MembershipDTO is a workspace the caller can open.
type MembershipDTO struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	Role          string `json:"role" enum:"owner,builder,editor,viewer"`
	Status        string `json:"status"`
}

// MeBody describes the signed-in user.
type MeBody struct {
	User        UserDTO         `json:"user"`
	CSRFToken   string          `json:"csrf_token"`
	Memberships []MembershipDTO `json:"memberships"`
}

type meOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      MeBody
}

type meGetOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         MeBody
}

func (s *Server) me(ctx context.Context, p auth.Principal) (MeBody, error) {
	acc, err := s.deps.Admin.ListWorkspacesForUser(ctx, p.UserID)
	if err != nil {
		return MeBody{}, err
	}
	ms := make([]MembershipDTO, 0, len(acc))
	for _, a := range acc {
		ms = append(ms, MembershipDTO{WorkspaceID: string(a.Workspace.ID), WorkspaceName: a.Workspace.Name,
			Role: string(a.Role), Status: string(a.Workspace.Status)})
	}
	return MeBody{User: UserDTO{ID: p.UserID, Email: p.Email, DisplayName: p.DisplayName, IsPlatformAdmin: p.IsPlatformAdmin},
		CSRFToken: p.CSRFToken, Memberships: ms}, nil
}

func userDTO(u admin.User) UserDTO {
	return UserDTO{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, IsPlatformAdmin: u.IsPlatformAdmin}
}

func (s *Server) registerAuth(api huma.API) {
	public := map[string]any{metaAccess: "public"}

	huma.Register(api, huma.Operation{
		OperationID: "login", Method: http.MethodPost, Path: "/api/v1/auth/login", Tags: []string{"auth"},
		Summary: "Sign in", Metadata: public, Errors: []int{400, 401, 403, 429},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Email    string `json:"email" maxLength:"254"`
			Password string `json:"password" maxLength:"1024"`
		}
	}) (*meOutput, error) {
		res, err := s.deps.Auth.Login(ctx, in.Body.Email, in.Body.Password, clientIP(ctx), "")
		if err != nil {
			return nil, err
		}
		// Rotate: end the session the browser presented before signing in.
		if old, ok := PrincipalFrom(ctx); ok {
			_ = s.deps.Auth.Logout(detached(ctx), old)
		}
		body, err := s.me(ctx, res.Principal)
		if err != nil {
			return nil, err
		}
		return &meOutput{SetCookie: *s.sessionCookie(res.Token, int(auth.AbsoluteTimeout.Seconds())), Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: "/api/v1/auth/logout", Tags: []string{"auth"},
		Summary: "Sign out", DefaultStatus: http.StatusNoContent, Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		SetCookie http.Cookie `header:"Set-Cookie"`
	}, error) {
		p, _ := PrincipalFrom(ctx)
		if err := s.deps.Auth.Logout(detached(ctx), p); err != nil {
			return nil, err
		}
		return &struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
		}{SetCookie: *s.sessionCookie("", -1)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "me", Method: http.MethodGet, Path: "/api/v1/auth/me", Tags: []string{"auth"},
		Summary: "Current user, memberships and CSRF token", Errors: []int{401},
	}, func(ctx context.Context, _ *struct{}) (*meGetOutput, error) {
		p, _ := PrincipalFrom(ctx)
		body, err := s.me(ctx, p)
		if err != nil {
			return nil, err
		}
		return &meGetOutput{CacheControl: "no-store", Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "changePassword", Method: http.MethodPost, Path: "/api/v1/auth/password", Tags: []string{"auth"},
		Summary: "Change password (ends other sessions)", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403},
	}, func(ctx context.Context, in *struct {
		Body struct {
			CurrentPassword string `json:"current_password" maxLength:"1024"`
			NewPassword     string `json:"new_password" maxLength:"1024"`
		}
	}) (*struct{}, error) {
		p, _ := PrincipalFrom(ctx)
		return nil, s.deps.Auth.ChangePassword(ctx, p, in.Body.CurrentPassword, in.Body.NewPassword)
	})

	huma.Register(api, huma.Operation{
		OperationID: "setupStatus", Method: http.MethodGet, Path: "/api/v1/auth/setup", Tags: []string{"auth"},
		Summary: "Whether first-admin setup is pending", Metadata: public,
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Pending bool `json:"pending"`
		}
	}, error) {
		out := &struct {
			Body struct {
				Pending bool `json:"pending"`
			}
		}{}
		out.Body.Pending = s.deps.Auth.BootstrapPending()
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "completeSetup", Method: http.MethodPost, Path: "/api/v1/auth/setup", Tags: []string{"auth"},
		Summary: "Create the first platform admin with the one-time setup token", Metadata: public, Errors: []int{400, 403},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Token       string `json:"token" maxLength:"128"`
			DisplayName string `json:"display_name" maxLength:"200"`
			Password    string `json:"password" maxLength:"1024"`
		}
	}) (*meOutput, error) {
		u, err := s.deps.Auth.CompleteBootstrap(ctx, in.Body.Token, in.Body.DisplayName, in.Body.Password)
		if err != nil {
			return nil, err
		}
		res, err := s.deps.Auth.Login(ctx, u.Email, in.Body.Password, clientIP(ctx), "")
		if err != nil {
			return nil, err
		}
		body, err := s.me(ctx, res.Principal)
		if err != nil {
			return nil, err
		}
		return &meOutput{SetCookie: *s.sessionCookie(res.Token, int(auth.AbsoluteTimeout.Seconds())), Body: body}, nil
	})
}

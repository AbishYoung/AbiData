package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// APIKeyDTO describes a key. It never carries the secret or its hash; the full
// token appears once, in the create response.
type APIKeyDTO struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	WorkspaceName string     `json:"workspace_name,omitempty" doc:"Admin listing only."`
	Name          string     `json:"name"`
	Role          string     `json:"role" enum:"viewer,editor,builder"`
	CreatedBy     string     `json:"created_by"`
	CreatedByName string     `json:"created_by_name,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	Revoked       bool       `json:"revoked"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	Expired       bool       `json:"expired"`
}

// APIKeyCreated is the one response that carries the token.
type APIKeyCreated struct {
	APIKeyDTO
	Token string `json:"token" doc:"The full API key. It is shown only in this response and cannot be recovered."`
}

func apiKeyDTO(k admin.APIKey, now time.Time) APIKeyDTO {
	return APIKeyDTO{ID: k.ID, WorkspaceID: string(k.WorkspaceID), Name: k.Name, Role: string(k.Role), CreatedBy: k.CreatedBy,
		CreatedByName: k.CreatedByName, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt,
		Revoked: k.RevokedAt != nil, RevokedAt: k.RevokedAt, Expired: k.ExpiresAt != nil && !now.Before(*k.ExpiresAt)}
}

type apiKeyList struct {
	Body struct {
		Items []APIKeyDTO `json:"items"`
	}
}

func (s *Server) listKeys(ctx context.Context, ws ids.WorkspaceID, names map[string]string) (*apiKeyList, error) {
	ks, err := s.deps.Admin.ListAPIKeys(ctx, ws)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := &apiKeyList{}
	out.Body.Items = make([]APIKeyDTO, 0, len(ks))
	for _, k := range ks {
		d := apiKeyDTO(k, now)
		d.WorkspaceName = names[d.WorkspaceID]
		out.Body.Items = append(out.Body.Items, d)
	}
	return out, nil
}

func (s *Server) revokeKey(ctx context.Context, k admin.APIKey) error {
	if err := s.deps.Admin.RevokeAPIKey(detached(ctx), k.ID, time.Now()); err != nil {
		return mapAdminErr(err)
	}
	s.audit(ctx, "apikey.revoke", "api_key", k.ID, map[string]any{"workspace_id": string(k.WorkspaceID), "name": k.Name, "role": string(k.Role)})
	return nil
}

func (s *Server) registerAPIKeys(api huma.API) {
	// Key management is for signed-in owners only. sessionOnly keeps that true
	// even if a key were ever issued a higher role.
	owner := map[string]any{metaRole: string(admin.RoleOwner), metaSessionOnly: true}
	adminOnly := map[string]any{metaAccess: "platform_admin", metaSessionOnly: true}
	tags := []string{"api-keys"}

	huma.Register(api, huma.Operation{
		OperationID: "listApiKeys", Method: http.MethodGet, Path: "/api/v1/w/{ws}/api-keys", Tags: tags, Metadata: owner,
		Summary: "List the workspace's API keys (never the secrets)", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, _ *struct {
		WS string `path:"ws"`
	}) (*apiKeyList, error) {
		m, _ := MembershipFrom(ctx)
		return s.listKeys(ctx, m.Workspace.ID, nil)
	})

	huma.Register(api, huma.Operation{
		OperationID: "createApiKey", Method: http.MethodPost, Path: "/api/v1/w/{ws}/api-keys", Tags: tags, Metadata: owner,
		Summary:       "Create an API key; the token is returned once",
		Description:   "A key can have the viewer, editor or builder role, never owner.",
		DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body struct {
			Name          string `json:"name" minLength:"1" maxLength:"100"`
			Role          string `json:"role" enum:"viewer,editor,builder"`
			ExpiresInDays *int   `json:"expires_in_days,omitempty" minimum:"1" maximum:"3650" doc:"Omit for a key that does not expire."`
		}
	}) (*struct{ Body APIKeyCreated }, error) {
		m, _ := MembershipFrom(ctx)
		p, _ := PrincipalFrom(ctx)
		var ttl time.Duration
		if in.Body.ExpiresInDays != nil {
			ttl = time.Duration(*in.Body.ExpiresInDays) * 24 * time.Hour
		}
		k, token, err := s.deps.Auth.CreateAPIKey(ctx, m.Workspace.ID, in.Body.Name, admin.Role(in.Body.Role), p.UserID, ttl)
		if err != nil {
			return nil, err
		}
		k.CreatedByName = p.DisplayName
		details := map[string]any{"workspace_id": string(k.WorkspaceID), "name": k.Name, "role": string(k.Role)}
		if k.ExpiresAt != nil {
			details["expires_at"] = k.ExpiresAt
		}
		s.audit(ctx, "apikey.create", "api_key", k.ID, details)
		return &struct{ Body APIKeyCreated }{Body: APIKeyCreated{APIKeyDTO: apiKeyDTO(k, time.Now()), Token: token}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "revokeApiKey", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/api-keys/{id}", Tags: tags, Metadata: owner,
		Summary: "Revoke an API key (immediate; revoking twice is fine)", DefaultStatus: http.StatusNoContent, Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS string `path:"ws"`
		ID string `path:"id"`
	}) (*struct{}, error) {
		m, _ := MembershipFrom(ctx)
		k, err := s.deps.Admin.GetAPIKey(ctx, in.ID)
		if errors.Is(err, admin.ErrNotFound) || (err == nil && k.WorkspaceID != m.Workspace.ID) {
			return nil, fault.New(fault.NotFound, "API key not found.")
		}
		if err != nil {
			return nil, err
		}
		return nil, s.revokeKey(ctx, k)
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminListApiKeys", Method: http.MethodGet, Path: "/api/v1/admin/api-keys", Tags: []string{"admin"}, Metadata: adminOnly,
		Summary: "List API keys of one workspace, or of all workspaces", Errors: []int{400, 401, 403},
	}, func(ctx context.Context, in *struct {
		Workspace string `query:"workspace" doc:"Workspace ID; omit for every workspace."`
	}) (*apiKeyList, error) {
		var ws ids.WorkspaceID
		if in.Workspace != "" {
			var err error
			if ws, err = ids.ParseWorkspaceID(in.Workspace); err != nil {
				return nil, fault.New(fault.BadRequest, "That is not a workspace ID.")
			}
		}
		all, err := s.deps.Admin.ListWorkspaces(ctx)
		if err != nil {
			return nil, err
		}
		names := make(map[string]string, len(all))
		for _, w := range all {
			names[string(w.ID)] = w.Name
		}
		return s.listKeys(ctx, ws, names)
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminRevokeApiKey", Method: http.MethodDelete, Path: "/api/v1/admin/api-keys/{id}", Tags: []string{"admin"}, Metadata: adminOnly,
		Summary: "Revoke any workspace's API key", DefaultStatus: http.StatusNoContent, Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*struct{}, error) {
		k, err := s.deps.Admin.GetAPIKey(ctx, in.ID)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		return nil, s.revokeKey(ctx, k)
	})
}

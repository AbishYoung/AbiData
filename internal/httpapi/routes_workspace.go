package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// WorkspaceInfo is what a member sees about a workspace.
type WorkspaceInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Role   string `json:"role" enum:"owner,builder,editor,viewer"`
}

// MemberDTO is a workspace member.
type MemberDTO struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role" enum:"owner,builder,editor,viewer"`
}

func (s *Server) removeWorkspaceFiles(id ids.WorkspaceID) {
	p, err := s.deps.Workspaces.PathsFor(id)
	if err != nil {
		return
	}
	for _, f := range []string{p.DB, p.WAL, p.SHM} {
		_ = os.Remove(f)
	}
}

func (s *Server) registerWorkspace(api huma.API) {
	tags := []string{"workspace"}
	// Membership is managed by signed-in owners only: sessionOnly keeps API keys
	// out even if a key were ever issued a higher role.
	owner := map[string]any{metaRole: string(admin.RoleOwner), metaSessionOnly: true}

	huma.Register(api, huma.Operation{
		OperationID: "getWorkspace", Method: http.MethodGet, Path: "/api/v1/w/{ws}", Tags: tags,
		Summary: "Workspace summary and the caller's role", Errors: []int{401, 404},
	}, func(ctx context.Context, _ *struct {
		WS string `path:"ws"`
	}) (*struct{ Body WorkspaceInfo }, error) {
		m, _ := MembershipFrom(ctx)
		return &struct{ Body WorkspaceInfo }{Body: WorkspaceInfo{ID: string(m.Workspace.ID), Name: m.Workspace.Name,
			Status: string(m.Workspace.Status), Role: string(m.Role)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listMembers", Method: http.MethodGet, Path: "/api/v1/w/{ws}/members", Tags: tags, Metadata: owner,
		Summary: "List members", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, _ *struct {
		WS string `path:"ws"`
	}) (*struct {
		Body struct {
			Items []MemberDTO `json:"items"`
		}
	}, error) {
		m, _ := MembershipFrom(ctx)
		ms, err := s.deps.Admin.ListMembers(ctx, m.Workspace.ID)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []MemberDTO `json:"items"`
			}
		}{}
		out.Body.Items = make([]MemberDTO, 0, len(ms))
		for _, x := range ms {
			out.Body.Items = append(out.Body.Items, MemberDTO{UserID: x.UserID, Email: x.Email, DisplayName: x.DisplayName, Role: string(x.Role)})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "addMember", Method: http.MethodPost, Path: "/api/v1/w/{ws}/members", Tags: tags, Metadata: owner,
		Summary: "Add an existing user to the workspace", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404, 409},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		Body struct {
			Email string `json:"email" maxLength:"254"`
			Role  string `json:"role" enum:"owner,builder,editor,viewer"`
		}
	}) (*struct{ Body MemberDTO }, error) {
		m, _ := MembershipFrom(ctx)
		u, err := s.deps.Admin.GetUserByEmail(ctx, in.Body.Email)
		if errors.Is(err, admin.ErrNotFound) {
			return nil, fault.New(fault.BadRequest, "No user has that email. Ask a platform administrator to create the account first.")
		} else if err != nil {
			return nil, err
		}
		if _, err := s.deps.Admin.GetRole(ctx, m.Workspace.ID, u.ID); err == nil {
			return nil, fault.New(fault.DuplicateValue, "That user is already a member.")
		}
		role := admin.Role(in.Body.Role)
		if err := s.deps.Admin.SetMembership(ctx, admin.Membership{WorkspaceID: m.Workspace.ID, UserID: u.ID, Role: role}); err != nil {
			return nil, err
		}
		s.audit(ctx, "member.add", "workspace", string(m.Workspace.ID), map[string]any{"user_id": u.ID, "role": role})
		return &struct{ Body MemberDTO }{Body: MemberDTO{UserID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Role: string(role)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateMember", Method: http.MethodPatch, Path: "/api/v1/w/{ws}/members/{user}", Tags: tags, Metadata: owner,
		Summary: "Change a member's role", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		User string `path:"user"`
		Body struct {
			Role string `json:"role" enum:"owner,builder,editor,viewer"`
		}
	}) (*struct{ Body MemberDTO }, error) {
		m, _ := MembershipFrom(ctx)
		role := admin.Role(in.Body.Role)
		if err := s.deps.Admin.ChangeMembership(ctx, m.Workspace.ID, in.User, &role); err != nil {
			return nil, mapAdminErr(err)
		}
		u, err := s.deps.Admin.GetUser(ctx, in.User)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		s.audit(ctx, "member.update", "workspace", string(m.Workspace.ID), map[string]any{"user_id": in.User, "role": role})
		return &struct{ Body MemberDTO }{Body: MemberDTO{UserID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Role: string(role)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "removeMember", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/members/{user}", Tags: tags, Metadata: owner,
		Summary: "Remove a member", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		WS   string `path:"ws"`
		User string `path:"user"`
	}) (*struct{}, error) {
		m, _ := MembershipFrom(ctx)
		if err := s.deps.Admin.ChangeMembership(ctx, m.Workspace.ID, in.User, nil); err != nil {
			return nil, mapAdminErr(err)
		}
		s.audit(ctx, "member.remove", "workspace", string(m.Workspace.ID), map[string]any{"user_id": in.User})
		return nil, nil
	})
}

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/backup"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// WorkspaceDTO is the admin view of a workspace.
type WorkspaceDTO struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Status    string      `json:"status" enum:"active,suspended,restoring,needs_repair,deleted"`
	Quota     admin.Quota `json:"quota"`
	CreatedAt time.Time   `json:"created_at"`
	CreatedBy string      `json:"created_by"`
}

func workspaceDTO(w admin.Workspace) WorkspaceDTO {
	return WorkspaceDTO{ID: string(w.ID), Name: w.Name, Status: string(w.Status), Quota: w.Quota, CreatedAt: w.CreatedAt, CreatedBy: w.CreatedBy}
}

// AdminUserDTO is the admin view of a user.
type AdminUserDTO struct {
	UserDTO
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func (s *Server) audit(ctx context.Context, action, kind, id string, details map[string]any) {
	p, _ := PrincipalFrom(ctx)
	if err := s.deps.Admin.Audit(detached(ctx), admin.AuditEntry{At: time.Now(), UserID: p.UserID, Action: action,
		ObjectKind: kind, ObjectID: id, Details: details}); err != nil {
		s.log.WarnContext(ctx, "audit write failed", "err", err)
	}
}

func (s *Server) registerAdmin(api huma.API) {
	adminOnly := map[string]any{metaAccess: "platform_admin"}
	tags := []string{"admin"}

	huma.Register(api, huma.Operation{
		OperationID: "adminListWorkspaces", Method: http.MethodGet, Path: "/api/v1/admin/workspaces", Tags: tags, Metadata: adminOnly,
		Summary: "List workspaces", Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Items []WorkspaceDTO `json:"items"`
		}
	}, error) {
		ws, err := s.deps.Admin.ListWorkspaces(ctx)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []WorkspaceDTO `json:"items"`
			}
		}{}
		out.Body.Items = make([]WorkspaceDTO, 0, len(ws))
		for _, w := range ws {
			out.Body.Items = append(out.Body.Items, workspaceDTO(w))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminCreateWorkspace", Method: http.MethodPost, Path: "/api/v1/admin/workspaces", Tags: tags, Metadata: adminOnly,
		Summary: "Create a workspace", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 409},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Name       string       `json:"name" minLength:"1" maxLength:"100"`
			OwnerEmail string       `json:"owner_email,omitempty" maxLength:"254" doc:"Defaults to the caller."`
			Quota      *admin.Quota `json:"quota,omitempty"`
		}
	}) (*struct{ Body WorkspaceDTO }, error) {
		p, _ := PrincipalFrom(ctx)
		name := strings.TrimSpace(in.Body.Name)
		if name == "" {
			return nil, fault.New(fault.BadRequest, "Enter a workspace name.")
		}
		owner := p.UserID
		if in.Body.OwnerEmail != "" {
			u, err := s.deps.Admin.GetUserByEmail(ctx, in.Body.OwnerEmail)
			if errors.Is(err, admin.ErrNotFound) {
				return nil, fault.New(fault.BadRequest, "No user has that owner email.")
			} else if err != nil {
				return nil, err
			}
			owner = u.ID
		}
		w := admin.Workspace{ID: ids.NewWorkspaceID(), Name: name, Status: admin.StatusActive, CreatedAt: time.Now(), CreatedBy: p.UserID}
		if in.Body.Quota != nil {
			w.Quota = *in.Body.Quota
		}
		// File first (no one can reach it without the admin row), then the row.
		dctx := detached(ctx)
		if err := s.deps.Workspaces.Create(dctx, w.ID); err != nil {
			return nil, err
		}
		if err := s.deps.Admin.CreateWorkspace(dctx, w, owner); err != nil {
			s.removeWorkspaceFiles(w.ID)
			if errors.Is(err, admin.ErrConflict) {
				return nil, fault.New(fault.DuplicateValue, "A workspace with this name already exists.")
			}
			return nil, err
		}
		s.audit(ctx, "workspace.create", "workspace", string(w.ID), map[string]any{"name": name, "owner": owner})
		return &struct{ Body WorkspaceDTO }{Body: workspaceDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminUpdateWorkspace", Method: http.MethodPatch, Path: "/api/v1/admin/workspaces/{id}", Tags: tags, Metadata: adminOnly,
		Summary: "Rename, suspend/activate, or change quota", Errors: []int{400, 401, 403, 404, 409},
	}, func(ctx context.Context, in *struct {
		ID   string `path:"id"`
		Body struct {
			Name   *string      `json:"name,omitempty" maxLength:"100"`
			Status *string      `json:"status,omitempty" enum:"active,suspended"`
			Quota  *admin.Quota `json:"quota,omitempty"`
		}
	}) (*struct{ Body WorkspaceDTO }, error) {
		id, err := ids.ParseWorkspaceID(in.ID)
		if err != nil {
			return nil, errNotFound
		}
		cur, err := s.deps.Admin.GetWorkspace(ctx, id)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		if cur.Status == admin.StatusDeleted || cur.Status == admin.StatusRestoring {
			return nil, fault.Newf(fault.BadRequest, "A %s workspace cannot be changed here.", cur.Status)
		}
		patch := admin.WorkspacePatch{Name: in.Body.Name, Quota: in.Body.Quota}
		if in.Body.Status != nil {
			st := admin.WorkspaceStatus(*in.Body.Status)
			patch.Status = &st
		}
		if err := s.deps.Admin.UpdateWorkspace(ctx, id, patch); err != nil {
			return nil, mapAdminErr(err)
		}
		if patch.Status != nil && *patch.Status == admin.StatusSuspended {
			// Suspension stops what is already running, not only new requests.
			if s.deps.Jobs != nil && s.deps.Jobs.Scheduler != nil {
				s.deps.Jobs.Scheduler.CancelWorkspace(id)
			}
			if s.deps.Ops != nil {
				s.deps.Ops.CancelWorkspace(id)
			}
		}
		if in.Body.Quota != nil {
			if err := s.deps.Workspaces.ReapplyQuota(detached(ctx), id); err != nil {
				return nil, err
			}
		}
		w, err := s.deps.Admin.GetWorkspace(ctx, id)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		s.audit(ctx, "workspace.update", "workspace", string(id), nil)
		return &struct{ Body WorkspaceDTO }{Body: workspaceDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminListUsers", Method: http.MethodGet, Path: "/api/v1/admin/users", Tags: tags, Metadata: adminOnly,
		Summary: "List users", Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Items []AdminUserDTO `json:"items"`
		}
	}, error) {
		us, err := s.deps.Admin.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items []AdminUserDTO `json:"items"`
			}
		}{}
		out.Body.Items = make([]AdminUserDTO, 0, len(us))
		for _, u := range us {
			out.Body.Items = append(out.Body.Items, AdminUserDTO{UserDTO: userDTO(u), Disabled: u.Disabled, CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminCreateUser", Method: http.MethodPost, Path: "/api/v1/admin/users", Tags: tags, Metadata: adminOnly,
		Summary: "Create a user", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 409},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Email           string `json:"email" maxLength:"254"`
			DisplayName     string `json:"display_name" maxLength:"200"`
			Password        string `json:"password" maxLength:"1024"`
			IsPlatformAdmin bool   `json:"is_platform_admin,omitempty"`
		}
	}) (*struct{ Body AdminUserDTO }, error) {
		u, err := s.deps.Auth.CreateUser(ctx, in.Body.Email, in.Body.DisplayName, in.Body.Password, in.Body.IsPlatformAdmin)
		if err != nil {
			return nil, err
		}
		s.audit(ctx, "user.create", "user", u.ID, map[string]any{"email": u.Email, "platform_admin": u.IsPlatformAdmin})
		return &struct{ Body AdminUserDTO }{Body: AdminUserDTO{UserDTO: userDTO(u), CreatedAt: u.CreatedAt}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminUpdateUser", Method: http.MethodPatch, Path: "/api/v1/admin/users/{id}", Tags: tags, Metadata: adminOnly,
		Summary: "Update a user (disable, rename, platform admin flag)", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		ID   string `path:"id"`
		Body struct {
			DisplayName     *string `json:"display_name,omitempty" maxLength:"200"`
			IsPlatformAdmin *bool   `json:"is_platform_admin,omitempty"`
			Disabled        *bool   `json:"disabled,omitempty"`
		}
	}) (*struct{ Body AdminUserDTO }, error) {
		p, _ := PrincipalFrom(ctx)
		if in.ID == p.UserID && ((in.Body.Disabled != nil && *in.Body.Disabled) || (in.Body.IsPlatformAdmin != nil && !*in.Body.IsPlatformAdmin)) {
			return nil, fault.New(fault.BadRequest, "You cannot disable yourself or remove your own platform admin role.")
		}
		if err := s.deps.Admin.UpdateUser(ctx, in.ID, admin.UserPatch{DisplayName: in.Body.DisplayName,
			IsPlatformAdmin: in.Body.IsPlatformAdmin, Disabled: in.Body.Disabled}); err != nil {
			return nil, mapAdminErr(err)
		}
		if in.Body.Disabled != nil && *in.Body.Disabled {
			_ = s.deps.Admin.DeleteUserSessions(detached(ctx), in.ID)
		}
		u, err := s.deps.Admin.GetUser(ctx, in.ID)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		s.audit(ctx, "user.update", "user", u.ID, nil)
		return &struct{ Body AdminUserDTO }{Body: AdminUserDTO{UserDTO: userDTO(u), Disabled: u.Disabled, CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminGetSettings", Method: http.MethodGet, Path: "/api/v1/admin/settings", Tags: tags, Metadata: adminOnly,
		Summary: "Platform settings (values are JSON text)", Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body map[string]string }, error) {
		m, err := s.deps.Admin.ListSettings(ctx)
		if err != nil {
			return nil, err
		}
		return &struct{ Body map[string]string }{Body: m}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminStats", Method: http.MethodGet, Path: "/api/v1/admin/stats", Tags: tags, Metadata: adminOnly,
		Summary: "Server statistics", Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body AdminStats }, error) {
		ws, err := s.deps.Admin.ListWorkspaces(ctx)
		if err != nil {
			return nil, err
		}
		users, err := s.deps.Admin.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		out := AdminStats{Version: s.deps.Version, UptimeSeconds: int64(time.Since(s.started).Seconds()),
			OpenWorkspaces: s.deps.Workspaces.Open(), Users: len(users), HeapBytes: ms.HeapAlloc, Goroutines: runtime.NumGoroutine()}
		for _, w := range ws {
			if w.Status != admin.StatusDeleted {
				out.Workspaces++
			}
		}
		return &struct{ Body AdminStats }{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "adminGetBackupPolicy", Method: http.MethodGet, Path: "/api/v1/admin/settings/backup", Tags: tags, Metadata: adminOnly,
		Summary: "The scheduled-backup policy", Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body backup.Policy }, error) {
		p, err := backup.LoadPolicy(ctx, s.deps.Admin)
		if err != nil {
			return nil, err
		}
		return &struct{ Body backup.Policy }{Body: p}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "adminPutBackupPolicy", Method: http.MethodPut, Path: "/api/v1/admin/settings/backup", Tags: tags, Metadata: adminOnly,
		Summary: "Replace the scheduled-backup policy", Errors: []int{400, 401, 403},
	}, func(ctx context.Context, in *struct{ Body backup.Policy }) (*struct{ Body backup.Policy }, error) {
		b, _ := json.Marshal(in.Body)
		if err := s.deps.Admin.PutSetting(ctx, backup.PolicySettingKey, string(b)); err != nil {
			return nil, err
		}
		s.audit(ctx, "backup.policy", "settings", backup.PolicySettingKey, map[string]any{"enabled": in.Body.Enabled, "hour_utc": in.Body.HourUTC})
		return &struct{ Body backup.Policy }{Body: in.Body}, nil
	})
}

func mapAdminErr(err error) error {
	switch {
	case errors.Is(err, admin.ErrNotFound):
		return errNotFound
	case errors.Is(err, admin.ErrConflict):
		return fault.E(fault.DuplicateValue)
	case errors.Is(err, admin.ErrLastOwner):
		return fault.New(fault.BadRequest, "A workspace must keep at least one owner.")
	}
	return err
}

// AdminStats summarises the server for the admin console.
type AdminStats struct {
	Version        string `json:"version"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	OpenWorkspaces int    `json:"open_workspaces"`
	Workspaces     int    `json:"workspaces"`
	Users          int    `json:"users"`
	HeapBytes      uint64 `json:"heap_bytes"`
	Goroutines     int    `json:"goroutines"`
}

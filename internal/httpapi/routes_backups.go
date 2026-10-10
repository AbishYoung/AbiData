package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// BackupDTO is one backup of a workspace.
type BackupDTO struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind" enum:"manual,scheduled,restore_point,pre_restore"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	RawBytes  int64     `json:"raw_bytes"`
	GzBytes   int64     `json:"gz_bytes"`
}

func backupDTO(b admin.Backup) BackupDTO {
	return BackupDTO{ID: b.ID, Kind: b.Kind, CreatedAt: b.CreatedAt, CreatedBy: b.CreatedBy, RawBytes: b.RawBytes, GzBytes: b.GzBytes}
}

// ownBackup loads a backup of the caller's workspace (404 for any other).
func (s *Server) ownBackup(ctx context.Context, id string) (admin.Backup, error) {
	m, ok := MembershipFrom(ctx)
	if !ok || !ids.Valid(id) || s.deps.Backups == nil {
		return admin.Backup{}, errNotFound
	}
	b, err := s.deps.Admin.GetBackup(ctx, id)
	if err != nil || b.WorkspaceID != m.Workspace.ID {
		return admin.Backup{}, errNotFound
	}
	return b, nil
}

func (s *Server) registerBackups(api huma.API) {
	owner := map[string]any{metaRole: string(admin.RoleOwner)}
	tags := []string{"backups"}

	huma.Register(api, huma.Operation{OperationID: "listBackups", Method: http.MethodGet, Path: "/api/v1/w/{ws}/backups", Tags: tags, Metadata: owner,
		Summary: "List the workspace's backups, newest first (owner)", Errors: []int{401, 403, 404}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct {
			Body struct {
				Items []BackupDTO `json:"items"`
			}
		}, error) {
			m, _ := MembershipFrom(ctx)
			all, err := s.deps.Admin.ListBackups(ctx, m.Workspace.ID)
			if err != nil {
				return nil, err
			}
			out := &struct {
				Body struct {
					Items []BackupDTO `json:"items"`
				}
			}{}
			out.Body.Items = make([]BackupDTO, 0, len(all))
			for _, b := range all {
				out.Body.Items = append(out.Body.Items, backupDTO(b))
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createBackup", Method: http.MethodPost, Path: "/api/v1/w/{ws}/backups", Tags: tags, Metadata: owner,
		Summary: "Back up the workspace now (owner); writers continue meanwhile", DefaultStatus: http.StatusCreated, Errors: []int{401, 403, 404, 503, 507}},
		func(ctx context.Context, _ *struct {
			WS string `path:"ws"`
		}) (*struct{ Body BackupDTO }, error) {
			if s.deps.Backups == nil {
				return nil, errNotFound
			}
			l, err := s.lease(ctx)
			if err != nil {
				return nil, err
			}
			defer l.Release()
			p, _ := PrincipalFrom(ctx)
			b, err := s.deps.Backups.Manual(detached(ctx), l, p.UserID)
			if err != nil {
				return nil, err
			}
			s.audit(ctx, "backup.create", "backup", b.ID, map[string]any{"gz_bytes": b.GzBytes})
			return &struct{ Body BackupDTO }{Body: backupDTO(b)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteBackup", Method: http.MethodDelete, Path: "/api/v1/w/{ws}/backups/{id}", Tags: tags, Metadata: owner,
		Summary: "Delete a manual backup (owner); other kinds follow the retention policy", DefaultStatus: http.StatusNoContent, Errors: []int{400, 401, 403, 404}},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{}, error) {
			b, err := s.ownBackup(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if b.Kind != admin.BackupManual {
				return nil, fault.New(fault.BadRequest, "Only manual backups can be deleted; the others are removed by the retention policy.")
			}
			if err := s.deps.Backups.Delete(detached(ctx), b); err != nil {
				return nil, err
			}
			s.audit(ctx, "backup.delete", "backup", b.ID, nil)
			return &struct{}{}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "restoreBackup", Method: http.MethodPost, Path: "/api/v1/w/{ws}/backups/{id}/restore", Tags: tags, Metadata: owner,
		Summary:     "Replace the workspace with this backup (owner)",
		Description: "Everyone is disconnected while the files are swapped. The replaced files are kept as a pre_restore backup.",
		Errors:      []int{401, 403, 404, 422, 500, 503}, DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			WS string `path:"ws"`
			ID string `path:"id"`
		}) (*struct{}, error) {
			b, err := s.ownBackup(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			// No lease is held here: the restore drains the workspace itself.
			p, _ := PrincipalFrom(ctx)
			if err := s.deps.Backups.Restore(detached(ctx), b.WorkspaceID, b.ID, p.UserID); err != nil {
				return nil, err
			}
			return &struct{}{}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "restoreBackupAsNew", Method: http.MethodPost, Path: "/api/v1/w/{ws}/backups/{id}/restore-as-new", Tags: tags, Metadata: owner,
		Summary: "Create a new workspace from this backup (owner); the caller owns it", DefaultStatus: http.StatusCreated,
		Errors: []int{400, 401, 403, 404, 409, 422, 507}},
		func(ctx context.Context, in *struct {
			WS   string `path:"ws"`
			ID   string `path:"id"`
			Body struct {
				Name string `json:"name" minLength:"1" maxLength:"100"`
			}
		}) (*struct{ Body WorkspaceDTO }, error) {
			b, err := s.ownBackup(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(in.Body.Name)
			if name == "" {
				return nil, fault.New(fault.BadRequest, "Enter a name for the new workspace.")
			}
			m, _ := MembershipFrom(ctx)
			p, _ := PrincipalFrom(ctx)
			// Creating workspaces is a platform-admin power; owners get a few
			// copies of their own (Phase 6 review M1).
			if !p.IsPlatformAdmin {
				n, err := s.createdBy(ctx, p.UserID)
				if err != nil {
					return nil, err
				}
				if n >= MaxOwnerCopies {
					return nil, fault.Newf(fault.QuotaExceeded, "You can create at most %d workspaces from backups. Ask an administrator for more.", MaxOwnerCopies)
				}
			}
			id, err := s.deps.Backups.RestoreAsNew(detached(ctx), b.ID, func(id ids.WorkspaceID) error {
				w := admin.Workspace{ID: id, Name: name, Status: admin.StatusRestoring, Quota: m.Workspace.Quota, CreatedAt: time.Now(), CreatedBy: p.UserID}
				return s.deps.Admin.CreateWorkspace(detached(ctx), w, p.UserID)
			})
			if errors.Is(err, admin.ErrConflict) {
				return nil, fault.New(fault.DuplicateValue, "A workspace with this name already exists.")
			}
			if err != nil {
				return nil, err
			}
			w, err := s.deps.Admin.GetWorkspace(ctx, id)
			if err != nil {
				return nil, mapAdminErr(err)
			}
			s.audit(ctx, "workspace.restore_as_new", "workspace", string(id), map[string]any{"backup_id": b.ID, "from": string(b.WorkspaceID)})
			return &struct{ Body WorkspaceDTO }{Body: workspaceDTO(w)}, nil
		})
}

// MaxOwnerCopies is how many live workspaces a user who is not a platform
// admin may have created (through restore-as-new).
const MaxOwnerCopies = 3

func (s *Server) createdBy(ctx context.Context, userID string) (int, error) {
	all, err := s.deps.Admin.ListWorkspaces(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, w := range all {
		if w.CreatedBy == userID && w.Status != admin.StatusDeleted {
			n++
		}
	}
	return n, nil
}

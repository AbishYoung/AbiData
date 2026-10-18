package admin

import (
	"context"
	"encoding/json"
	"fmt"

	"abidata/internal/dbx"
	"abidata/internal/ids"
)

const wsCols = "w.id, w.name, w.status, w.quota_json, w.created_at, w.created_by"

func scanWorkspace(r scanner, extra ...any) (Workspace, error) {
	var w Workspace
	var id, status, quota, created string
	dest := append([]any{&id, &w.Name, &status, &quota, &created, &w.CreatedBy}, extra...)
	if err := r.Scan(dest...); err != nil {
		return Workspace{}, err
	}
	w.ID = ids.WorkspaceID(id)
	w.Status = WorkspaceStatus(status)
	if err := json.Unmarshal([]byte(quota), &w.Quota); err != nil {
		return Workspace{}, fmt.Errorf("admin: bad quota_json for workspace %s: %w", id, err)
	}
	var err error
	w.CreatedAt, err = parseTime(created)
	return w, err
}

func (s *Store) CreateWorkspace(ctx context.Context, w Workspace, owner string) error {
	q, err := json.Marshal(w.Quota)
	if err != nil {
		return err
	}
	status := w.Status
	if status == "" {
		status = StatusActive
	}
	return s.Write(ctx, func(tx dbx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO _adm_workspaces (id, name, status, quota_json, created_at, created_by)
			VALUES (?,?,?,?,?,?)`, string(w.ID), w.Name, string(status), string(q), w.CreatedAt, w.CreatedBy); err != nil {
			return wrapWrite(err, "create workspace")
		}
		_, err := tx.Exec(ctx, `INSERT INTO _adm_memberships (workspace_id, user_id, role) VALUES (?,?,?)`,
			string(w.ID), owner, string(RoleOwner))
		return wrapWrite(err, "create owner membership")
	})
}

// DropWorkspace deletes a workspace row outright (with its memberships). Only
// for a workspace whose creation failed before it was ever used.
func (s *Store) DropWorkspace(ctx context.Context, id ids.WorkspaceID) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM _adm_workspaces WHERE id = ?`, string(id))
		return wrapWrite(err, "drop workspace")
	})
}

func (s *Store) GetWorkspace(ctx context.Context, id ids.WorkspaceID) (Workspace, error) {
	var w Workspace
	err := s.Read(ctx, func(c dbx.Querier) error {
		var err error
		w, err = scanWorkspace(c.QueryRow(ctx, `SELECT `+wsCols+` FROM _adm_workspaces w WHERE w.id = ?`, string(id)))
		return err
	})
	if err != nil {
		return Workspace{}, notFoundIfNoRows(err, "workspace")
	}
	return w, nil
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var out []Workspace
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT `+wsCols+` FROM _adm_workspaces w WHERE w.status <> 'deleted'
			ORDER BY w.name COLLATE NOCASE, w.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w, err := scanWorkspace(rows)
			if err != nil {
				return err
			}
			out = append(out, w)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) ListWorkspacesForUser(ctx context.Context, userID string) ([]WorkspaceAccess, error) {
	var out []WorkspaceAccess
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT `+wsCols+`, m.role FROM _adm_workspaces w
			JOIN _adm_memberships m ON m.workspace_id = w.id
			WHERE m.user_id = ? AND w.status <> 'deleted'
			ORDER BY w.name COLLATE NOCASE, w.id`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var role string
			w, err := scanWorkspace(rows, &role)
			if err != nil {
				return err
			}
			out = append(out, WorkspaceAccess{Workspace: w, Role: Role(role)})
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) UpdateWorkspace(ctx context.Context, id ids.WorkspaceID, p WorkspacePatch) error {
	var name, status, quota any
	if p.Name != nil {
		name = *p.Name
	}
	if p.Status != nil {
		status = string(*p.Status)
	}
	if p.Quota != nil {
		b, err := json.Marshal(*p.Quota)
		if err != nil {
			return err
		}
		quota = string(b)
	}
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, `UPDATE _adm_workspaces SET
			name = COALESCE(:name, name),
			status = COALESCE(:status, status),
			quota_json = COALESCE(:quota, quota_json)
			WHERE id = :id`,
			dbx.Named{Name: "name", Value: name}, dbx.Named{Name: "status", Value: status},
			dbx.Named{Name: "quota", Value: quota}, dbx.Named{Name: "id", Value: string(id)})
		if err != nil {
			return wrapWrite(err, "update workspace")
		}
		return requireChanged(res, "update workspace")
	})
}

func (s *Store) GetRole(ctx context.Context, ws ids.WorkspaceID, userID string) (Role, error) {
	var role string
	err := s.Read(ctx, func(c dbx.Querier) error {
		return c.QueryRow(ctx, `SELECT role FROM _adm_memberships WHERE workspace_id = ? AND user_id = ?`,
			string(ws), userID).Scan(&role)
	})
	if err != nil {
		return "", notFoundIfNoRows(err, "membership")
	}
	return Role(role), nil
}

func (s *Store) SetMembership(ctx context.Context, m Membership) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO _adm_memberships (workspace_id, user_id, role) VALUES (?,?,?)
			ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = excluded.role`,
			string(m.WorkspaceID), m.UserID, string(m.Role))
		return err
	})
}

func (s *Store) DeleteMembership(ctx context.Context, ws ids.WorkspaceID, userID string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, `DELETE FROM _adm_memberships WHERE workspace_id = ? AND user_id = ?`, string(ws), userID)
		if err != nil {
			return err
		}
		return requireChanged(res, "delete membership")
	})
}

func (s *Store) ListMembers(ctx context.Context, ws ids.WorkspaceID) ([]Member, error) {
	var out []Member
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT u.id, u.email, u.display_name, m.role FROM _adm_memberships m
			JOIN _adm_users u ON u.id = m.user_id WHERE m.workspace_id = ?
			ORDER BY u.email COLLATE NOCASE, u.id`, string(ws))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			var role string
			if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &role); err != nil {
				return err
			}
			m.Role = Role(role)
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) CountOwners(ctx context.Context, ws ids.WorkspaceID) (int, error) {
	var n int
	err := s.Read(ctx, func(c dbx.Querier) error {
		return c.QueryRow(ctx, `SELECT count(*) FROM _adm_memberships WHERE workspace_id = ? AND role = 'owner'`,
			string(ws)).Scan(&n)
	})
	return n, err
}

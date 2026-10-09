package admin

import (
	"context"
	"errors"

	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// ErrLastOwner is returned when a change would leave a workspace without an owner.
var ErrLastOwner = errors.New("admin: a workspace must keep at least one owner")

// ChangeMembership sets userID's role in ws (role != nil) or removes the
// membership (role == nil), refusing with ErrLastOwner if the workspace would be
// left without an owner. The check and the change share one transaction, so two
// owners demoting each other concurrently cannot both succeed.
func (s *Store) ChangeMembership(ctx context.Context, ws ids.WorkspaceID, userID string, role *Role) error {
	return s.Write(ctx, func(tx *sqlitex.Tx) error {
		var cur string
		err := tx.QueryRow(ctx, `SELECT role FROM _adm_memberships WHERE workspace_id = ? AND user_id = ?`, string(ws), userID).Scan(&cur)
		if sqlitex.IsNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if Role(cur) == RoleOwner && (role == nil || *role != RoleOwner) {
			var owners int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM _adm_memberships WHERE workspace_id = ? AND role = 'owner'`, string(ws)).Scan(&owners); err != nil {
				return err
			}
			if owners <= 1 {
				return ErrLastOwner
			}
		}
		if role == nil {
			_, err = tx.Exec(ctx, `DELETE FROM _adm_memberships WHERE workspace_id = ? AND user_id = ?`, string(ws), userID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE _adm_memberships SET role = ? WHERE workspace_id = ? AND user_id = ?`, string(*role), string(ws), userID)
		}
		return err
	})
}

package admin

import (
	"context"
	"errors"

	"abidata/internal/dbx"
	"abidata/internal/ids"
)

// ErrLastOwner is returned when a change would leave a workspace without an owner.
var ErrLastOwner = errors.New("admin: a workspace must keep at least one owner")

// ErrProtected is returned for a change a system workspace (a user's blog)
// does not allow: sharing, role changes, transfer or deletion.
var ErrProtected = errors.New("admin: protected system workspace")

// ChangeMembership sets userID's role in ws (role != nil) or removes the
// membership (role == nil), refusing with ErrLastOwner if the workspace would be
// left without an owner. The check and the change share one transaction, so two
// owners demoting each other concurrently cannot both succeed. A blog
// workspace's membership is pinned (ErrProtected).
func (s *Store) ChangeMembership(ctx context.Context, ws ids.WorkspaceID, userID string, role *Role) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		if err := refuseSystem(ctx, tx, ws); err != nil {
			return err
		}
		var cur string
		err := tx.QueryRow(ctx, sqlChangeMembership.SQL, string(ws), userID).Scan(&cur)
		if dbx.IsNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if Role(cur) == RoleOwner && (role == nil || *role != RoleOwner) {
			var owners int
			if err := tx.QueryRow(ctx, sqlChangeMembership2.SQL, string(ws)).Scan(&owners); err != nil {
				return err
			}
			if owners <= 1 {
				return ErrLastOwner
			}
		}
		if role == nil {
			_, err = tx.Exec(ctx, sqlChangeMembership3.SQL, string(ws), userID)
		} else {
			_, err = tx.Exec(ctx, sqlChangeMembership4.SQL, string(*role), string(ws), userID)
		}
		return err
	})
}

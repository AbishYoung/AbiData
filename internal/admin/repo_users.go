package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/dbx"
	"abidata/internal/pgdb"
)

var _ Repository = (*Store)(nil)

func isUnique(err error) bool {
	var se *sqlite3.Error
	if errors.As(err, &se) {
		c := se.ExtendedCode()
		return c == sqlite3.CONSTRAINT_UNIQUE || c == sqlite3.CONSTRAINT_PRIMARYKEY
	}
	return pgdb.IsUniqueViolation(err)
}

func wrapWrite(err error, what string) error {
	if isUnique(err) {
		return fmt.Errorf("%s: %w", what, ErrConflict)
	}
	return err
}

func notFoundIfNoRows(err error, what string) error {
	if dbx.IsNoRows(err) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return err
}

func requireChanged(res dbx.Result, what string) error {
	if res.Changes == 0 {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return nil
}

func parseTime(s string) (time.Time, error) {
	t, err := dbx.ParseTime(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("admin: bad timestamp %q: %w", s, err)
	}
	return t, nil
}

const userCols = "id, email, display_name, password_hash, is_platform_admin, disabled, created_at, last_login_at, username, username_changed_at"

type scanner interface{ Scan(dest ...any) error }

func scanUser(r scanner) (User, error) {
	var u User
	var created string
	var last, username, renamed *string
	if err := r.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsPlatformAdmin, &u.Disabled, &created, &last,
		&username, &renamed); err != nil {
		return User{}, err
	}
	if username != nil {
		u.Username = *username
	}
	if renamed != nil {
		t, err := parseTime(*renamed)
		if err != nil {
			return User{}, err
		}
		u.UsernameChangedAt = &t
	}
	var err error
	if u.CreatedAt, err = parseTime(created); err != nil {
		return User{}, err
	}
	if last != nil {
		t, err := parseTime(*last)
		if err != nil {
			return User{}, err
		}
		u.LastLoginAt = &t
	}
	return u, nil
}

func (s *Store) CreateUser(ctx context.Context, u User) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		var last *string
		if u.LastLoginAt != nil {
			v := dbx.FormatTime(*u.LastLoginAt)
			last = &v
		}
		var username *string
		if u.Username != "" {
			username = &u.Username
		}
		_, err := tx.Exec(ctx, `INSERT INTO _adm_users (`+userCols+`) VALUES (?,?,?,?,?,?,?,?,?,NULL)`,
			u.ID, u.Email, u.DisplayName, u.PasswordHash, u.IsPlatformAdmin, u.Disabled, u.CreatedAt, last, username)
		return wrapWrite(err, "create user")
	})
}

func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	var u User
	err := s.Read(ctx, func(c dbx.Querier) error {
		var err error
		u, err = scanUser(c.QueryRow(ctx, `SELECT `+userCols+` FROM _adm_users WHERE id = ?`, id))
		return err
	})
	if err != nil {
		return User{}, notFoundIfNoRows(err, "user")
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.Read(ctx, func(c dbx.Querier) error {
		var err error
		u, err = scanUser(c.QueryRow(ctx, `SELECT `+userCols+` FROM _adm_users WHERE email = ?`, email))
		return err
	})
	if err != nil {
		return User{}, notFoundIfNoRows(err, "user")
	}
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	var out []User
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT `+userCols+` FROM _adm_users ORDER BY email, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u, err := scanUser(rows)
			if err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.Read(ctx, func(c dbx.Querier) error {
		return c.QueryRow(ctx, sqlCountUsers.SQL).Scan(&n)
	})
	return n, err
}

func (s *Store) UpdateUser(ctx context.Context, id string, p UserPatch) error {
	var dn, admin, disabled any
	if p.DisplayName != nil {
		dn = *p.DisplayName
	}
	if p.IsPlatformAdmin != nil {
		admin = *p.IsPlatformAdmin
	}
	if p.Disabled != nil {
		disabled = *p.Disabled
	}
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlUpdateUser.SQL,
			dbx.Named{Name: "dn", Value: dn}, dbx.Named{Name: "admin", Value: admin},
			dbx.Named{Name: "disabled", Value: disabled}, dbx.Named{Name: "id", Value: id})
		if err != nil {
			return err
		}
		return requireChanged(res, "update user")
	})
}

func (s *Store) SetPasswordHash(ctx context.Context, id, hash string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlSetPasswordHash.SQL, hash, id)
		if err != nil {
			return err
		}
		return requireChanged(res, "set password")
	})
}

func (s *Store) TouchLogin(ctx context.Context, id string, at time.Time) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlTouchLogin.SQL, at, id)
		if err != nil {
			return err
		}
		return requireChanged(res, "touch login")
	})
}

// DeleteUser removes a user outright. Only for undoing a creation whose
// follow-up (the user's blog) failed; there is no user deletion otherwise.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlDeleteUser.SQL, id)
		return err
	})
}

// GetUserByUsername finds a user by current username (case-insensitive).
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.Read(ctx, func(c dbx.Querier) error {
		var err error
		u, err = scanUser(c.QueryRow(ctx, `SELECT `+userCols+` FROM _adm_users WHERE username = ?`, username))
		return err
	})
	if err != nil {
		return User{}, notFoundIfNoRows(err, "user")
	}
	return u, nil
}

// ListUsersWithoutUsername lists users created before usernames, oldest first.
func (s *Store) ListUsersWithoutUsername(ctx context.Context) ([]User, error) {
	var out []User
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT `+userCols+` FROM _adm_users WHERE username IS NULL ORDER BY created_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u, err := scanUser(rows)
			if err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}

// UsernameOwner returns the id of the user holding name: as a current
// username, or as an old one still reserved at now. ErrNotFound if free.
func (s *Store) UsernameOwner(ctx context.Context, name string, now time.Time) (string, error) {
	var id string
	err := s.Read(ctx, func(c dbx.Querier) error {
		return c.QueryRow(ctx, sqlUsernameOwner.SQL, name, now).Scan(&id)
	})
	if err != nil {
		return "", notFoundIfNoRows(err, "username")
	}
	return id, nil
}

// ResolveOldUsername returns the current username of the user who gave up
// old, while it is still reserved. ErrNotFound if old is not a held former name.
func (s *Store) ResolveOldUsername(ctx context.Context, old string, now time.Time) (string, error) {
	var name string
	err := s.Read(ctx, func(c dbx.Querier) error {
		return c.QueryRow(ctx, sqlResolveOldUsername.SQL, old, now).Scan(&name)
	})
	if err != nil {
		return "", notFoundIfNoRows(err, "username")
	}
	return name, nil
}

// InitUsername sets the username of a user that has none (the boot backfill).
func (s *Store) InitUsername(ctx context.Context, id, name string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlInitUsername.SQL, name, id)
		if err != nil {
			return wrapWrite(err, "set username")
		}
		return requireChanged(res, "set username")
	})
}

// SetUsername renames a user. The previous name stays in the history,
// reserved for the user until holdUntil; taking back one's own old name
// removes it from the history.
func (s *Store) SetUsername(ctx context.Context, id, name string, now, holdUntil time.Time) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		var old *string
		if err := tx.QueryRow(ctx, sqlSetUsername.SQL, id).Scan(&old); err != nil {
			return notFoundIfNoRows(err, "user")
		}
		if _, err := tx.Exec(ctx, sqlSetUsername2.SQL, name, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sqlSetUsername3.SQL, name, now, id); err != nil {
			return wrapWrite(err, "set username")
		}
		if old != nil && *old != "" {
			if _, err := tx.Exec(ctx, sqlSetUsername4.SQL, *old, id, now, holdUntil); err != nil {
				return err
			}
		}
		return nil
	})
}

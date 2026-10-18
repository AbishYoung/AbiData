package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/dbx"
)

var _ Repository = (*Store)(nil)

func isUnique(err error) bool {
	var se *sqlite3.Error
	if errors.As(err, &se) {
		c := se.ExtendedCode()
		return c == sqlite3.CONSTRAINT_UNIQUE || c == sqlite3.CONSTRAINT_PRIMARYKEY
	}
	return false
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

const userCols = "id, email, display_name, password_hash, is_platform_admin, disabled, created_at, last_login_at"

type scanner interface{ Scan(dest ...any) error }

func scanUser(r scanner) (User, error) {
	var u User
	var created string
	var last *string
	if err := r.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsPlatformAdmin, &u.Disabled, &created, &last); err != nil {
		return User{}, err
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
		_, err := tx.Exec(ctx, `INSERT INTO _adm_users (`+userCols+`) VALUES (?,?,?,?,?,?,?,?)`,
			u.ID, u.Email, u.DisplayName, u.PasswordHash, u.IsPlatformAdmin, u.Disabled, u.CreatedAt, last)
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
		rows, err := c.Query(ctx, `SELECT `+userCols+` FROM _adm_users ORDER BY email COLLATE NOCASE, id`)
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

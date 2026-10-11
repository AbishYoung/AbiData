package admin

import (
	"context"
	"errors"
	"time"

	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// ErrTooManyKeys is returned when a workspace already has the maximum number of
// active API keys.
var ErrTooManyKeys = errors.New("admin: too many active API keys")

// MaxActiveAPIKeys caps the active (not revoked, not expired) keys per workspace.
const MaxActiveAPIKeys = 25

// apiKeyRetention is how long revoked or expired keys stay listed before a new
// key creation purges them.
const apiKeyRetention = 90 * 24 * time.Hour

// APIKey is a row of _adm_api_keys. SecretHash is HMAC-SHA256 of the secret;
// it is never exposed outside the auth package.
type APIKey struct {
	ID            string
	WorkspaceID   ids.WorkspaceID
	Name          string
	Role          Role // viewer, editor or builder; never owner
	SecretHash    []byte
	CreatedBy     string
	CreatedByName string // joined from _adm_users on reads; empty if the user is gone
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
}

// Active reports whether the key can authenticate at now.
func (k APIKey) Active(now time.Time) bool {
	if k.RevokedAt != nil {
		return false
	}
	return k.ExpiresAt == nil || now.Before(*k.ExpiresAt)
}

const apiKeyCols = `k.id, k.workspace_id, k.name, k.role, k.secret_hash, k.created_by, COALESCE(u.display_name, ''), k.created_at, k.expires_at, k.last_used_at, k.revoked_at`
const apiKeyFrom = ` FROM _adm_api_keys k LEFT JOIN _adm_users u ON u.id = k.created_by `

func scanAPIKey(r scanner) (APIKey, error) {
	var k APIKey
	var ws, role, created string
	var exp, used, rev *string
	if err := r.Scan(&k.ID, &ws, &k.Name, &role, &k.SecretHash, &k.CreatedBy, &k.CreatedByName, &created, &exp, &used, &rev); err != nil {
		return APIKey{}, err
	}
	k.WorkspaceID, k.Role = ids.WorkspaceID(ws), Role(role)
	var err error
	if k.CreatedAt, err = parseTime(created); err != nil {
		return APIKey{}, err
	}
	for _, p := range []struct {
		in  *string
		out **time.Time
	}{{exp, &k.ExpiresAt}, {used, &k.LastUsedAt}, {rev, &k.RevokedAt}} {
		if p.in == nil {
			continue
		}
		t, err := parseTime(*p.in)
		if err != nil {
			return APIKey{}, err
		}
		*p.out = &t
	}
	return k, nil
}

// CreateAPIKey inserts k, refusing with ErrTooManyKeys when the workspace has
// MaxActiveAPIKeys active keys. The count and the insert share one transaction.
// Keys revoked or expired more than 90 days ago are purged first.
func (s *Store) CreateAPIKey(ctx context.Context, k APIKey) error {
	return s.Write(ctx, func(tx *sqlitex.Tx) error {
		now := time.Now()
		cutoff := sqlitex.FormatTime(now.Add(-apiKeyRetention))
		if _, err := tx.Exec(ctx, `DELETE FROM _adm_api_keys WHERE workspace_id = ?1 AND
			((revoked_at IS NOT NULL AND revoked_at < ?2) OR (expires_at IS NOT NULL AND expires_at < ?2))`,
			string(k.WorkspaceID), cutoff); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM _adm_api_keys WHERE workspace_id = ?1 AND revoked_at IS NULL
			AND (expires_at IS NULL OR expires_at > ?2)`, string(k.WorkspaceID), sqlitex.FormatTime(now)).Scan(&n); err != nil {
			return err
		}
		if n >= MaxActiveAPIKeys {
			return ErrTooManyKeys
		}
		var exp any
		if k.ExpiresAt != nil {
			exp = sqlitex.FormatTime(*k.ExpiresAt)
		}
		_, err := tx.Exec(ctx, `INSERT INTO _adm_api_keys (id, workspace_id, name, role, secret_hash, created_by, created_at, expires_at)
			VALUES (?,?,?,?,?,?,?,?)`, k.ID, string(k.WorkspaceID), k.Name, string(k.Role), k.SecretHash, k.CreatedBy,
			sqlitex.FormatTime(k.CreatedAt), exp)
		return wrapWrite(err, "create api key")
	})
}

// GetAPIKey returns one key (including its hash) by id.
func (s *Store) GetAPIKey(ctx context.Context, id string) (APIKey, error) {
	var out APIKey
	err := s.Read(ctx, func(c *sqlitex.Conn) error {
		var err error
		out, err = scanAPIKey(c.QueryRow(ctx, `SELECT `+apiKeyCols+apiKeyFrom+`WHERE k.id = ?`, id))
		return err
	})
	if err != nil {
		return APIKey{}, notFoundIfNoRows(err, "api key")
	}
	return out, nil
}

// ListAPIKeys returns a workspace's keys, newest first; "" lists every
// workspace's keys. The result carries the hash: callers must not expose it.
func (s *Store) ListAPIKeys(ctx context.Context, ws ids.WorkspaceID) ([]APIKey, error) {
	out := []APIKey{}
	err := s.Read(ctx, func(c *sqlitex.Conn) error {
		rows, err := c.Query(ctx, `SELECT `+apiKeyCols+apiKeyFrom+`WHERE ?1 = '' OR k.workspace_id = ?1
			ORDER BY k.created_at DESC, k.id DESC LIMIT 1000`, string(ws))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			k, err := scanAPIKey(rows)
			if err != nil {
				return err
			}
			out = append(out, k)
		}
		return rows.Err()
	})
	return out, err
}

// RevokeAPIKey marks a key revoked. Revoking an already revoked key succeeds
// and keeps the first revocation time; an unknown id is ErrNotFound.
func (s *Store) RevokeAPIKey(ctx context.Context, id string, at time.Time) error {
	return s.Write(ctx, func(tx *sqlitex.Tx) error {
		res, err := tx.Exec(ctx, `UPDATE _adm_api_keys SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, sqlitex.FormatTime(at), id)
		if err != nil {
			return err
		}
		return requireChanged(res, "api key")
	})
}

// TouchAPIKey records the last use of a key.
func (s *Store) TouchAPIKey(ctx context.Context, id string, at time.Time) error {
	return s.Write(ctx, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE _adm_api_keys SET last_used_at = ? WHERE id = ?`, sqlitex.FormatTime(at), id)
		return err
	})
}

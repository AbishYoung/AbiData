package admin

import (
	"context"
	"encoding/json"
	"time"

	"abidata/internal/sqlitex"
)

const defaultAuditLimit = 100

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) Audit(ctx context.Context, e AuditEntry) error {
	var details any
	if len(e.Details) > 0 {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return err
		}
		details = string(b)
	}
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	return s.Write(ctx, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO _adm_audit (at, user_id, action, object_kind, object_id, details)
			VALUES (?,?,?,?,?,?)`, at, nullIfEmpty(e.UserID), e.Action, nullIfEmpty(e.ObjectKind),
			nullIfEmpty(e.ObjectID), details)
		return err
	})
}

// ListAudit returns up to limit entries, newest first; limit <= 0 means 100.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = defaultAuditLimit
	}
	var out []AuditEntry
	err := s.Read(ctx, func(c *sqlitex.Conn) error {
		rows, err := c.Query(ctx, `SELECT at, user_id, action, object_kind, object_id, details
			FROM _adm_audit ORDER BY at DESC, id DESC LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var at, action string
			var uid, kind, oid, details *string
			if err := rows.Scan(&at, &uid, &action, &kind, &oid, &details); err != nil {
				return err
			}
			e := AuditEntry{Action: action}
			if e.At, err = parseTime(at); err != nil {
				return err
			}
			if uid != nil {
				e.UserID = *uid
			}
			if kind != nil {
				e.ObjectKind = *kind
			}
			if oid != nil {
				e.ObjectID = *oid
			}
			if details != nil {
				if err := json.Unmarshal([]byte(*details), &e.Details); err != nil {
					return err
				}
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"abidata/internal/dbx"
)

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.Read(ctx, func(c dbx.Querier) error {
		return c.QueryRow(ctx, sqlGetSetting.SQL, key).Scan(&v)
	})
	if err != nil {
		return "", notFoundIfNoRows(err, "setting "+key)
	}
	return v, nil
}

func (s *Store) PutSetting(ctx context.Context, key, value string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlPutSetting.SQL, key, value)
		return err
	})
}

func (s *Store) ListSettings(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, sqlListSettings.SQL)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				return err
			}
			out[k] = v
		}
		return rows.Err()
	})
	return out, err
}

// settingBackend records the backend an install was created on (ADR 0012 §1).
const settingBackend = "backend"

// ErrBackendMismatch is returned when an admin store was created on another
// backend than the one opening it.
var ErrBackendMismatch = errors.New("admin: the store belongs to another backend")

// lockBackend records backend on first open and refuses a store that records
// another one: an install never changes backend silently, only through
// abidata migrate-backend (ADR 0012 §9).
func (s *Store) lockBackend(ctx context.Context, backend string) error {
	raw, err := s.GetSetting(ctx, settingBackend)
	if errors.Is(err, ErrNotFound) {
		b, _ := json.Marshal(backend) // settings hold JSON text
		return s.PutSetting(ctx, settingBackend, string(b))
	}
	if err != nil {
		return err
	}
	var got string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		got = raw
	}
	if got != backend {
		return fmt.Errorf("%w: %s was created on %s, not %s; move it with abidata migrate-backend --to %s", ErrBackendMismatch, s.path, got, backend, backend)
	}
	return nil
}

package admin

import (
	"context"

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

package admin

import (
	"context"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/ids"
)

// Backup kinds (CHECK constraint in _adm_backups).
const (
	BackupManual       = "manual"
	BackupScheduled    = "scheduled"
	BackupRestorePoint = "restore_point"
	BackupPreRestore   = "pre_restore"
)

// Backup is a row of _adm_backups.
type Backup struct {
	ID           string
	WorkspaceID  ids.WorkspaceID
	Kind         string
	Path         string // the .db.gz file
	RawBytes     int64
	GzBytes      int64
	SHA256Gz     string
	CreatedAt    time.Time
	CreatedBy    string
	ManifestJSON string
}

const backupCols = `id, workspace_id, kind, path, raw_bytes, gz_bytes, sha256_gz, created_at, created_by, manifest_json`

func scanBackup(rows dbx.Rows) (Backup, error) {
	var b Backup
	var ws, created string
	if err := rows.Scan(&b.ID, &ws, &b.Kind, &b.Path, &b.RawBytes, &b.GzBytes, &b.SHA256Gz, &created, &b.CreatedBy, &b.ManifestJSON); err != nil {
		return Backup{}, err
	}
	b.WorkspaceID = ids.WorkspaceID(ws)
	b.CreatedAt, _ = dbx.ParseTime(created)
	return b, nil
}

// InsertBackup records a completed backup.
func (s *Store) InsertBackup(ctx context.Context, b Backup) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO _adm_backups (`+backupCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			b.ID, string(b.WorkspaceID), b.Kind, b.Path, b.RawBytes, b.GzBytes, b.SHA256Gz, dbx.FormatTime(b.CreatedAt), b.CreatedBy, b.ManifestJSON)
		return err
	})
}

// GetBackup returns one backup.
func (s *Store) GetBackup(ctx context.Context, id string) (Backup, error) {
	var out Backup
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT `+backupCols+` FROM _adm_backups WHERE id = ?`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return dbx.ErrNoRows
		}
		out, err = scanBackup(rows)
		return err
	})
	if err != nil {
		return Backup{}, notFoundIfNoRows(err, "backup "+id)
	}
	return out, nil
}

// ListBackups returns a workspace's backups, newest first ("" lists all).
func (s *Store) ListBackups(ctx context.Context, ws ids.WorkspaceID) ([]Backup, error) {
	out := []Backup{}
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, `SELECT `+backupCols+` FROM _adm_backups WHERE ?1 = '' OR workspace_id = ?1 ORDER BY created_at DESC, id DESC`, string(ws))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			b, err := scanBackup(rows)
			if err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// DeleteBackup removes a backup record (the caller removes the files).
func (s *Store) DeleteBackup(ctx context.Context, id string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, `DELETE FROM _adm_backups WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return requireChanged(res, "backup "+id)
	})
}

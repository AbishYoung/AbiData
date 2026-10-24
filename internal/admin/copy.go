package admin

import (
	"context"
	"fmt"
	"strings"

	"abidata/internal/dbx"
	"abidata/internal/ids"
)

// copyTables are the admin tables abidata migrate-backend carries to the
// other backend (ADR 0012 §9), parents before children. Sessions and login
// attempts are left behind, so everyone signs in again; _adm_schema is each
// store's own. TestCopyTablesComplete keeps the list in step with the
// migrations.
var copyTables = []copyTable{
	{name: "_adm_users"},
	{name: "_adm_workspaces"},
	{name: "_adm_memberships"},
	{name: "_adm_job_index"},
	{name: "_adm_backups"},
	{name: "_adm_settings"},
	{name: "_adm_audit", identity: true},
	{name: "_adm_api_keys"},
	{name: "_adm_apps"},
	{name: "_adm_app_items"},
	{name: "_adm_username_history"},
	{name: "_adm_blogs"},
	{name: "_adm_blog_takedowns"},
	{name: "_adm_blog_reports"},
	{name: "_adm_message_threads"},
	{name: "_adm_messages", selfRef: "parent_id"},
	{name: "_adm_message_participants"},
}

// notCopied are the admin tables that stay behind on purpose.
var notCopied = []string{"_adm_sessions", "_adm_login_attempts", "_adm_schema"}

type copyTable struct {
	name     string
	identity bool   // its id is an identity column on PostgreSQL, whose sequence must move past the copied ids
	selfRef  string // a column referencing the table's own id, set in a second pass
}

// CopyOptions adjusts a copy.
type CopyOptions struct {
	// Rewrite, when set, may replace a value before it is written (for
	// example a backup's path, when the backup files move).
	Rewrite func(table, column string, v any) any
}

// CopyWorkspace is a workspace record the copy carried, so the caller can
// move its data.
type CopyWorkspace struct {
	ID     ids.WorkspaceID
	Status WorkspaceStatus
}

// CopyResult reports what CopyTo wrote.
type CopyResult struct {
	Rows       map[string]int64 // per table
	Workspaces []CopyWorkspace
}

// CopyTo copies every durable admin row of s into dst, which must be a
// freshly created store (of either backend) with no users, in one
// transaction on dst: it fails as a whole or not at all. The backend
// setting stays dst's own. Both stores run the same migrations, so the
// tables have the same columns; values are SQLite's storage forms on both
// (ADR 0012 §4), so they pass through unchanged.
func (s *Store) CopyTo(ctx context.Context, dst *Store, opts CopyOptions) (CopyResult, error) {
	res := CopyResult{Rows: map[string]int64{}}
	n, err := dst.CountUsers(ctx)
	if err != nil {
		return res, err
	}
	if n > 0 {
		return res, fmt.Errorf("admin: copy: the target admin store already has %d users", n)
	}
	err = dst.Write(ctx, func(tx dbx.Tx) error {
		for _, t := range copyTables {
			if err := s.copyTable(ctx, tx, t, dst.pg != nil, opts, &res); err != nil {
				return fmt.Errorf("admin: copy %s: %w", t.name, err)
			}
		}
		return nil
	})
	if err != nil {
		return CopyResult{}, err
	}
	return res, nil
}

func (s *Store) copyTable(ctx context.Context, tx dbx.Tx, t copyTable, toPostgres bool, opts CopyOptions, res *CopyResult) error {
	type link struct{ id, ref any }
	var links []link
	err := s.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, `SELECT * FROM `+t.name)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols := rows.Columns()
		quoted := make([]string, len(cols))
		marks := make([]string, len(cols))
		idCol, statusCol, refCol, keyCol := -1, -1, -1, -1
		for i, c := range cols {
			quoted[i] = `"` + c + `"`
			marks[i] = "?"
			switch c {
			case "id":
				idCol = i
			case "status":
				statusCol = i
			case "key":
				keyCol = i
			}
			if c == t.selfRef {
				refCol = i
			}
		}
		insert := `INSERT INTO ` + t.name + ` (` + strings.Join(quoted, ", ") + `) VALUES (` + strings.Join(marks, ", ") + `)`
		for rows.Next() {
			vals := rows.Values()
			if vals == nil {
				return fmt.Errorf("reading a row failed")
			}
			for i, v := range vals {
				v = plainValue(v)
				if opts.Rewrite != nil {
					v = opts.Rewrite(t.name, cols[i], v)
				}
				vals[i] = v
			}
			if t.name == "_adm_settings" && keyCol >= 0 && vals[keyCol] == settingBackend {
				continue // dst records its own backend
			}
			if refCol >= 0 && vals[refCol] != nil {
				links = append(links, link{vals[idCol], vals[refCol]})
				vals[refCol] = nil
			}
			if _, err := tx.Exec(ctx, insert, vals...); err != nil {
				return err
			}
			res.Rows[t.name]++
			if t.name == "_adm_workspaces" && idCol >= 0 && statusCol >= 0 {
				id, _ := vals[idCol].(string)
				st, _ := vals[statusCol].(string)
				res.Workspaces = append(res.Workspaces, CopyWorkspace{ID: ids.WorkspaceID(id), Status: WorkspaceStatus(st)})
			}
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.Exec(ctx, `UPDATE `+t.name+` SET "`+t.selfRef+`" = ? WHERE id = ?`, l.ref, l.id); err != nil {
			return err
		}
	}
	// An identity's sequence does not follow explicitly inserted ids.
	if t.identity && toPostgres {
		if _, err := tx.Exec(ctx, sqlCopyAuditIdentity.SQL); err != nil {
			return err
		}
	}
	return nil
}

// plainValue narrows a driver's integer types to int64.
func plainValue(v any) any {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case int8:
		return int64(x)
	}
	return v
}

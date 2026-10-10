package records

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"abidata/internal/fault"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// HistoryEntry is one recorded change of a row (tables with track_history).
// Before and After hold field values by column id; a field since removed from
// the table still appears under its old id.
type HistoryEntry struct {
	ID      int64          `json:"id"`
	Version int64          `json:"row_version"`
	Op      string         `json:"op" enum:"insert,update,delete"`
	UserID  string         `json:"user_id,omitempty"`
	At      time.Time      `json:"at"`
	Before  map[string]any `json:"before,omitempty"`
	After   map[string]any `json:"after,omitempty"`
}

// MaxHistoryPage is the most entries History returns at once.
const MaxHistoryPage = 100

// History lists a row's recorded changes, newest first: up to limit entries
// with an id below beforeID (0 = from the newest). more reports whether older
// entries exist. A deleted row's history stays readable.
func (s *Service) History(ctx context.Context, l *workspace.Lease, tableID string, rowID, beforeID int64, limit int) (out []HistoryEntry, more bool, err error) {
	_, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 || limit > MaxHistoryPage {
		limit = MaxHistoryPage
	}
	if beforeID <= 0 {
		beforeID = 1<<63 - 1
	}
	out = []HistoryEntry{}
	err = l.Read(ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(ctx, `SELECT id, row_version, op, user_id, at, before, after FROM _sys_row_history
			WHERE table_id = ? AND row_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, t.ID, rowID, beforeID, limit+1)
		if err != nil {
			return fault.Classify(err)
		}
		defer rows.Close()
		for rows.Next() {
			var e HistoryEntry
			var user, before, after *string
			var at string
			if err := rows.Scan(&e.ID, &e.Version, &e.Op, &user, &at, &before, &after); err != nil {
				return err
			}
			if len(out) == limit {
				more = true
				break
			}
			if user != nil {
				e.UserID = *user
			}
			if e.At, err = sqlitex.ParseTime(at); err != nil {
				return err
			}
			for _, p := range []struct {
				src *string
				dst *map[string]any
			}{{before, &e.Before}, {after, &e.After}} {
				if p.src == nil {
					continue
				}
				d := json.NewDecoder(strings.NewReader(*p.src))
				d.UseNumber()
				if err := d.Decode(p.dst); err != nil {
					return err
				}
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, more, err
}

package ingest

import (
	"context"
	"fmt"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/records"
	"abidata/internal/sqlgen"
	"abidata/internal/workspace"
)

// lookupBatch bounds the ids in one lookup query, well under SQLite's variable limit.
const lookupBatch = 500

// lookupCacheMax bounds the display values remembered per lookup column; the
// cache is dropped when it fills, so a table with millions of distinct parents
// stays within memory.
const lookupCacheMax = 100_000

// idSuffix names the adjacent column that keeps a resolved lookup's id.
const idSuffix = " [ID]"

// resolveLookups reports whether the export shows lookup columns as the
// parent's display text. The default is on for the spreadsheet formats and off
// for JSON, which keeps ids (the API form).
func (r ExportRequest) resolveLookups() bool {
	if r.ResolveLookups != nil {
		return *r.ResolveLookups
	}
	return r.Format == "csv" || r.Format == "xlsx"
}

// lookupTarget is one lookup column that can be resolved.
type lookupTarget struct {
	pos    int // position in a row (ID is 0)
	parent *catalog.Table
	disp   catalog.Column
	cache  map[int64]string
}

// lookupResolver turns lookup cells (parent ids) into the parent's display
// column text, in batches, like fileNamer does for attachments (ADR 0020). A
// nil *lookupResolver does nothing.
type lookupResolver struct {
	targets []*lookupTarget
	byPos   map[int]*lookupTarget
	ids     bool // keep the id in an adjacent column
}

// newLookupResolver returns nil when the export resolves no column.
func newLookupResolver(sch *catalog.Schema, cols []catalog.Column, ids bool) *lookupResolver {
	r := &lookupResolver{byPos: map[int]*lookupTarget{}, ids: ids}
	for i, c := range cols {
		if c.Type != catalog.Lookup || c.LookupRelationshipID == "" {
			continue
		}
		rel, ok := sch.Relationship(c.LookupRelationshipID)
		if !ok {
			continue
		}
		parent, ok := sch.Table(rel.ParentTableID)
		if !ok {
			continue
		}
		disp, ok := parent.Column(c.LookupDisplayColumnID)
		if !ok {
			continue
		}
		t := &lookupTarget{pos: i + 1, parent: parent, disp: *disp, cache: map[int64]string{}}
		r.targets = append(r.targets, t)
		r.byPos[t.pos] = t
	}
	if len(r.targets) == 0 {
		return nil
	}
	return r
}

// shape inserts the id columns into the header, kinds and textual flags, and
// marks resolved columns as text. names, kinds and textual start with the ID
// column. The slices are returned, rebuilt when ids are kept.
func (r *lookupResolver) shape(names []string, kinds []colKind, textual []bool) ([]string, []colKind, []bool) {
	if r == nil {
		return names, kinds, textual
	}
	var n []string
	var k []colKind
	var tx []bool
	for i := range names {
		t := r.byPos[i]
		if t != nil {
			kinds[i] = kText
		}
		n, k, tx = append(n, names[i]), append(k, kinds[i]), append(tx, textual[i])
		if t != nil && r.ids {
			n, k, tx = append(n, names[i]+idSuffix), append(k, kGeneral), append(tx, false)
		}
	}
	return n, k, tx
}

// apply replaces the lookup cells of rows with display text and, when ids are
// kept, inserts the id after each. It returns the rows to write.
func (r *lookupResolver) apply(ctx context.Context, l *workspace.Lease, rows [][]any) ([][]any, error) {
	if r == nil {
		return rows, nil
	}
	for _, t := range r.targets {
		if err := t.resolve(ctx, l, rows); err != nil {
			return nil, err
		}
	}
	out := make([][]any, len(rows))
	for j, row := range rows {
		o := make([]any, 0, len(row))
		for i, v := range row {
			t := r.byPos[i]
			if t == nil {
				o = append(o, v)
				continue
			}
			id, ok := idOf(v)
			switch {
			case v == nil:
				o = append(o, nil)
			case !ok:
				o = append(o, fmt.Sprint(v))
			default:
				o = append(o, t.cache[id])
			}
			if r.ids {
				o = append(o, v)
			}
		}
		out[j] = o
	}
	return out, nil
}

// idOf reads a lookup cell: an integer id as the rows API encodes it.
func idOf(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case float64:
		return integralFloat(x)
	}
	return 0, false
}

// resolve loads the display text of the ids in rows that the cache lacks.
func (t *lookupTarget) resolve(ctx context.Context, l *workspace.Lease, rows [][]any) error {
	if len(t.cache) > lookupCacheMax {
		t.cache = map[int64]string{}
	}
	var missing []int64
	seen := map[int64]bool{}
	for _, row := range rows {
		id, ok := idOf(row[t.pos])
		if !ok || seen[id] {
			continue
		}
		if _, hit := t.cache[id]; !hit {
			seen[id] = true
			missing = append(missing, id)
		}
	}
	for start := 0; start < len(missing); start += lookupBatch {
		if err := t.load(ctx, l, missing[start:min(start+lookupBatch, len(missing))]); err != nil {
			return err
		}
	}
	// Cells whose id is not in the cache (a parent deleted without enforcement)
	// keep the id as text, so no information is lost.
	for _, row := range rows {
		if id, ok := idOf(row[t.pos]); ok {
			if _, hit := t.cache[id]; !hit {
				t.cache[id] = fmt.Sprint(id)
			}
		}
	}
	return nil
}

func (t *lookupTarget) load(ctx context.Context, l *workspace.Lease, ids []int64) error {
	list := make([]sqlgen.Expr, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		list[i], args[i] = sqlgen.Arg{}, id
		t.cache[id] = fmt.Sprint(id) // overwritten when the parent row exists
	}
	q, err := sqlgen.Render(l.Dialect(), &sqlgen.Select{
		Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}, {Expr: sqlgen.Col{Name: t.disp.PhysicalName}}},
		From:    sqlgen.Table{Name: t.parent.PhysicalName},
		Where:   sqlgen.In{X: sqlgen.Col{Name: "id"}, List: list},
	})
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	return l.Read(ctx, func(qr dbx.Querier) error {
		rows, err := qr.Query(ctx, q.SQL, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v := rows.Values()
			id, ok := v[0].(int64)
			if !ok {
				continue
			}
			t.cache[id] = displayText(records.EncodeValue(t.disp, catalog.StorageForm(t.disp.Type, v[1])))
		}
		return rows.Err()
	})
}

// displayText renders a parent's display value as the cell text.
func displayText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	return fmt.Sprint(v)
}

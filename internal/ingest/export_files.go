package ingest

import (
	"context"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

// removedFileName stands in for a file that was purged after the row last
// referenced it.
const removedFileName = "(file removed)"

// fileNameBatch bounds the ids in one lookup, well under SQLite's variable limit.
const fileNameBatch = 500

// fileNamer turns attachments cells (arrays of file ids) into the file names,
// separated by "; ", for the spreadsheet formats (ADR 0011). JSON keeps the
// id arrays, which are the API form. A nil *fileNamer does nothing.
type fileNamer struct {
	idx   []int // positions in a row (ID is 0) of attachments columns
	names map[string]string
}

// newFileNamer returns nil when the export has no attachments column or the
// format keeps ids.
func newFileNamer(sk sink, cols []catalog.Column) *fileNamer {
	if _, isJSON := sk.(*jsonSink); isJSON {
		return nil
	}
	var idx []int
	for i, c := range cols {
		if c.Type == catalog.Attachments {
			idx = append(idx, i+1)
		}
	}
	if len(idx) == 0 {
		return nil
	}
	return &fileNamer{idx: idx, names: map[string]string{}}
}

// apply replaces the attachments cells of rows in place.
func (f *fileNamer) apply(ctx context.Context, l *workspace.Lease, rows [][]any) error {
	if f == nil {
		return nil
	}
	var missing []string
	seen := map[string]bool{}
	for _, r := range rows {
		for _, i := range f.idx {
			for _, id := range idsOf(r[i]) {
				if _, ok := f.names[id]; !ok && !seen[id] {
					seen[id] = true
					missing = append(missing, id)
				}
			}
		}
	}
	for start := 0; start < len(missing); start += fileNameBatch {
		chunk := missing[start:min(start+fileNameBatch, len(missing))]
		if err := f.load(ctx, l, chunk); err != nil {
			return err
		}
	}
	for _, r := range rows {
		for _, i := range f.idx {
			list := idsOf(r[i])
			if len(list) == 0 {
				r[i] = nil
				continue
			}
			parts := make([]string, len(list))
			for j, id := range list {
				parts[j] = f.names[id]
			}
			r[i] = strings.Join(parts, "; ")
		}
	}
	return nil
}

func (f *fileNamer) load(ctx context.Context, l *workspace.Lease, ids []string) error {
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
		f.names[id] = removedFileName
	}
	return l.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, `SELECT id, name FROM _sys_files WHERE id IN (`+marks+`)`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			f.names[id] = name
		}
		return rows.Err()
	})
}

// idsOf reads the ids of an exported attachments cell: the API form (a list)
// or the stored form (JSON text).
func idsOf(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return records.AttachmentIDs(v)
}

package records

import (
	"context"
	"encoding/json"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// Attachments cells (ADR 0011) are stored as a JSON array of file ids in TEXT
// and exposed through the API as an array of id strings. An empty list is
// stored as NULL, so a required attachments field needs at least one file.

// decodeAttachments validates an API value for an attachments column: a list of
// canonical file ids, duplicates dropped (first occurrence wins, so order is
// preserved), at most the column's max_files. It returns the stored JSON text,
// or nil for an empty list. Whether the ids exist is checked inside the write
// transaction by checkAttachments.
func decodeAttachments(c catalog.Column, v any) (any, error) {
	var raw []any
	switch x := v.(type) {
	case []any:
		raw = x
	case []string:
		raw = make([]any, len(x))
		for i, s := range x {
			raw[i] = s
		}
	default:
		return nil, badf(c, "must be a list of file ids.")
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		s, ok := e.(string)
		if !ok || !ids.Valid(s) {
			return nil, badf(c, "contains something that is not a file id.")
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	if limit := c.MaxFilesLimit(); len(out) > limit {
		return nil, fault.Newf(fault.BadRequest, "%q can hold at most %d files.", c.DisplayName, limit)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// AttachmentIDs parses a stored attachments cell into its file ids.
func AttachmentIDs(stored any) []string {
	s, ok := stored.(string)
	if !ok || s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// encodeAttachments is the API form of a stored attachments cell: an array of
// ids, or null for none.
func encodeAttachments(v any) any {
	ids := AttachmentIDs(v)
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// checkAttachments verifies, inside the write transaction, that every file id
// written to an attachments column exists in this workspace (a field never
// points anywhere else). The reference triggers re-assert this with a foreign
// key for writes that bypass the row API; this gives the readable error.
func checkAttachments(ctx context.Context, q sqlitex.Querier, cols []catalog.Column, vals []any) error {
	for i, c := range cols {
		if c.Type != catalog.Attachments || vals[i] == nil {
			continue
		}
		list := AttachmentIDs(vals[i])
		if len(list) == 0 {
			continue
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(list)), ",")
		args := make([]any, len(list))
		for j, id := range list {
			args[j] = id
		}
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM _sys_files WHERE id IN (`+marks+`)`, args...).Scan(&n); err != nil {
			return err
		}
		if n != len(list) {
			return &fault.Error{Code: fault.BadRequest, Message: "\"" + c.DisplayName + "\" refers to a file that does not exist in this workspace. Upload it first.",
				Details: map[string]any{"field": c.ID}}
		}
	}
	return nil
}

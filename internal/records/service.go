package records

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Paging and batch limits.
const (
	MaxPage        = 500
	CountCap       = 100000
	MaxBatch       = 1000
	coalesceIDs    = 500
	idempotencyTTL = 24 * time.Hour
)

// Row is a record in API representation. Fields are keyed by column ID; Display
// holds lookup display text keyed by the lookup column's ID.
type Row struct {
	ID      int64             `json:"id"`
	Version int64             `json:"_version"`
	Fields  map[string]any    `json:"fields"`
	Display map[string]string `json:"display,omitempty"`
}

// Publisher receives change-feed events (events.Hub satisfies it).
type Publisher interface {
	Publish(key, typ string, data any)
}

// Service is the row API.
type Service struct {
	Schemas *catalog.Cache
	Events  Publisher
	Now     func() time.Time
}

// Idempotency identifies a retried mutation (D17).
type Idempotency struct {
	Key    string
	UserID string
}

// Actor is the user performing a mutation (for history).
type Actor struct {
	UserID string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Table resolves a table by ID in the current schema.
func (s *Service) Table(ctx context.Context, l *workspace.Lease, tableID string) (*catalog.Schema, *catalog.Table, error) {
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return nil, nil, err
	}
	t, ok := sch.Table(tableID)
	if !ok {
		return nil, nil, fault.E(fault.NotFound)
	}
	return sch, t, nil
}

// ---- reads -----------------------------------------------------------------

// ListQuery selects a page of rows.
type ListQuery struct {
	Filter *Filter
	Sort   []Sort
	Offset int
	Limit  int
}

// selectList renders the column list for t plus lookup display subqueries.
func selectList(sch *catalog.Schema, t *catalog.Table) (string, []catalog.Column, []catalog.Column) {
	cols := []string{`t."id"`, `t."_version"`}
	var data, lookups []catalog.Column
	for _, c := range t.Columns {
		if c.Type == catalog.Autonumber {
			continue
		}
		cols = append(cols, "t."+catalog.Quote(c.PhysicalName))
		data = append(data, c)
	}
	for _, c := range data {
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
		cols = append(cols, fmt.Sprintf(`(SELECT p.%s FROM %s p WHERE p."id" = t.%s)`,
			catalog.Quote(disp.PhysicalName), catalog.Quote(parent.PhysicalName), catalog.Quote(c.PhysicalName)))
		lookups = append(lookups, c)
	}
	return strings.Join(cols, ", "), data, lookups
}

func scanRow(rows *sqlitex.Rows, data, lookups []catalog.Column) (Row, error) {
	vals := rows.Values()
	r := Row{Fields: make(map[string]any, len(data))}
	id, ok1 := vals[0].(int64)
	ver, ok2 := vals[1].(int64)
	if !ok1 || !ok2 {
		return Row{}, errors.New("records: malformed id/_version")
	}
	r.ID, r.Version = id, ver
	for i, c := range data {
		r.Fields[c.ID] = EncodeValue(c, vals[2+i])
	}
	for i, c := range lookups {
		if v := vals[2+len(data)+i]; v != nil {
			if r.Display == nil {
				r.Display = map[string]string{}
			}
			r.Display[c.ID] = fmt.Sprint(v)
		}
	}
	return r, nil
}

// List returns one page and whether more rows follow.
func (s *Service) List(ctx context.Context, l *workspace.Lease, tableID string, q ListQuery) ([]Row, bool, error) {
	sch, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return nil, false, err
	}
	if q.Limit <= 0 || q.Limit > MaxPage {
		q.Limit = min(max(q.Limit, 1), MaxPage)
	}
	if q.Offset < 0 {
		return nil, false, fault.New(fault.BadRequest, "Offset cannot be negative.")
	}
	where, args, err := CompileFilter(t, q.Filter, DecodeValue)
	if err != nil {
		return nil, false, err
	}
	order, err := CompileSort(t, q.Sort)
	if err != nil {
		return nil, false, err
	}
	cols, data, lookups := selectList(sch, t)
	sql := fmt.Sprintf(`SELECT %s FROM %s t`, cols, catalog.Quote(t.PhysicalName))
	if where != "" {
		sql += " WHERE " + qualify(where)
	}
	sql += " ORDER BY " + qualify(order) + " LIMIT ? OFFSET ?"
	args = append(args, q.Limit+1, q.Offset)
	var out []Row
	err = l.Read(ctx, func(qr sqlitex.Querier) error {
		rows, err := qr.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRow(rows, data, lookups)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, false, fault.Classify(err)
	}
	more := len(out) > q.Limit
	if more {
		out = out[:q.Limit]
	}
	if out == nil {
		out = []Row{}
	}
	return out, more, nil
}

// qualify prefixes bare quoted identifiers from the compilers with the table
// alias "t." so lookup subqueries (alias p) cannot capture them.
func qualify(sqlFrag string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(sqlFrag); i++ {
		c := sqlFrag[i]
		switch {
		case c == '\'':
			inStr = !inStr
			b.WriteByte(c)
		case c == '"' && !inStr:
			j := i + 1
			for j < len(sqlFrag) {
				if sqlFrag[j] == '"' {
					if j+1 < len(sqlFrag) && sqlFrag[j+1] == '"' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if i == 0 || sqlFrag[i-1] != '.' {
				b.WriteString("t.")
			}
			b.WriteString(sqlFrag[i : j+1])
			i = j
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Count returns the number of matching rows, capped at CountCap.
func (s *Service) Count(ctx context.Context, l *workspace.Lease, tableID string, f *Filter) (int64, bool, error) {
	_, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return 0, false, err
	}
	where, args, err := CompileFilter(t, f, DecodeValue)
	if err != nil {
		return 0, false, err
	}
	sql := fmt.Sprintf(`SELECT count(*) FROM (SELECT 1 FROM %s t`, catalog.Quote(t.PhysicalName))
	if where != "" {
		sql += " WHERE " + qualify(where)
	}
	sql += " LIMIT ?)"
	args = append(args, CountCap+1)
	var n int64
	err = l.Read(ctx, func(q sqlitex.Querier) error { return q.QueryRow(ctx, sql, args...).Scan(&n) })
	if err != nil {
		return 0, false, fault.Classify(err)
	}
	if n > CountCap {
		return CountCap, true, nil
	}
	return n, false, nil
}

// Get returns one row.
func (s *Service) Get(ctx context.Context, l *workspace.Lease, tableID string, id int64) (Row, error) {
	sch, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return Row{}, err
	}
	var r Row
	err = l.Read(ctx, func(q sqlitex.Querier) error {
		var e error
		r, e = getRow(ctx, q, sch, t, id)
		return e
	})
	return r, err
}

func getRow(ctx context.Context, q sqlitex.Querier, sch *catalog.Schema, t *catalog.Table, id int64) (Row, error) {
	cols, data, lookups := selectList(sch, t)
	rows, err := q.Query(ctx, fmt.Sprintf(`SELECT %s FROM %s t WHERE t."id" = ?`, cols, catalog.Quote(t.PhysicalName)), id)
	if err != nil {
		return Row{}, fault.Classify(err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Row{}, fault.Classify(err)
		}
		return Row{}, fault.E(fault.NotFound)
	}
	return scanRow(rows, data, lookups)
}

// ---- writes ----------------------------------------------------------------

// decodeFields validates API field values (keyed by column ID) into storage
// values keyed by column, in deterministic (physical name) order.
func decodeFields(t *catalog.Table, fields map[string]any) ([]catalog.Column, []any, error) {
	type kv struct {
		c catalog.Column
		v any
	}
	var out []kv
	for id, raw := range fields {
		c, ok := t.Column(id)
		if !ok {
			return nil, nil, fault.Newf(fault.BadRequest, "Unknown field %s.", id)
		}
		v, err := DecodeValue(*c, raw)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, kv{*c, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].c.PhysicalName < out[j].c.PhysicalName })
	cols := make([]catalog.Column, len(out))
	vals := make([]any, len(out))
	for i, x := range out {
		cols[i], vals[i] = x.c, x.v
	}
	return cols, vals, nil
}

// mutation runs fn in a write transaction with idempotency (same key + same
// request → stored response replayed; different request → IDEMPOTENCY_CONFLICT)
// and publishes the resulting change after commit.
func (s *Service) mutation(ctx context.Context, l *workspace.Lease, idem *Idempotency, request any,
	fn func(tx *sqlitex.Tx) (resp any, change *change, err error)) (json.RawMessage, error) {
	var hash string
	if idem != nil && idem.Key != "" {
		if len(idem.Key) > 200 {
			return nil, fault.New(fault.BadRequest, "Idempotency-Key is too long.")
		}
		b, err := json.Marshal(request)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		hash = hex.EncodeToString(sum[:])
	}
	var out json.RawMessage
	var ch *change
	err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		if hash != "" {
			var user, h, resp string
			err := tx.QueryRow(ctx, `SELECT user_id, request_hash, response FROM _sys_idempotency WHERE key = ?`, idem.Key).Scan(&user, &h, &resp)
			switch {
			case err == nil && user == idem.UserID && h == hash:
				out = json.RawMessage(resp)
				return nil
			case err == nil:
				return fault.E(fault.IdempotencyConflict)
			case !sqlitex.IsNoRows(err):
				return err
			}
		}
		resp, c, err := fn(tx)
		if err != nil {
			return err
		}
		b, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		out, ch = b, c
		if hash != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO _sys_idempotency(key, user_id, request_hash, status_code, response, created_at) VALUES (?,?,?,?,?,?)`,
				idem.Key, idem.UserID, hash, 200, string(b), sqlitex.FormatTime(s.now())); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if ch != nil && s.Events != nil {
		s.publish(string(l.ID()), ch)
	}
	return out, nil
}

type change struct {
	tableID  string
	op       string
	ids      []int64
	versions []int64
}

func (s *Service) publish(ws string, c *change) {
	if len(c.ids) > coalesceIDs {
		s.Events.Publish(ws, events.TableInvalidated, map[string]any{"table_id": c.tableID})
		return
	}
	s.Events.Publish(ws, events.RowsChanged, map[string]any{"table_id": c.tableID, "op": c.op, "ids": c.ids, "versions": c.versions})
}

// translate maps constraint failures to field display names and validation text.
func translate(t *catalog.Table, err error) error {
	fe := fault.Classify(err)
	switch fe.Code {
	case fault.ValidationFailed:
		if name, _ := fe.Details["constraint"].(string); name != "" {
			if colID, isRule, ok := catalog.ConstraintColumn(name); ok {
				if c, ok := t.Column(colID); ok {
					msg := fmt.Sprintf("The value for %q is not valid.", c.DisplayName)
					if isRule && c.ValidationText != "" {
						msg = c.ValidationText
					} else if !isRule {
						msg = typeRuleMessage(*c)
					}
					return &fault.Error{Code: fault.ValidationFailed, Message: msg + " Nothing was saved.",
						Details: map[string]any{"field": c.ID}, Cause: err}
				}
			}
		}
	case fault.RequiredField:
		if phys, _ := fe.Details["column"].(string); phys != "" {
			if c, ok := t.ColumnByPhysical(phys); ok {
				return &fault.Error{Code: fault.RequiredField, Message: fmt.Sprintf("%q is required. Nothing was saved.", c.DisplayName),
					Details: map[string]any{"field": c.ID}, Cause: err}
			}
		}
	case fault.DuplicateValue:
		if cols, _ := fe.Details["columns"].(string); cols != "" {
			var names, idsOut []string
			for _, part := range strings.Split(cols, ", ") {
				phys := part[strings.LastIndex(part, ".")+1:]
				if c, ok := t.ColumnByPhysical(phys); ok {
					names = append(names, fmt.Sprintf("%q", c.DisplayName))
					idsOut = append(idsOut, c.ID)
				}
			}
			if len(names) > 0 {
				return &fault.Error{Code: fault.DuplicateValue,
					Message: fmt.Sprintf("Another record already has this value for %s. Nothing was saved.", strings.Join(names, ", ")),
					Details: map[string]any{"fields": idsOut}, Cause: err}
			}
		}
	}
	return fe
}

func typeRuleMessage(c catalog.Column) string {
	switch c.Type {
	case catalog.ShortText:
		return fmt.Sprintf("%q can hold at most %d characters.", c.DisplayName, c.MaxLen())
	case catalog.Date:
		return fmt.Sprintf("%q must be a date (YYYY-MM-DD).", c.DisplayName)
	case catalog.DateTime:
		return fmt.Sprintf("%q must be a date and time.", c.DisplayName)
	case catalog.Hyperlink:
		return fmt.Sprintf("%q must be a web address starting with http:// or https://.", c.DisplayName)
	case catalog.YesNo:
		return fmt.Sprintf("%q must be Yes or No.", c.DisplayName)
	}
	return fmt.Sprintf("The value for %q is not valid.", c.DisplayName)
}

func (s *Service) history(ctx context.Context, tx *sqlitex.Tx, t *catalog.Table, rowID, version int64, op string, actor Actor, before, after *Row) error {
	if !t.TrackHistory {
		return nil
	}
	enc := func(r *Row) any {
		if r == nil {
			return nil
		}
		b, _ := json.Marshal(r.Fields)
		return string(b)
	}
	var user any
	if actor.UserID != "" {
		user = actor.UserID
	}
	_, err := tx.Exec(ctx, `INSERT INTO _sys_row_history(table_id, row_id, row_version, op, user_id, at, before, after) VALUES (?,?,?,?,?,?,?,?)`,
		t.ID, rowID, version, op, user, sqlitex.FormatTime(s.now()), enc(before), enc(after))
	return err
}

// Insert creates a row.
func (s *Service) Insert(ctx context.Context, l *workspace.Lease, tableID string, fields map[string]any, actor Actor, idem *Idempotency) (Row, error) {
	sch, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return Row{}, err
	}
	cols, vals, err := decodeFields(t, fields)
	if err != nil {
		return Row{}, err
	}
	raw, err := s.mutation(ctx, l, idem, map[string]any{"op": "insert", "table": tableID, "fields": fields},
		func(tx *sqlitex.Tx) (any, *change, error) {
			r, err := insertRow(ctx, tx, sch, t, cols, vals)
			if err != nil {
				return nil, nil, err
			}
			if err := s.history(ctx, tx, t, r.ID, r.Version, "insert", actor, nil, &r); err != nil {
				return nil, nil, err
			}
			return r, &change{tableID: t.ID, op: "insert", ids: []int64{r.ID}, versions: []int64{r.Version}}, nil
		})
	if err != nil {
		return Row{}, translate(t, err)
	}
	var r Row
	return r, json.Unmarshal(raw, &r)
}

func insertRow(ctx context.Context, tx *sqlitex.Tx, sch *catalog.Schema, t *catalog.Table, cols []catalog.Column, vals []any) (Row, error) {
	var sql string
	if len(cols) == 0 {
		sql = fmt.Sprintf(`INSERT INTO %s DEFAULT VALUES`, catalog.Quote(t.PhysicalName))
	} else {
		names := make([]string, len(cols))
		marks := make([]string, len(cols))
		for i, c := range cols {
			names[i], marks[i] = catalog.Quote(c.PhysicalName), "?"
		}
		sql = fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, catalog.Quote(t.PhysicalName), strings.Join(names, ", "), strings.Join(marks, ", "))
	}
	res, err := tx.Exec(ctx, sql, vals...)
	if err != nil {
		return Row{}, err
	}
	return getRow(ctx, tx, sch, t, res.LastInsertID)
}

// Update applies fields to row id if its version is expectedVersion (OCC).
func (s *Service) Update(ctx context.Context, l *workspace.Lease, tableID string, id, expectedVersion int64, fields map[string]any, actor Actor, idem *Idempotency) (Row, error) {
	sch, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return Row{}, err
	}
	cols, vals, err := decodeFields(t, fields)
	if err != nil {
		return Row{}, err
	}
	raw, err := s.mutation(ctx, l, idem, map[string]any{"op": "update", "table": tableID, "id": id, "version": expectedVersion, "fields": fields},
		func(tx *sqlitex.Tx) (any, *change, error) {
			r, before, err := updateRow(ctx, tx, sch, t, id, expectedVersion, cols, vals, t.TrackHistory)
			if err != nil {
				return nil, nil, err
			}
			if err := s.history(ctx, tx, t, r.ID, r.Version, "update", actor, before, &r); err != nil {
				return nil, nil, err
			}
			return r, &change{tableID: t.ID, op: "update", ids: []int64{r.ID}, versions: []int64{r.Version}}, nil
		})
	if err != nil {
		return Row{}, translate(t, err)
	}
	var r Row
	return r, json.Unmarshal(raw, &r)
}

func updateRow(ctx context.Context, tx *sqlitex.Tx, sch *catalog.Schema, t *catalog.Table, id, expected int64, cols []catalog.Column, vals []any, wantBefore bool) (Row, *Row, error) {
	var before *Row
	if wantBefore {
		b, err := getRow(ctx, tx, sch, t, id)
		if err != nil && !errors.Is(err, fault.E(fault.NotFound)) {
			return Row{}, nil, err
		}
		if err == nil {
			before = &b
		}
	}
	sets := []string{`"_version" = "_version" + 1`}
	for _, c := range cols {
		sets = append(sets, catalog.Quote(c.PhysicalName)+" = ?")
	}
	args := append(append([]any{}, vals...), id, expected)
	res, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s WHERE "id" = ? AND "_version" = ?`, catalog.Quote(t.PhysicalName), strings.Join(sets, ", ")), args...)
	if err != nil {
		return Row{}, nil, err
	}
	if res.Changes == 0 {
		return Row{}, nil, occFailure(ctx, tx, sch, t, id)
	}
	r, err := getRow(ctx, tx, sch, t, id)
	return r, before, err
}

// occFailure distinguishes a missing row from a version conflict.
func occFailure(ctx context.Context, tx *sqlitex.Tx, sch *catalog.Schema, t *catalog.Table, id int64) error {
	cur, err := getRow(ctx, tx, sch, t, id)
	if err != nil {
		return err
	}
	return fault.E(fault.ConcurrentModification).WithDetails(map[string]any{"current": cur})
}

// Delete removes row id if its version is expectedVersion.
func (s *Service) Delete(ctx context.Context, l *workspace.Lease, tableID string, id, expectedVersion int64, actor Actor, idem *Idempotency) error {
	sch, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return err
	}
	_, err = s.mutation(ctx, l, idem, map[string]any{"op": "delete", "table": tableID, "id": id, "version": expectedVersion},
		func(tx *sqlitex.Tx) (any, *change, error) {
			if err := deleteRow(ctx, tx, sch, t, id, expectedVersion, s, actor); err != nil {
				return nil, nil, err
			}
			return map[string]any{"deleted": id}, &change{tableID: t.ID, op: "delete", ids: []int64{id}, versions: []int64{expectedVersion}}, nil
		})
	if err != nil {
		return translate(t, err)
	}
	return nil
}

func deleteRow(ctx context.Context, tx *sqlitex.Tx, sch *catalog.Schema, t *catalog.Table, id, expected int64, s *Service, actor Actor) error {
	var before *Row
	if t.TrackHistory {
		if b, err := getRow(ctx, tx, sch, t, id); err == nil {
			before = &b
		}
	}
	res, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE "id" = ? AND "_version" = ?`, catalog.Quote(t.PhysicalName)), id, expected)
	if err != nil {
		return err
	}
	if res.Changes == 0 {
		return occFailure(ctx, tx, sch, t, id)
	}
	return s.history(ctx, tx, t, id, expected, "delete", actor, before, nil)
}

// BatchOp is one operation in an all-or-nothing batch.
type BatchOp struct {
	Op      string         `json:"op" enum:"insert,update,delete"`
	ID      int64          `json:"id,omitempty"`
	Version int64          `json:"version,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// BatchResult reports one op's outcome.
type BatchResult struct {
	Op  string `json:"op"`
	Row *Row   `json:"row,omitempty"`
	ID  int64  `json:"id"`
}

// Batch applies ops in one transaction; any failure rolls back all of them and
// reports the failing index in details.op_index.
func (s *Service) Batch(ctx context.Context, l *workspace.Lease, tableID string, ops []BatchOp, actor Actor, idem *Idempotency) ([]BatchResult, error) {
	sch, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return nil, err
	}
	if len(ops) == 0 || len(ops) > MaxBatch {
		return nil, fault.Newf(fault.BadRequest, "A batch must have between 1 and %d operations.", MaxBatch)
	}
	type prepared struct {
		cols []catalog.Column
		vals []any
	}
	preps := make([]prepared, len(ops))
	for i, op := range ops {
		if op.Op == "insert" || op.Op == "update" {
			cols, vals, err := decodeFields(t, op.Fields)
			if err != nil {
				return nil, withIndex(err, i)
			}
			preps[i] = prepared{cols, vals}
		} else if op.Op != "delete" {
			return nil, withIndex(fault.Newf(fault.BadRequest, "Unknown operation %q.", op.Op), i)
		}
	}
	raw, err := s.mutation(ctx, l, idem, map[string]any{"op": "batch", "table": tableID, "ops": ops},
		func(tx *sqlitex.Tx) (any, *change, error) {
			out := make([]BatchResult, len(ops))
			ch := &change{tableID: t.ID, op: "batch"}
			for i, op := range ops {
				var err error
				switch op.Op {
				case "insert":
					var r Row
					r, err = insertRow(ctx, tx, sch, t, preps[i].cols, preps[i].vals)
					if err == nil {
						err = s.history(ctx, tx, t, r.ID, r.Version, "insert", actor, nil, &r)
						out[i] = BatchResult{Op: op.Op, Row: &r, ID: r.ID}
						ch.ids, ch.versions = append(ch.ids, r.ID), append(ch.versions, r.Version)
					}
				case "update":
					var r Row
					var before *Row
					r, before, err = updateRow(ctx, tx, sch, t, op.ID, op.Version, preps[i].cols, preps[i].vals, t.TrackHistory)
					if err == nil {
						err = s.history(ctx, tx, t, r.ID, r.Version, "update", actor, before, &r)
						out[i] = BatchResult{Op: op.Op, Row: &r, ID: r.ID}
						ch.ids, ch.versions = append(ch.ids, r.ID), append(ch.versions, r.Version)
					}
				case "delete":
					err = deleteRow(ctx, tx, sch, t, op.ID, op.Version, s, actor)
					out[i] = BatchResult{Op: op.Op, ID: op.ID}
					ch.ids, ch.versions = append(ch.ids, op.ID), append(ch.versions, op.Version)
				}
				if err != nil {
					return nil, nil, withIndex(translate(t, err), i)
				}
			}
			return out, ch, nil
		})
	if err != nil {
		return nil, err
	}
	var res []BatchResult
	return res, json.Unmarshal(raw, &res)
}

func withIndex(err error, i int) error {
	fe := fault.Classify(err)
	d := map[string]any{"op_index": i}
	for k, v := range fe.Details {
		d[k] = v
	}
	return &fault.Error{Code: fe.Code, Message: fe.Message, Details: d, RetryAfter: fe.RetryAfter, Cause: fe.Cause}
}

// PurgeIdempotency deletes keys older than 24 h (janitor).
func PurgeIdempotency(ctx context.Context, l *workspace.Lease, now time.Time) error {
	return l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM _sys_idempotency WHERE created_at < ?`, sqlitex.FormatTime(now.Add(-idempotencyTTL)))
		return err
	})
}

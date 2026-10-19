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
	"abidata/internal/dbx"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/pgdb"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Paging and batch limits.
const (
	MaxPage        = 500
	CountCap       = 100000
	MaxBatch       = 1000
	maxPageBytes   = 64 << 20
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
	// Hook, when set, may rewrite the fields of every insert and update
	// (ADR 0017's post rules); see WriteHook.
	Hook WriteHook
}

// WriteHook may rewrite an insert's (id 0) or update's fields, keyed by
// column id in API form, inside the write transaction and before they are
// decoded. current reads the row being updated. It returns changed=false to
// write the fields as given.
type WriteHook func(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, t *catalog.Table, id int64, fields map[string]any,
	current func() (Row, error)) (out map[string]any, changed bool, err error)

// hooked runs the Hook for one write and returns the columns and values to write.
func (s *Service) hooked(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, t *catalog.Table, id int64, fields map[string]any,
	cols []catalog.Column, vals []any) ([]catalog.Column, []any, error) {
	if s.Hook == nil {
		return cols, vals, nil
	}
	out, changed, err := s.Hook(ctx, tx, sch, t, id, fields, func() (Row, error) { return getRow(ctx, tx, sch, t, id) })
	if err != nil || !changed {
		return cols, vals, err
	}
	return decodeFields(t, out)
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

// selectList builds the column list for t (as alias "t") plus lookup display
// subqueries.
func selectList(d sqlgen.Dialect, sch *catalog.Schema, t *catalog.Table) ([]sqlgen.Column, []catalog.Column, []catalog.Column) {
	cols := []sqlgen.Column{{Expr: sqlgen.Col{Table: "t", Name: "id", Type: sqlgen.Integer}}, {Expr: sqlgen.Col{Table: "t", Name: "_version", Type: sqlgen.Integer}}}
	var data, lookups []catalog.Column
	for _, c := range t.Columns {
		if c.Type == catalog.Autonumber {
			continue
		}
		cols = append(cols, sqlgen.Column{Expr: selectCol(d, c)})
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
		cols = append(cols, sqlgen.Column{Expr: sqlgen.Subquery{Select: &sqlgen.Select{
			Columns: []sqlgen.Column{{Expr: sqlgen.Col{Table: "p", Name: disp.PhysicalName}}},
			From:    sqlgen.Table{Name: parent.PhysicalName, Alias: "p"},
			Where:   sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Table: "p", Name: "id"}, R: sqlgen.Col{Table: "t", Name: c.PhysicalName}},
		}}})
		lookups = append(lookups, c)
	}
	return cols, data, lookups
}

// render renders a statement this package built; failures are programming
// errors, since every value was decoded and checked first.
func render(d sqlgen.Dialect, s sqlgen.Stmt) (sqlgen.Rendered, error) {
	r, err := sqlgen.Render(d, s)
	if err != nil {
		return r, fmt.Errorf("records: %w", err)
	}
	return r, nil
}

func scanRow(rows dbx.Rows, data, lookups []catalog.Column) (Row, error) {
	vals := rows.Values()
	r := Row{Fields: make(map[string]any, len(data))}
	id, ok1 := vals[0].(int64)
	ver, ok2 := vals[1].(int64)
	if !ok1 || !ok2 {
		return Row{}, errors.New("records: malformed id/_version")
	}
	r.ID, r.Version = id, ver
	for i, c := range data {
		r.Fields[c.ID] = EncodeValue(c, catalog.StorageForm(storedType(c), vals[2+i]))
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
	var out []Row
	var more bool
	err = l.Read(ctx, func(qr dbx.Querier) error {
		out, more, err = ListTx(ctx, qr, sch, t, q)
		return err
	})
	return out, more, err
}

// ListTx is List on q: inside a write transaction, it sees the transaction's
// own changes.
func ListTx(ctx context.Context, qr dbx.Querier, sch *catalog.Schema, t *catalog.Table, q ListQuery) ([]Row, bool, error) {
	d := pgdb.DialectOf(qr)
	if q.Limit <= 0 || q.Limit > MaxPage {
		q.Limit = min(max(q.Limit, 1), MaxPage)
	}
	if q.Offset < 0 {
		return nil, false, fault.New(fault.BadRequest, "Offset cannot be negative.")
	}
	where, err := filterExpr(t, q.Filter, decoderFor(d), "t")
	if err != nil {
		return nil, false, err
	}
	order, err := sortOrders(t, q.Sort, "t")
	if err != nil {
		return nil, false, err
	}
	cols, data, lookups := selectList(d, sch, t)
	stmt, err := render(d, &sqlgen.Select{Columns: cols, From: sqlgen.Table{Name: t.PhysicalName, Alias: "t"}, Where: where, OrderBy: order,
		Limit: sqlgen.Arg{Value: q.Limit + 1}, Offset: sqlgen.Arg{Value: q.Offset}})
	if err != nil {
		return nil, false, err
	}
	var out []Row
	err = func() error {
		rows, err := qr.Query(ctx, stmt.SQL, stmt.Args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		size := 0
		for rows.Next() {
			for _, v := range rows.Values() {
				size += sqlitex.SizeOf(v)
			}
			if size > maxPageBytes {
				return fault.New(fault.BadRequest, "The rows on this page are too large to load at once; request fewer rows.")
			}
			r, err := scanRow(rows, data, lookups)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}()
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

// Count returns the number of matching rows, capped at CountCap.
func (s *Service) Count(ctx context.Context, l *workspace.Lease, tableID string, f *Filter) (int64, bool, error) {
	_, t, err := s.Table(ctx, l, tableID)
	if err != nil {
		return 0, false, err
	}
	where, err := filterExpr(t, f, decoderFor(l.Dialect()), "t")
	if err != nil {
		return 0, false, err
	}
	// Counting stops one past the cap, so a huge table costs no more than that.
	stmt, err := render(l.Dialect(), &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Agg{Fn: sqlgen.AggCount}}},
		From: sqlgen.SubSelect{Alias: "c", Select: &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Lit{Value: 1}}},
			From: sqlgen.Table{Name: t.PhysicalName, Alias: "t"}, Where: where, Limit: sqlgen.Arg{Value: CountCap + 1}}}})
	if err != nil {
		return 0, false, err
	}
	var n int64
	err = l.Read(ctx, func(q dbx.Querier) error { return q.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&n) })
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
	err = l.Read(ctx, func(q dbx.Querier) error {
		var e error
		r, e = getRow(ctx, q, sch, t, id)
		return e
	})
	return r, err
}

func getRow(ctx context.Context, q dbx.Querier, sch *catalog.Schema, t *catalog.Table, id int64) (Row, error) {
	cols, data, lookups := selectList(pgdb.DialectOf(q), sch, t)
	stmt, err := render(pgdb.DialectOf(q), &sqlgen.Select{Columns: cols, From: sqlgen.Table{Name: t.PhysicalName, Alias: "t"},
		Where: sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Table: "t", Name: "id"}, R: sqlgen.Arg{Value: id}}})
	if err != nil {
		return Row{}, err
	}
	rows, err := q.Query(ctx, stmt.SQL, stmt.Args...)
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
	fn func(tx dbx.Tx) (resp any, change *change, err error)) (json.RawMessage, error) {
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
	err := l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		if hash != "" {
			var user, h, resp string
			err := tx.QueryRow(ctx, sqlMutation.SQL, idem.Key).Scan(&user, &h, &resp)
			switch {
			case err == nil && user == idem.UserID && h == hash:
				out = json.RawMessage(resp)
				return nil
			case err == nil:
				return fault.E(fault.IdempotencyConflict)
			case !dbx.IsNoRows(err):
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
			if _, err := tx.Exec(ctx, sqlMutation2.SQL,
				idem.Key, idem.UserID, hash, 200, string(b), dbx.FormatTime(s.now())); err != nil {
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
	case catalog.GeoPoint, catalog.GeoArea:
		return geoTypeRuleMessage(c)
	}
	return fmt.Sprintf("The value for %q is not valid.", c.DisplayName)
}

func (s *Service) history(ctx context.Context, tx dbx.Tx, t *catalog.Table, rowID, version int64, op string, actor Actor, before, after *Row) error {
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
	_, err := tx.Exec(ctx, sqlHistory2.SQL,
		t.ID, rowID, version, op, user, dbx.FormatTime(s.now()), enc(before), enc(after))
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
		func(tx dbx.Tx) (any, *change, error) {
			cols, vals, err := s.hooked(ctx, tx, sch, t, 0, fields, cols, vals)
			if err != nil {
				return nil, nil, err
			}
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

func insertRow(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, t *catalog.Table, cols []catalog.Column, vals []any) (Row, error) {
	if err := checkAttachments(ctx, tx, cols, vals); err != nil {
		return Row{}, err
	}
	ins := &sqlgen.Insert{Table: t.PhysicalName, Returning: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}}}
	if len(cols) > 0 {
		row := make([]sqlgen.Expr, len(cols))
		for i, c := range cols {
			ins.Columns = append(ins.Columns, c.PhysicalName)
			row[i] = sqlgen.Arg{Value: ArgValue(pgdb.DialectOf(tx), c, vals[i])}
		}
		ins.Rows = [][]sqlgen.Expr{row}
	}
	stmt, err := render(pgdb.DialectOf(tx), ins)
	if err != nil {
		return Row{}, err
	}
	var id int64
	if err := tx.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&id); err != nil {
		return Row{}, err
	}
	return getRow(ctx, tx, sch, t, id)
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
		func(tx dbx.Tx) (any, *change, error) {
			cols, vals, err := s.hooked(ctx, tx, sch, t, id, fields, cols, vals)
			if err != nil {
				return nil, nil, err
			}
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

func updateRow(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, t *catalog.Table, id, expected int64, cols []catalog.Column, vals []any, wantBefore bool) (Row, *Row, error) {
	if err := checkAttachments(ctx, tx, cols, vals); err != nil {
		return Row{}, nil, err
	}
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
	set := []sqlgen.Assign{{Column: "_version", Value: sqlgen.Binary{Op: sqlgen.OpAdd, L: sqlgen.Col{Name: "_version"}, R: sqlgen.Lit{Value: 1}}}}
	for i, c := range cols {
		set = append(set, sqlgen.Assign{Column: c.PhysicalName, Value: sqlgen.Arg{Value: ArgValue(pgdb.DialectOf(tx), c, vals[i])}})
	}
	stmt, err := render(pgdb.DialectOf(tx), &sqlgen.Update{Table: t.PhysicalName, Set: set, Where: versionMatch(id, expected)})
	if err != nil {
		return Row{}, nil, err
	}
	res, err := tx.Exec(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return Row{}, nil, err
	}
	if res.Changes == 0 {
		return Row{}, nil, occFailure(ctx, tx, sch, t, id)
	}
	r, err := getRow(ctx, tx, sch, t, id)
	return r, before, err
}

// versionMatch selects row id only at the expected version (OCC).
func versionMatch(id, expected int64) sqlgen.Expr {
	return sqlgen.And{
		sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: "id"}, R: sqlgen.Arg{Value: id}},
		sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: "_version"}, R: sqlgen.Arg{Value: expected}},
	}
}

// occFailure distinguishes a missing row from a version conflict.
func occFailure(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, t *catalog.Table, id int64) error {
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
		func(tx dbx.Tx) (any, *change, error) {
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

func deleteRow(ctx context.Context, tx dbx.Tx, sch *catalog.Schema, t *catalog.Table, id, expected int64, s *Service, actor Actor) error {
	var before *Row
	if t.TrackHistory {
		if b, err := getRow(ctx, tx, sch, t, id); err == nil {
			before = &b
		}
	}
	stmt, err := render(pgdb.DialectOf(tx), &sqlgen.Delete{Table: t.PhysicalName, Where: versionMatch(id, expected)})
	if err != nil {
		return err
	}
	res, err := tx.Exec(ctx, stmt.SQL, stmt.Args...)
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
		func(tx dbx.Tx) (any, *change, error) {
			out := make([]BatchResult, len(ops))
			ch := &change{tableID: t.ID, op: "batch"}
			for i, op := range ops {
				var err error
				switch op.Op {
				case "insert":
					var r Row
					cols, vals, herr := s.hooked(ctx, tx, sch, t, 0, op.Fields, preps[i].cols, preps[i].vals)
					if herr != nil {
						return nil, nil, withIndex(herr, i)
					}
					r, err = insertRow(ctx, tx, sch, t, cols, vals)
					if err == nil {
						err = s.history(ctx, tx, t, r.ID, r.Version, "insert", actor, nil, &r)
						out[i] = BatchResult{Op: op.Op, Row: &r, ID: r.ID}
						ch.ids, ch.versions = append(ch.ids, r.ID), append(ch.versions, r.Version)
					}
				case "update":
					var r Row
					var before *Row
					cols, vals, herr := s.hooked(ctx, tx, sch, t, op.ID, op.Fields, preps[i].cols, preps[i].vals)
					if herr != nil {
						return nil, nil, withIndex(herr, i)
					}
					r, before, err = updateRow(ctx, tx, sch, t, op.ID, op.Version, cols, vals, t.TrackHistory)
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
	return l.Write(ctx, workspace.Background, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlPurgeIdempotency.SQL, dbx.FormatTime(now.Add(-idempotencyTTL)))
		return err
	})
}

// ---- in-transaction variants (Lua db.transaction blocks) -------------------

// TxResult is a row change made inside a caller-owned transaction; Publish
// must be called only after that transaction commits.
type TxResult struct {
	Row     Row
	Publish func()
}

func (s *Service) publisher(ws string, c *change) func() {
	return func() {
		if s.Events != nil {
			s.publish(ws, c)
		}
	}
}

// InsertTx inserts within tx.
func (s *Service) InsertTx(ctx context.Context, tx dbx.Tx, ws string, sch *catalog.Schema, t *catalog.Table, fields map[string]any, actor Actor) (TxResult, error) {
	cols, vals, err := decodeFields(t, fields)
	if err != nil {
		return TxResult{}, err
	}
	if cols, vals, err = s.hooked(ctx, tx, sch, t, 0, fields, cols, vals); err != nil {
		return TxResult{}, err
	}
	r, err := insertRow(ctx, tx, sch, t, cols, vals)
	if err != nil {
		return TxResult{}, translate(t, err)
	}
	if err := s.history(ctx, tx, t, r.ID, r.Version, "insert", actor, nil, &r); err != nil {
		return TxResult{}, err
	}
	return TxResult{Row: r, Publish: s.publisher(ws, &change{tableID: t.ID, op: "insert", ids: []int64{r.ID}, versions: []int64{r.Version}})}, nil
}

// UpdateTx updates within tx (OCC on expectedVersion).
func (s *Service) UpdateTx(ctx context.Context, tx dbx.Tx, ws string, sch *catalog.Schema, t *catalog.Table, id, expectedVersion int64, fields map[string]any, actor Actor) (TxResult, error) {
	cols, vals, err := decodeFields(t, fields)
	if err != nil {
		return TxResult{}, err
	}
	if cols, vals, err = s.hooked(ctx, tx, sch, t, id, fields, cols, vals); err != nil {
		return TxResult{}, err
	}
	r, before, err := updateRow(ctx, tx, sch, t, id, expectedVersion, cols, vals, t.TrackHistory)
	if err != nil {
		return TxResult{}, translate(t, err)
	}
	if err := s.history(ctx, tx, t, r.ID, r.Version, "update", actor, before, &r); err != nil {
		return TxResult{}, err
	}
	return TxResult{Row: r, Publish: s.publisher(ws, &change{tableID: t.ID, op: "update", ids: []int64{r.ID}, versions: []int64{r.Version}})}, nil
}

// DeleteTx deletes within tx (OCC on expectedVersion).
func (s *Service) DeleteTx(ctx context.Context, tx dbx.Tx, ws string, sch *catalog.Schema, t *catalog.Table, id, expectedVersion int64, actor Actor) (TxResult, error) {
	if err := deleteRow(ctx, tx, sch, t, id, expectedVersion, s, actor); err != nil {
		return TxResult{}, translate(t, err)
	}
	return TxResult{Publish: s.publisher(ws, &change{tableID: t.ID, op: "delete", ids: []int64{id}, versions: []int64{expectedVersion}})}, nil
}

// GetTx reads a row within tx.
func GetTx(ctx context.Context, q dbx.Querier, sch *catalog.Schema, t *catalog.Table, id int64) (Row, error) {
	return getRow(ctx, q, sch, t, id)
}

// PublishInvalidated announces that a table changed in ways the row API did
// not track (raw SQL from scripts).
func (s *Service) PublishInvalidated(ws string, tableIDs []string) {
	if s.Events == nil {
		return
	}
	for _, id := range tableIDs {
		s.Events.Publish(ws, events.TableInvalidated, map[string]any{"table_id": id})
	}
}

// Translate maps a database error raised while writing t to a classified fault
// that names fields by display name (the same mapping the row API uses). Bulk
// writers such as the importer call it for per-row failures.
func Translate(t *catalog.Table, err error) error { return translate(t, err) }

// storedType is the field type whose storage form column c holds.
func storedType(c catalog.Column) catalog.FieldType {
	if c.Type == catalog.Calculated {
		return ResultFieldType(c.Options.ResultType)
	}
	return c.Type
}

// ArgValue binds a storage value for column c in dialect d. PostgreSQL
// stores yes/no as boolean rather than SQLite's 0/1; dates, times and JSON go
// as text, which the server parses for the column's type.
func ArgValue(d sqlgen.Dialect, c catalog.Column, v any) any {
	if n, ok := v.(int64); ok && c.Type == catalog.YesNo && d != sqlgen.SQLite {
		return n != 0
	}
	return v
}

// decoderFor decodes filter values for dialect d.
func decoderFor(d sqlgen.Dialect) valueDecoder {
	if d == sqlgen.SQLite {
		return DecodeValue
	}
	return func(c catalog.Column, v any) (any, error) {
		x, err := DecodeValue(c, v)
		return ArgValue(d, c, x), err
	}
}

// selectCol reads column c of the row alias t. PostgreSQL keeps JSON values
// (attachments, geo) as jsonb, which pgx would decode into Go maps; they are
// read as SQLite's compact text instead.
func selectCol(d sqlgen.Dialect, c catalog.Column) sqlgen.Expr {
	col := sqlgen.Col{Table: "t", Name: c.PhysicalName}
	if d == sqlgen.SQLite {
		return col
	}
	switch c.Type { // a calculated column's value is text
	case catalog.Attachments, catalog.GeoPoint, catalog.GeoArea:
		return sqlgen.Raw{SQL: `abi_json_min("t".` + catalog.Quote(c.PhysicalName) + `)`}
	}
	return col
}

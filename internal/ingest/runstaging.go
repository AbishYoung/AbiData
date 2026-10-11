package ingest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/query"
	"abidata/internal/script"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Run staging limits.
const (
	MaxRunStagingBytes = 256 << 20
	MaxRunHandles      = 8
	maxQueryRows       = 100_000
	maxQueryBytes      = 32 << 20
	queryTimeout       = 20 * time.Second
	stagingPageSize    = 4096
)

var colNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// RunStaging holds the staging databases of one job run. Close deletes them.
type RunStaging struct {
	s      *Service
	l      *workspace.Lease
	runID  string
	mu     sync.Mutex
	hs     []*runHandle
	closed bool
}

type runHandle struct {
	conn *sqlitex.Conn
	path string
	cols []string
	rows int64
}

// NewRunStaging returns the staging store for one job run.
func (s *Service) NewRunStaging(l *workspace.Lease, runID string) *RunStaging {
	return &RunStaging{s: s, l: l, runID: runID}
}

func (r *RunStaging) handle(h int) (*runHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || h < 0 || h >= len(r.hs) {
		return nil, fault.New(fault.BadRequest, "This staging table is no longer available.")
	}
	return r.hs[h], nil
}

// Close closes and deletes every staging database of the run.
func (r *RunStaging) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	for _, h := range r.hs {
		_ = h.conn.Close()
		_ = os.Remove(h.path)
	}
	r.hs = nil
}

// Create implements host.StagingStore.
func (r *RunStaging) Create(ctx context.Context, columns []string) (int, error) {
	if len(columns) == 0 || len(columns) > MaxColumns {
		return 0, fault.Newf(fault.BadRequest, "A staging table needs between 1 and %d columns.", MaxColumns)
	}
	seen := map[string]bool{"n": true}
	for _, c := range columns {
		if !colNameRe.MatchString(c) || seen[strings.ToLower(c)] {
			return 0, fault.Newf(fault.BadRequest, "Invalid or repeated staging column %q: use letters, digits and underscores, starting with a letter or underscore (the name n is reserved).", c)
		}
		seen[strings.ToLower(c)] = true
	}
	r.mu.Lock()
	if r.closed || len(r.hs) >= MaxRunHandles {
		r.mu.Unlock()
		return 0, fault.Newf(fault.QuotaExceeded, "A run may create at most %d staging tables.", MaxRunHandles)
	}
	idx := len(r.hs)
	r.hs = append(r.hs, nil)
	r.mu.Unlock()
	dir, err := r.s.stagingDir(r.l.ID())
	if err != nil {
		return 0, err
	}
	path, err := ids.SafeJoin(dir, fmt.Sprintf("run-%s-%d.db", r.runID, idx))
	if err != nil {
		return 0, err
	}
	c, err := sqlitex.Open(ctx, path, sqlitex.Options{Kind: sqlitex.Staging})
	if err != nil {
		return 0, err
	}
	if _, err := c.Exec(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", MaxRunStagingBytes/stagingPageSize)); err != nil {
		_ = c.Close()
		return 0, err
	}
	var b strings.Builder
	b.WriteString(`CREATE TABLE data (n INTEGER PRIMARY KEY`)
	for _, name := range columns {
		b.WriteString(", " + catalog.Quote(name))
	}
	b.WriteString(")")
	if err := c.ExecScript(ctx, b.String()); err != nil {
		_ = c.Close()
		return 0, err
	}
	r.mu.Lock()
	r.hs[idx] = &runHandle{conn: c, path: path, cols: append([]string(nil), columns...)}
	r.mu.Unlock()
	return idx, nil
}

func stageValue(v any) (any, error) {
	switch x := v.(type) {
	case nil, script.Null:
		return nil, nil
	case bool:
		if x {
			return int64(1), nil
		}
		return int64(0), nil
	case int64, string:
		return x, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, fault.New(fault.BadRequest, "Numbers must be finite.")
		}
		return x, nil
	}
	return nil, fault.Newf(fault.BadRequest, "Staging values must be text, numbers, booleans or empty (got %T).", v)
}

// Insert implements host.StagingStore.
func (r *RunStaging) Insert(ctx context.Context, h int, row any) error {
	st, err := r.handle(h)
	if err != nil {
		return err
	}
	if st.rows >= MaxRows {
		return fault.Newf(fault.QuotaExceeded, "A staging table holds at most %d rows.", MaxRows)
	}
	vals := make([]any, len(st.cols))
	switch x := row.(type) {
	case map[string]any:
		used := 0
		for i, c := range st.cols {
			if v, ok := x[c]; ok {
				used++
				if vals[i], err = stageValue(v); err != nil {
					return err
				}
			}
		}
		if used != len(x) {
			for k := range x {
				known := false
				for _, c := range st.cols {
					known = known || c == k
				}
				if !known {
					return fault.Newf(fault.BadRequest, "The staging table has no column %q.", k)
				}
			}
		}
	case []any:
		if len(x) > len(st.cols) {
			return fault.Newf(fault.BadRequest, "The row has %d values but the staging table has %d columns.", len(x), len(st.cols))
		}
		for i, v := range x {
			if vals[i], err = stageValue(v); err != nil {
				return err
			}
		}
	default:
		return fault.New(fault.BadRequest, "insert takes a table keyed by column name or an array in column order.")
	}
	names := make([]string, len(st.cols))
	marks := make([]string, len(st.cols))
	for i, c := range st.cols {
		names[i], marks[i] = catalog.Quote(c), "?"
	}
	if _, err := st.conn.Exec(ctx, `INSERT INTO data(`+strings.Join(names, ",")+`) VALUES (`+strings.Join(marks, ",")+`)`, vals...); err != nil {
		if errors.Is(err, sqlite3.FULL) {
			return fault.Newf(fault.QuotaExceeded, "The staging table is full (at most %d MiB).", MaxRunStagingBytes>>20)
		}
		return fault.Classify(err)
	}
	st.rows++
	return nil
}

// Count implements host.StagingStore.
func (r *RunStaging) Count(_ context.Context, h int) (int64, error) {
	st, err := r.handle(h)
	if err != nil {
		return 0, err
	}
	return st.rows, nil
}

// Query implements host.StagingStore. Only a single read-only statement on the
// staging table is allowed: the SQLite authorizer refuses every write, PRAGMA,
// ATTACH and system table, and the connection has no other database attached.
func (r *RunStaging) Query(ctx context.Context, h int, sql string, params any) ([]string, [][]any, error) {
	st, err := r.handle(h)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(sql) == "" || len(sql) > query.MaxSQLLen {
		return nil, nil, fault.New(fault.BadRequest, "Give a SELECT statement (at most 100,000 characters).")
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	var cols []string
	var out [][]any
	var denial query.Denial
	err = st.conn.WithAuthorizer(query.Policy{}.Authorizer(&denial), func() error {
		desc, err := st.conn.Describe(ctx, sql)
		if err != nil {
			return err
		}
		if !desc.ReadOnly {
			return fault.New(fault.SQLNotAllowed, "Only SELECT statements are allowed on staging tables.")
		}
		args, err := stageArgs(desc.Params, params)
		if err != nil {
			return err
		}
		rows, err := st.conn.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols = rows.Columns()
		size := 0
		for rows.Next() {
			if len(out) >= maxQueryRows || size > maxQueryBytes {
				return fault.Newf(fault.BadRequest, "The query returned more than %d rows or %d MiB; add a LIMIT or aggregate in SQL.", maxQueryRows, maxQueryBytes>>20)
			}
			vals := rows.Values()
			for i, v := range vals {
				size += sqlitex.SizeOf(v)
				if b, ok := v.([]byte); ok {
					vals[i] = base64.StdEncoding.EncodeToString(b)
				}
			}
			out = append(out, vals)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, stageQueryErr(&denial, err)
	}
	return cols, out, nil
}

func stageQueryErr(d *query.Denial, err error) error {
	var fe *fault.Error
	if errors.As(err, &fe) {
		return err
	}
	var se *sqlite3.Error
	if errors.As(err, &se) {
		if se.Code() == sqlite3.AUTH || d.Reason != "" {
			reason := d.Reason
			if reason == "" {
				reason = "the statement uses an operation that is not permitted"
			}
			return &fault.Error{Code: fault.SQLNotAllowed, Message: "Not allowed: " + reason + ".", Cause: err}
		}
		if se.Code() == sqlite3.ERROR {
			// Keep SQLite's whole message ("no such column: x"), as the query
			// console does; cutting at the last ": " lost the subject.
			return fault.SQLError(fault.SQLiteMessage(se), err)
		}
	}
	if strings.Contains(err.Error(), "multiple statements") {
		return fault.New(fault.SQLNotAllowed, "Only one statement can run at a time.")
	}
	return fault.Classify(err)
}

func stageArgs(names []string, params any) ([]any, error) {
	var args []any
	for i, n := range names {
		var v any
		switch p := params.(type) {
		case nil:
			return nil, fault.Newf(fault.BadRequest, "The statement needs parameters (%s).", n)
		case []any:
			if !strings.HasPrefix(n, "?") {
				return nil, fault.New(fault.BadRequest, "Use ? placeholders with an array of parameters.")
			}
			if i >= len(p) {
				return nil, fault.New(fault.BadRequest, "Fewer parameters were given than the statement has placeholders.")
			}
			v = p[i]
		case map[string]any:
			if strings.HasPrefix(n, "?") {
				return nil, fault.New(fault.BadRequest, "Use named parameters such as :name with a table of parameters.")
			}
			var ok bool
			if v, ok = p[n[1:]]; !ok {
				return nil, fault.Newf(fault.BadRequest, "Parameter %s was not given.", n)
			}
		default:
			return nil, fault.New(fault.BadRequest, "params must be an array or a table.")
		}
		sv, err := stageValue(v)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(n, "?") {
			args = append(args, sv)
		} else {
			args = append(args, sqlitex.Named{Name: n, Value: sv})
		}
	}
	return args, nil
}

// Merge implements host.StagingStore: staging columns are matched to the
// table's fields by name (display or physical, case-insensitive).
func (r *RunStaging) Merge(ctx context.Context, h int, table string, opts map[string]any) (map[string]any, error) {
	st, err := r.handle(h)
	if err != nil {
		return nil, err
	}
	sch, err := r.s.Schemas.Get(ctx, r.l)
	if err != nil {
		return nil, err
	}
	var t *catalog.Table
	for i := range sch.Tables {
		if strings.EqualFold(sch.Tables[i].DisplayName, table) || strings.EqualFold(sch.Tables[i].PhysicalName, table) {
			t = &sch.Tables[i]
			break
		}
	}
	if t == nil {
		return nil, fault.Newf(fault.NotFound, "There is no table named %q.", table)
	}
	p := &plan{Table: t, Mode: ModeAppend}
	chunk := ChunkRows
	for k, v := range opts {
		switch k {
		case "mode":
			s, _ := v.(string)
			p.Mode = s
		case "key":
		case "chunk":
			f, ok := v.(int64)
			if !ok || f < 1 {
				return nil, fault.New(fault.BadRequest, "chunk must be a positive whole number.")
			}
			chunk = int(min(f, 10_000))
		case "all_or_nothing":
			b, ok := v.(bool)
			if !ok {
				return nil, fault.New(fault.BadRequest, "all_or_nothing must be true or false.")
			}
			p.AllOrNo = b
		default:
			return nil, fault.Newf(fault.BadRequest, "Unknown option %q.", k)
		}
	}
	switch p.Mode {
	case ModeAppend, ModeUpsert, ModeReplace:
	default:
		return nil, fault.New(fault.BadRequest, `mode must be "append", "upsert" or "replace".`)
	}
	var unmatched []string
	for i, name := range st.cols {
		var c *catalog.Column
		for j := range t.Columns {
			col := &t.Columns[j]
			if strings.EqualFold(col.DisplayName, name) || strings.EqualFold(col.PhysicalName, name) {
				c = col
				break
			}
		}
		if c != nil && c.Type == catalog.Attachments {
			continue // files cannot be merged from a staging table; the column is ignored
		}
		if c == nil || c.Type == catalog.Autonumber || c.Type == catalog.Calculated {
			unmatched = append(unmatched, name)
			continue
		}
		p.Cols = append(p.Cols, mapCol{Src: i, Stage: name, Col: *c})
	}
	if len(unmatched) > 0 {
		return nil, fault.Newf(fault.BadRequest, "These staging columns match no writable field of %q: %s.", t.DisplayName, strings.Join(unmatched, ", "))
	}
	if p.Mode == ModeUpsert {
		kn, _ := opts["key"].(string)
		for i := range p.Cols {
			if strings.EqualFold(p.Cols[i].Col.DisplayName, kn) || strings.EqualFold(p.Cols[i].Col.PhysicalName, kn) {
				p.Key = &p.Cols[i].Col
			}
		}
		if p.Key == nil {
			return nil, fault.New(fault.BadRequest, "upsert needs key = the name of a staged column that is a field of the table.")
		}
		unique := false
		for _, ix := range t.Indexes {
			unique = unique || ix.Unique && len(ix.ColumnIDs) == 1 && ix.ColumnIDs[0] == p.Key.ID
		}
		if !unique {
			return nil, fault.Newf(fault.BadRequest, "Upsert needs a unique index on %q.", p.Key.DisplayName)
		}
	} else if _, ok := opts["key"]; ok {
		return nil, fault.New(fault.BadRequest, "key is only used with upsert.")
	}
	res, err := r.s.merge(ctx, r.l, st.conn, p, st.rows, chunk, nil)
	if res.Inserted+res.Updated > 0 || p.Mode == ModeReplace && err == nil {
		r.s.publish(string(r.l.ID()), t.ID)
	}
	if errors.Is(err, workspace.ErrNestedWrite) {
		return nil, fault.New(fault.BadRequest, "merge_into cannot run inside db.transaction.")
	}
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

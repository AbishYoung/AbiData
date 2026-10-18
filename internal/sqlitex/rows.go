package sqlitex

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ncruces/go-sqlite3"
)

// Rows iterates a result set. It is not safe for concurrent use.
type Rows struct {
	st      *sqlite3.Stmt
	conn    *Conn
	ctx     context.Context
	release func()
	err     error
	closed  bool
}

// Next advances to the next row.
func (r *Rows) Next() (ok bool) {
	if r.closed {
		return false
	}
	defer func() {
		if p := recover(); p != nil {
			r.conn.broken = true
			r.err = errors.Join(errOOM, fmt.Errorf("sqlite: %v", p))
			r.Close()
			ok = false
		}
	}()
	if r.st.Step() {
		return true
	}
	r.err = wrapErr(r.ctx, r.st.Err())
	r.Close()
	return false
}

// Err returns the iteration error, if any.
func (r *Rows) Err() error { return r.err }

// Close releases the statement back to the cache. Safe to call more than once.
func (r *Rows) Close() {
	if r.closed {
		return
	}
	r.closed = true
	r.release()
}

// Columns returns the result column names.
func (r *Rows) Columns() []string {
	n := r.st.ColumnCount()
	cols := make([]string, n)
	for i := range n {
		cols[i] = r.st.ColumnName(i)
	}
	return cols
}

// ColumnOrigin reports the table and column a result column is read from
// directly (both empty for expressions) and its declared type.
func (r *Rows) ColumnOrigin(i int) (table, column, decl string) {
	return r.st.ColumnTableName(i), r.st.ColumnOriginName(i), r.st.ColumnDeclType(i)
}

// ColumnCount returns the number of result columns.
func (r *Rows) ColumnCount() int { return r.st.ColumnCount() }

// Value returns column i as nil, int64, float64, string or []byte.
func (r *Rows) Value(i int) any {
	switch r.st.ColumnType(i) {
	case sqlite3.NULL:
		return nil
	case sqlite3.INTEGER:
		return r.st.ColumnInt64(i)
	case sqlite3.FLOAT:
		return r.st.ColumnFloat(i)
	case sqlite3.TEXT:
		return r.st.ColumnText(i)
	default:
		return r.st.ColumnBlob(i, nil)
	}
}

// Values returns the current row's values (see Value).
func (r *Rows) Values() []any {
	n := r.st.ColumnCount()
	out := make([]any, n)
	for i := range n {
		out[i] = r.Value(i)
	}
	return out
}

// Scan copies the current row into dest pointers: *int, *int64, *float64, *string,
// *bool, *[]byte, *any, and *NullString/*NullInt64 helpers (pointer-to-pointer for nullable).
func (r *Rows) Scan(dest ...any) error {
	if len(dest) != r.st.ColumnCount() {
		return fmt.Errorf("sqlitex: scan expects %d destinations, got %d", r.st.ColumnCount(), len(dest))
	}
	for i, d := range dest {
		null := r.st.ColumnType(i) == sqlite3.NULL
		switch p := d.(type) {
		case *int:
			*p = r.st.ColumnInt(i)
		case *int64:
			*p = r.st.ColumnInt64(i)
		case *float64:
			*p = r.st.ColumnFloat(i)
		case *string:
			*p = r.st.ColumnText(i)
		case *bool:
			*p = r.st.ColumnBool(i)
		case *[]byte:
			if null {
				*p = nil
			} else {
				*p = r.st.ColumnBlob(i, nil)
			}
		case *any:
			*p = r.Value(i)
		case **string:
			if null {
				*p = nil
			} else {
				s := r.st.ColumnText(i)
				*p = &s
			}
		case **int64:
			if null {
				*p = nil
			} else {
				v := r.st.ColumnInt64(i)
				*p = &v
			}
		case nil:
			// skip column
		default:
			// Named types over basic kinds (e.g. type FieldType string).
			rv := reflect.ValueOf(d)
			if rv.Kind() != reflect.Pointer || rv.IsNil() {
				return fmt.Errorf("sqlitex: unsupported scan destination %T", d)
			}
			switch e := rv.Elem(); e.Kind() {
			case reflect.String:
				e.SetString(r.st.ColumnText(i))
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				e.SetInt(r.st.ColumnInt64(i))
			case reflect.Float32, reflect.Float64:
				e.SetFloat(r.st.ColumnFloat(i))
			case reflect.Bool:
				e.SetBool(r.st.ColumnBool(i))
			default:
				return fmt.Errorf("sqlitex: unsupported scan destination %T", d)
			}
		}
	}
	return nil
}

// Row is the result of QueryRow.
type Row struct {
	rows *Rows
	err  error
}

// Scan scans the first row and closes. Returns ErrNoRows when empty.
func (r *Row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer r.rows.Close()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return ErrNoRows
	}
	return r.rows.Scan(dest...)
}

// stmtCache holds prepared statements keyed by SQL text. A statement taken from
// the cache is removed until put back, so nested use of the same SQL prepares a
// second statement instead of sharing one.
type stmtCache struct {
	max   int
	stmts map[string]*sqlite3.Stmt
	order []string
}

func newStmtCache(max int) *stmtCache { return &stmtCache{max: max, stmts: map[string]*sqlite3.Stmt{}} }

func (c *stmtCache) take(sql string) *sqlite3.Stmt {
	st, ok := c.stmts[sql]
	if !ok {
		return nil
	}
	delete(c.stmts, sql)
	return st
}

func (c *stmtCache) put(sql string, st *sqlite3.Stmt) {
	_ = st.Reset()
	_ = st.ClearBindings()
	if _, dup := c.stmts[sql]; dup || c.max == 0 {
		_ = st.Close()
		return
	}
	if len(c.stmts) >= c.max {
		for len(c.order) > 0 {
			oldest := c.order[0]
			c.order = c.order[1:]
			if old, ok := c.stmts[oldest]; ok {
				_ = old.Close()
				delete(c.stmts, oldest)
				break
			}
		}
	}
	c.stmts[sql] = st
	c.order = append(c.order, sql)
}

func (c *stmtCache) closeAll() {
	for k, st := range c.stmts {
		_ = st.Close()
		delete(c.stmts, k)
	}
	c.order = c.order[:0]
}

// SizeOf estimates the memory a scanned value occupies (for result byte caps).
func SizeOf(v any) int {
	switch x := v.(type) {
	case string:
		return len(x) + 16
	case []byte:
		return len(x) + 24
	}
	return 16
}

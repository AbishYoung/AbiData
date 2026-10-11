// Package db is the Lua `db` module: SQL reads and writes on workspace tables
// under the SQL authorizer, transactions with their own budget, and the Access
// DLookup family.
package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ncruces/go-sqlite3"
	lua "github.com/yuin/gopher-lua"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/query"
	"abidata/internal/script"
	"abidata/internal/script/host"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// MaxRows and MaxBytes cap results returned by db.query.
const (
	MaxRows  = 10000
	MaxBytes = 32 << 20
)

var allCtx = []script.Context{script.UIEvent, script.Report, script.Job}

func init() {
	script.Register(script.ModuleDef{Doc: doc, Build: build})
}

var doc = script.ModuleDoc{
	Name:     "db",
	Doc:      "Run SQL against this workspace's tables. Reads run on a snapshot; writes need edit rights and are not recorded in row history (use `records` for tracked edits). Statements are checked by the SQL authorizer: system tables, schema changes, PRAGMA and ATTACH are not allowed.",
	Contexts: allCtx,
	Funcs: []script.FuncDoc{
		{Name: "query", Signature: "db.query(sql, params?) → rows", Params: []script.ParamDoc{{Name: "sql", Type: "string", Doc: "One read-only statement; use :name placeholders."}, {Name: "params", Type: "table", Doc: "Values for :name placeholders.", Opt: true}},
			Returns: "array of row tables keyed by column name (SQL NULL is db.NULL)", Errors: []string{"SQL_NOT_ALLOWED", "BAD_REQUEST"},
			Doc: "Runs a read-only query and returns up to 10,000 rows.", Example: `local rows = db.query("SELECT Name FROM Customers WHERE City = :city", {city = "Paris"})`},
		{Name: "query_one", Signature: "db.query_one(sql, params?) → row|nil", Returns: "first row or nil", Doc: "Like db.query but returns only the first row.", Example: `local c = db.query_one("SELECT * FROM Customers WHERE id = :id", {id = 1})`},
		{Name: "query_value", Signature: "db.query_value(sql, params?) → value|nil", Returns: "first column of the first row, or nil when there are no rows", Doc: "Returns a single value.", Example: `local n = db.query_value("SELECT count(*) FROM Orders")`},
		{Name: "iter", Signature: "db.iter(sql, params?) → iterator", Returns: "iterator yielding row tables", Doc: "Iterates over a large result in pages of 1,000 rows (jobs only).", Example: `for row in db.iter("SELECT * FROM Orders") do total = total + row.Qty end`, Contexts: []script.Context{script.Job}},
		{Name: "exec", Signature: "db.exec(sql, params?) → {changes, last_id}", Returns: "table with changes and last_id", Errors: []string{"SQL_NOT_ALLOWED", "VALIDATION_FAILED", "LINKED_RECORDS"},
			Doc: "Runs one INSERT, UPDATE or DELETE on a workspace table. Not available in reports.", Example: `db.exec("UPDATE Orders SET Status = 'Shipped' WHERE id = :id", {id = 7})`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "transaction", Signature: "db.transaction(fn) → results of fn", Doc: "Runs fn inside one write transaction (all-or-nothing). Raising an error inside fn rolls back. The block has its own time budget (250 ms in form events, 2 s in jobs).",
			Example: `db.transaction(function() db.exec("UPDATE Stock SET Qty = Qty - 1 WHERE id = :id", {id = 3}) records.insert("Moves", {Item = 3}) end)`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "lookup", Signature: "db.lookup(table, field, where?, params?) → value|db.NULL", Doc: "Access DLookup: the field's value in the first matching row, or db.NULL.", Example: `local email = db.lookup("Customers", "Email", "id = :id", {id = record.Customer})`},
		{Name: "count", Signature: "db.count(table, field?, where?, params?) → number", Doc: "Access DCount: number of matching rows (non-empty values of field, or all rows when field is \"*\" or omitted).", Example: `local open = db.count("Orders", "*", "Status = 'Open'")`},
		{Name: "sum", Signature: "db.sum(table, field, where?, params?) → number|db.NULL", Doc: "Access DSum.", Example: `local total = db.sum("OrderLines", "Qty", "OrderId = :id", {id = record.id})`},
		{Name: "avg", Signature: "db.avg(table, field, where?, params?) → number|db.NULL", Doc: "Access DAvg.", Example: `db.avg("Products", "Price")`},
		{Name: "min", Signature: "db.min(table, field, where?, params?) → value|db.NULL", Doc: "Access DMin.", Example: `db.min("Orders", "OrderDate")`},
		{Name: "max", Signature: "db.max(table, field, where?, params?) → value|db.NULL", Doc: "Access DMax.", Example: `db.max("Orders", "OrderDate")`},
		{Name: "is_null", Signature: "db.is_null(v) → boolean", Doc: "True for nil and db.NULL.", Example: `if db.is_null(record.ShippedAt) then … end`},
	},
	Values: []script.ValueDoc{{Name: "NULL", Type: "userdata", Doc: "SQL NULL. Lua tables cannot hold nil, so empty field values are db.NULL."}},
}

func data(r *script.Run, L *lua.LState) *host.Data {
	d, ok := r.Host.(*host.Data)
	if !ok || d == nil || d.Lease == nil {
		L.RaiseError("database access is not available here")
	}
	return d
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	t.RawSetString("NULL", r.Sentinel(script.NullName))
	fn := func(name string, f func(L *lua.LState) int) { t.RawSetString(name, L.NewFunction(f)) }
	fn("is_null", func(L *lua.LState) int {
		v := L.Get(1)
		L.Push(lua.LBool(v == lua.LNil || v == r.Sentinel(script.NullName)))
		return 1
	})
	fn("query", func(L *lua.LState) int {
		rows := readRows(r, L, L.CheckString(1), params(r, L, 2), MaxRows)
		out := L.CreateTable(len(rows), 0)
		for _, row := range rows {
			out.Append(row)
		}
		L.Push(out)
		return 1
	})
	fn("query_one", func(L *lua.LState) int {
		rows := readRows(r, L, L.CheckString(1), params(r, L, 2), 1)
		if len(rows) == 0 {
			L.Push(lua.LNil)
		} else {
			L.Push(rows[0])
		}
		return 1
	})
	fn("query_value", func(L *lua.LState) int {
		vals := readValues(r, L, L.CheckString(1), params(r, L, 2))
		if vals == nil {
			L.Push(lua.LNil)
		} else {
			L.Push(vals)
		}
		return 1
	})
	fn("iter", func(L *lua.LState) int {
		sql := L.CheckString(1)
		args := params(r, L, 2)
		offset := 0
		var buf []*lua.LTable
		done := false
		L.Push(L.NewFunction(func(L *lua.LState) int {
			if len(buf) == 0 && !done {
				buf = readRows(r, L, "SELECT * FROM ("+sql+") LIMIT 1000 OFFSET "+fmt.Sprint(offset), args, 1000)
				offset += len(buf)
				done = len(buf) < 1000
			}
			if len(buf) == 0 {
				return 0
			}
			L.Push(buf[0])
			buf = buf[1:]
			return 1
		}))
		return 1
	})
	fn("exec", func(L *lua.LState) int {
		res := execSQL(r, L, L.CheckString(1), params(r, L, 2))
		out := L.NewTable()
		out.RawSetString("changes", lua.LNumber(res.Changes))
		out.RawSetString("last_id", lua.LNumber(res.LastInsertID))
		L.Push(out)
		return 1
	})
	fn("transaction", func(L *lua.LState) int { return transaction(r, L) })
	agg := func(name, sqlFn string) {
		fn(name, func(L *lua.LState) int { return aggregate(r, L, sqlFn) })
	}
	fn("lookup", func(L *lua.LState) int { return aggregate(r, L, "") })
	agg("count", "count")
	agg("sum", "sum")
	agg("avg", "avg")
	agg("min", "min")
	agg("max", "max")
	return t
}

// params converts a Lua table at index i into named SQL parameters.
func params(r *script.Run, L *lua.LState, i int) []any {
	v := L.Get(i)
	if v == lua.LNil {
		return nil
	}
	t, ok := v.(*lua.LTable)
	if !ok {
		L.ArgError(i, "params must be a table")
	}
	var out []any
	var keys []string
	vals := map[string]any{}
	t.ForEach(func(k, val lua.LValue) {
		ks, ok := k.(lua.LString)
		if !ok {
			L.ArgError(i, "params keys must be names (use :name placeholders)")
		}
		g := script.FromLua(r, val)
		switch g.(type) {
		case script.Null:
			g = nil
		case nil, string, int64, float64, bool:
		default:
			L.ArgError(i, fmt.Sprintf("parameter %s must be a text, number, boolean or db.NULL", ks))
		}
		keys = append(keys, string(ks))
		vals[string(ks)] = g
	})
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, sqlitex.Named{Name: ":" + k, Value: vals[k]})
	}
	return out
}

func toLua(r *script.Run, v any) lua.LValue {
	if v == nil {
		return r.Sentinel(script.NullName)
	}
	if b, ok := v.([]byte); ok {
		return lua.LString(string(b))
	}
	return script.ToLua(r, v)
}

// withConn runs fn on the transaction connection (inside db.transaction) or a
// reader connection, under the authorizer.
func withConn(r *script.Run, L *lua.LState, write bool, fn func(ctx context.Context, c *sqlitex.Conn) error) {
	d := data(r, L)
	var denial query.Denial
	pol := query.Policy{Builder: d.Builder, Write: write}
	run := func(ctx context.Context, c *sqlitex.Conn) error {
		return c.WithAuthorizer(pol.Authorizer(&denial), func() error { return fn(ctx, c) })
	}
	var err error
	if tx, tctx, ok := d.Tx(); ok {
		err = run(tctx, tx.Conn)
	} else if write {
		err = d.Lease.Write(r.Ctx, priority(r), func(tx *sqlitex.Tx) error { return run(r.Ctx, tx.Conn) })
	} else {
		err = d.Lease.ReadConn(r.Ctx, func(c *sqlitex.Conn) error { return run(r.Ctx, c) })
	}
	if err != nil {
		r.RaiseFault(sqlError(&denial, err))
	}
}

func priority(r *script.Run) workspace.Priority {
	if r.Kind == script.Job {
		return workspace.Background
	}
	return workspace.Interactive
}

func sqlError(d *query.Denial, err error) error {
	var fe *fault.Error
	if errors.As(err, &fe) {
		return fe
	}
	var se *sqlite3.Error
	if errors.As(err, &se) && (se.Code() == sqlite3.AUTH || d.Reason != "") {
		reason := d.Reason
		if reason == "" {
			reason = "the statement uses an operation that is not permitted"
		}
		return &fault.Error{Code: fault.SQLNotAllowed, Message: "Not allowed: " + reason + ".", Cause: err}
	}
	if errors.As(err, &se) && se.Code() == sqlite3.ERROR {
		msg := se.Error()
		if i := strings.LastIndex(msg, ": "); i >= 0 {
			msg = msg[i+2:]
		}
		return fault.SQLError(msg, err)
	}
	if strings.Contains(err.Error(), "multiple statements") {
		return fault.New(fault.SQLNotAllowed, "Only one statement can run at a time.")
	}
	return err
}

func readRows(r *script.Run, L *lua.LState, sql string, args []any, limit int) []*lua.LTable {
	var out []*lua.LTable
	withConn(r, L, false, func(ctx context.Context, c *sqlitex.Conn) error {
		desc, err := c.Describe(ctx, sql)
		if err != nil {
			return err
		}
		if !desc.ReadOnly {
			return fault.New(fault.SQLNotAllowed, "db.query only reads; use db.exec to change data.")
		}
		rows, err := c.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols := rows.Columns()
		size := 0
		for rows.Next() {
			if len(out) >= limit {
				break
			}
			t := L.CreateTable(0, len(cols))
			for i, v := range rows.Values() {
				size += sqlitex.SizeOf(v)
				t.RawSetString(cols[i], toLua(r, v))
			}
			if size > MaxBytes {
				r.RaiseLimit(fmt.Sprintf("query result exceeds %d MiB", MaxBytes>>20))
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out
}

func readValues(r *script.Run, L *lua.LState, sql string, args []any) lua.LValue {
	var out lua.LValue
	withConn(r, L, false, func(ctx context.Context, c *sqlitex.Conn) error {
		desc, err := c.Describe(ctx, sql)
		if err != nil {
			return err
		}
		if !desc.ReadOnly {
			return fault.New(fault.SQLNotAllowed, "db.query_value only reads; use db.exec to change data.")
		}
		rows, err := c.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() && rows.ColumnCount() > 0 {
			out = toLua(r, rows.Value(0))
		}
		return rows.Err()
	})
	return out
}

func checkWrite(r *script.Run, L *lua.LState) *host.Data {
	d := data(r, L)
	if !r.Budget.Writes || !d.CanWrite {
		r.RaiseFault(fault.New(fault.SQLNotAllowed, "This script may not change data (insufficient role or read-only context)."))
	}
	return d
}

func execSQL(r *script.Run, L *lua.LState, sql string, args []any) sqlitex.Result {
	d := checkWrite(r, L)
	if err := query.CheckDML(sql); err != nil {
		r.RaiseFault(err)
	}
	var res sqlitex.Result
	var touched []string
	withConn(r, L, true, func(ctx context.Context, c *sqlitex.Conn) error {
		desc, err := c.Describe(ctx, sql)
		if err != nil {
			return err
		}
		if desc.ReadOnly {
			return fault.New(fault.BadRequest, "db.exec runs INSERT, UPDATE or DELETE; use db.query to read.")
		}
		res, err = c.Exec(ctx, sql, args...)
		if err != nil {
			return err
		}
		touched = writtenTables(sql)
		return nil
	})
	if len(touched) > 0 && d.Records != nil && d.Schemas != nil {
		ids := tableIDs(r, d, touched)
		ws := string(d.Lease.ID())
		d.AfterCommit(func() { d.Records.PublishInvalidated(ws, ids) })
	}
	return res
}

// writtenTables extracts candidate table names after INSERT INTO / UPDATE /
// DELETE FROM for change-feed invalidation (best effort; the authorizer is
// what enforces access).
func writtenTables(sql string) []string {
	f := strings.Fields(strings.NewReplacer("(", " ", ")", " ", ",", " ").Replace(sql))
	var out []string
	for i := 0; i+1 < len(f); i++ {
		w := strings.ToUpper(f[i])
		next := f[i+1]
		if (w == "INTO" || w == "UPDATE" || w == "FROM") && i+1 < len(f) {
			if strings.EqualFold(next, "OR") && i+3 < len(f) {
				next = f[i+3]
			}
			out = append(out, strings.Trim(next, `"`+"`[]"))
		}
	}
	return out
}

func tableIDs(r *script.Run, d *host.Data, names []string) []string {
	sch, err := d.Schemas.Get(r.Ctx, d.Lease)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if t, ok := sch.TableByPhysical(n); ok && !seen[t.ID] {
			seen[t.ID] = true
			out = append(out, t.ID)
		}
	}
	return out
}

func transaction(r *script.Run, L *lua.LState) int {
	fn := L.CheckFunction(1)
	d := checkWrite(r, L)
	if _, _, ok := d.Tx(); ok {
		L.RaiseError("db.transaction blocks cannot be nested")
	}
	if r.Budget.TxBlock <= 0 {
		L.RaiseError("transactions are not available in this context")
	}
	tctx, cancel := context.WithTimeout(r.Ctx, r.Budget.TxBlock)
	defer cancel()
	nret := 0
	var luaErr error
	err := d.Lease.Write(tctx, priority(r), func(tx *sqlitex.Tx) error {
		d.SetTx(tctx, tx)
		// The block's budget applies to Lua instructions too.
		prev := L.Context()
		L.SetContext(tctx)
		defer L.SetContext(prev)
		top := L.GetTop()
		L.Push(fn)
		if err := L.PCall(0, lua.MultRet, nil); err != nil {
			luaErr = err
			return err
		}
		nret = L.GetTop() - top
		return nil
	})
	d.FinishTx(err == nil)
	if err != nil {
		if tctx.Err() != nil && r.Ctx.Err() == nil {
			r.RaiseLimit(fmt.Sprintf("db.transaction exceeded its %s budget; nothing was saved", r.Budget.TxBlock))
		}
		if luaErr != nil {
			var apiErr *lua.ApiError
			if errors.As(luaErr, &apiErr) {
				L.Error(apiErr.Object, 0)
			}
			L.RaiseError("%s", luaErr.Error())
		}
		r.RaiseFault(err)
	}
	return nret
}

// aggregate implements lookup (sqlFn "") and the D-functions.
func aggregate(r *script.Run, L *lua.LState, sqlFn string) int {
	d := data(r, L)
	tableName := L.CheckString(1)
	field := L.OptString(2, "*")
	where := L.OptString(3, "")
	args := params(r, L, 4)
	sch, err := d.Schemas.Get(r.Ctx, d.Lease)
	if err != nil {
		r.RaiseFault(err)
	}
	t, ok := sch.TableByPhysical(tableName)
	if !ok {
		r.RaiseFault(fault.Newf(fault.BadRequest, "Table %q does not exist.", tableName))
	}
	expr := "*"
	if field != "*" {
		if field == "id" || field == "_version" {
			expr = catalog.Quote(field)
		} else if c, ok := t.ColumnByPhysical(field); ok {
			expr = catalog.Quote(c.PhysicalName)
		} else {
			r.RaiseFault(fault.Newf(fault.BadRequest, "Table %q has no field %q.", tableName, field))
		}
	}
	if expr == "*" && sqlFn != "count" {
		L.ArgError(2, "a field name is required")
	}
	sel := expr
	if sqlFn != "" {
		sel = sqlFn + "(" + expr + ")"
	}
	sql := "SELECT " + sel + " FROM " + catalog.Quote(t.PhysicalName)
	if where != "" {
		if err := catalog.CheckExprAllowParams(where); err != nil {
			r.RaiseFault(err)
		}
		sql += " WHERE (" + where + ")"
	}
	if sqlFn == "" {
		sql += " LIMIT 1"
	}
	v := readValues(r, L, sql, args)
	if v == nil {
		v = r.Sentinel(script.NullName)
	}
	L.Push(v)
	return 1
}

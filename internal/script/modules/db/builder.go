package db

// The neutral query builder (ADR 0013 §8): db.from, db.select, db.update and
// db.delete take a table, a where table and values, never SQL text, so a
// script that uses them runs the same on SQLite and PostgreSQL. They go
// through the records layer: reads are records.list's, and writes update or
// delete each matching row as records.update and records.delete do, with
// validation, row history, version checks and change events, inside
// db.transaction's transaction when there is one.

import (
	"context"
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	rec "abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	recmod "abidata/internal/script/modules/records"
)

const (
	// DefaultSelectLimit is db.select's page size when limit is not given.
	DefaultSelectLimit = 100
	// MaxBuilderRows caps the rows one db.update or db.delete may change.
	MaxBuilderRows = 10000
)

var whereOps = map[string]rec.FilterOp{
	"eq": rec.OpEq, "ne": rec.OpNe, "lt": rec.OpLt, "le": rec.OpLe, "gt": rec.OpGt, "ge": rec.OpGe,
	"contains": rec.OpContains, "starts": rec.OpStarts, "ends": rec.OpEnds, "in": rec.OpIn,
	"not_in": rec.OpNotIn, "between": rec.OpBetween,
}

var symbolOps = map[string]rec.FilterOp{
	"=": rec.OpEq, "==": rec.OpEq, "!=": rec.OpNe, "<>": rec.OpNe,
	"<": rec.OpLt, "<=": rec.OpLe, ">": rec.OpGt, ">=": rec.OpGe,
}

// builderTable resolves the table a builder call names.
func builderTable(r *script.Run, d *host.Data, name string) (*catalog.Schema, *catalog.Table) {
	if d.Records == nil || d.Schemas == nil {
		r.RaiseFault(fault.New(fault.BadRequest, "Record access is not available here."))
	}
	sch, err := d.Schemas.Get(r.Ctx, d.Lease)
	if err != nil {
		r.RaiseFault(err)
	}
	t, ok := sch.TableByPhysical(name)
	if !ok {
		r.RaiseFault(fault.Newf(fault.BadRequest, "Table %q does not exist.", name))
	}
	return sch, t
}

func resolveCol(t *catalog.Table, name string) (string, error) {
	if name == "id" {
		return "id", nil
	}
	c, ok := t.ColumnByPhysical(name)
	if !ok {
		return "", fmt.Errorf("table %q has no field %q", t.PhysicalName, name)
	}
	return c.ID, nil
}

// field reads a string field of a builder call's argument table.
func field(L *lua.LState, spec *lua.LTable, name string, required bool) string {
	v := spec.RawGetString(name)
	if v == lua.LNil {
		if required {
			L.ArgError(1, name+" is required")
		}
		return ""
	}
	s, ok := v.(lua.LString)
	if !ok {
		L.ArgError(1, name+" must be a string")
	}
	return string(s)
}

// whereFilter turns a where table into a records filter.
func whereFilter(r *script.Run, L *lua.LState, t *catalog.Table, v lua.LValue, depth int) *rec.Filter {
	return recmod.WhereFilter(r, L, t, v, depth)
}

// cond is one condition; db.NULL compared for equality means empty.
func cond(r *script.Run, col string, op rec.FilterOp, v lua.LValue) rec.Filter {
	return recmod.Cond(r, col, op, v)
}

// orderSorts turns order = {"Name", "-Total"} into sorts.
func orderSorts(L *lua.LState, t *catalog.Table, v lua.LValue) []rec.Sort {
	if v == lua.LNil {
		return nil
	}
	ot, ok := v.(*lua.LTable)
	if !ok {
		L.ArgError(1, `order must be a list such as {"Name", "-Total"}`)
	}
	var out []rec.Sort
	for i := 1; i <= ot.Len(); i++ {
		s, ok := ot.RawGetInt(i).(lua.LString)
		if !ok {
			L.ArgError(1, "order entries must be field names")
		}
		name, desc := strings.CutPrefix(string(s), "-")
		col, err := resolveCol(t, name)
		if err != nil {
			L.ArgError(1, err.Error())
		}
		out = append(out, rec.Sort{Col: col, Desc: desc})
	}
	return out
}

func intField(L *lua.LState, spec *lua.LTable, name string, def int) int {
	v := spec.RawGetString(name)
	if v == lua.LNil {
		return def
	}
	n, ok := v.(lua.LNumber)
	if !ok || float64(n) != float64(int(n)) || n < 0 {
		L.ArgError(1, name+" must be a whole number")
	}
	return int(n)
}

// builderSelect is db.select{from, where?, order?, limit?, offset?}.
func builderSelect(r *script.Run, L *lua.LState) int {
	d := data(r, L)
	spec := L.CheckTable(1)
	sch, t := builderTable(r, d, field(L, spec, "from", true))
	q := rec.ListQuery{
		Filter: whereFilter(r, L, t, spec.RawGetString("where"), 1),
		Sort:   orderSorts(L, t, spec.RawGetString("order")),
		Limit:  intField(L, spec, "limit", DefaultSelectLimit),
		Offset: intField(L, spec, "offset", 0),
	}
	if q.Limit == 0 || q.Limit > rec.MaxPage {
		L.ArgError(1, fmt.Sprintf("limit must be between 1 and %d", rec.MaxPage))
	}
	var rows []rec.Row
	var err error
	if tx, tctx, ok := d.Tx(); ok {
		rows, _, err = rec.ListTx(tctx, tx, sch, t, q)
	} else {
		rows, _, err = d.Records.List(r.Ctx, d.Lease, t.ID, q)
	}
	if err != nil {
		r.RaiseFault(err)
	}
	out := L.CreateTable(len(rows), 0)
	for _, row := range rows {
		out.Append(recmod.RowToLua(r, t, row))
	}
	L.Push(out)
	return 1
}

// builderWrite is db.update{table, set, where} and db.delete{table, where}.
func builderWrite(r *script.Run, L *lua.LState, del bool) int {
	d := checkWrite(r, L)
	spec := L.CheckTable(1)
	sch, t := builderTable(r, d, field(L, spec, "table", true))
	wv := spec.RawGetString("where")
	if wv == lua.LNil {
		L.ArgError(1, "where is required; pass where = {} to change every row")
	}
	filter := whereFilter(r, L, t, wv, 1)
	var fields map[string]any
	if !del {
		set, ok := spec.RawGetString("set").(*lua.LTable)
		if !ok {
			L.ArgError(1, "set must be a table of field values")
		}
		L.Push(set)
		fields = recmod.ValuesFromLua(r, L, t, L.GetTop())
		L.Pop(1)
		if len(fields) == 0 {
			L.ArgError(1, "set has no fields")
		}
	}
	actor := rec.Actor{UserID: d.UserID}
	ws := string(d.Lease.ID())
	var changed int
	run := func(ctx context.Context, tx dbx.Tx) error {
		changed = 0
		var match []rec.Row
		for {
			page, more, err := rec.ListTx(ctx, tx, sch, t, rec.ListQuery{Filter: filter, Limit: rec.MaxPage, Offset: len(match)})
			if err != nil {
				return err
			}
			match = append(match, page...)
			if len(match) > MaxBuilderRows {
				return fault.Newf(fault.BadRequest, "More than %d rows match; narrow the where.", MaxBuilderRows)
			}
			if !more {
				break
			}
		}
		for _, row := range match {
			var res rec.TxResult
			var err error
			if del {
				res, err = d.Records.DeleteTx(ctx, tx, ws, sch, t, row.ID, row.Version, actor)
			} else {
				res, err = d.Records.UpdateTx(ctx, tx, ws, sch, t, row.ID, row.Version, fields, actor)
			}
			if err != nil {
				return err
			}
			d.AfterCommit(res.Publish)
			changed++
		}
		return nil
	}
	if err := d.Write(r.Ctx, priority(r), run); err != nil {
		r.RaiseFault(err)
	}
	out := L.NewTable()
	out.RawSetString("changes", lua.LNumber(changed))
	L.Push(out)
	return 1
}

// -----------------------------------------------------------------------------
// Fluent Query Builder: db.from(table)
// -----------------------------------------------------------------------------

type queryBuilder struct {
	r       *script.Run
	d       *host.Data
	sch     *catalog.Schema
	t       *catalog.Table
	filters []rec.Filter
	sorts   []rec.Sort
	limit   int
	offset  int
}

func (qb *queryBuilder) combinedFilter() *rec.Filter {
	if len(qb.filters) == 0 {
		return nil
	}
	if len(qb.filters) == 1 {
		return &qb.filters[0]
	}
	return &rec.Filter{And: qb.filters}
}

func (qb *queryBuilder) executeRows(L *lua.LState, limit int) []rec.Row {
	q := rec.ListQuery{
		Filter: qb.combinedFilter(),
		Sort:   qb.sorts,
		Limit:  limit,
		Offset: qb.offset,
	}
	var rows []rec.Row
	var err error
	if tx, tctx, ok := qb.d.Tx(); ok {
		rows, _, err = rec.ListTx(tctx, tx, qb.sch, qb.t, q)
	} else {
		rows, _, err = qb.d.Records.List(qb.r.Ctx, qb.d.Lease, qb.t.ID, q)
	}
	if err != nil {
		qb.r.RaiseFault(err)
	}
	return rows
}

// executeAllRows reads every matching row, a page at a time (a list returns
// at most rec.MaxPage rows), for the aggregates. It ignores limit and offset
// and refuses more than MaxBuilderRows matches.
func (qb *queryBuilder) executeAllRows() []rec.Row {
	var all []rec.Row
	for {
		q := rec.ListQuery{Filter: qb.combinedFilter(), Limit: rec.MaxPage, Offset: len(all)}
		var page []rec.Row
		var more bool
		var err error
		if tx, tctx, ok := qb.d.Tx(); ok {
			page, more, err = rec.ListTx(tctx, tx, qb.sch, qb.t, q)
		} else {
			page, more, err = qb.d.Records.List(qb.r.Ctx, qb.d.Lease, qb.t.ID, q)
		}
		if err != nil {
			qb.r.RaiseFault(err)
		}
		all = append(all, page...)
		if len(all) > MaxBuilderRows {
			qb.r.RaiseFault(fault.Newf(fault.BadRequest, "More than %d rows match; narrow the where.", MaxBuilderRows))
		}
		if !more {
			return all
		}
	}
}

func (qb *queryBuilder) executeCount() int64 {
	f := qb.combinedFilter()
	var n int64
	var err error
	if tx, tctx, ok := qb.d.Tx(); ok {
		n, _, err = rec.CountTx(tctx, tx, qb.t, f)
	} else {
		n, _, err = qb.d.Records.Count(qb.r.Ctx, qb.d.Lease, qb.t.ID, f)
	}
	if err != nil {
		qb.r.RaiseFault(err)
	}
	return n
}

func toAPIList(r *script.Run, lt *lua.LTable) []any {
	n := lt.Len()
	out := make([]any, 0, n)
	for i := 1; i <= n; i++ {
		v := script.FromLua(r, lt.RawGetInt(i))
		out = append(out, recmod.ToAPI(v))
	}
	return out
}

func builderFrom(r *script.Run, L *lua.LState) int {
	d := data(r, L)
	tableName := L.CheckString(1)
	sch, t := builderTable(r, d, tableName)

	qb := &queryBuilder{
		r:      r,
		d:      d,
		sch:    sch,
		t:      t,
		limit:  DefaultSelectLimit,
		offset: 0,
	}

	tbl := L.NewTable()

	// Helper to check self and get method arg starting index
	argIdx := func(L *lua.LState) int {
		if L.GetTop() >= 1 && L.Get(1) == tbl {
			return 2
		}
		return 1
	}

	method := func(name string, fn lua.LGFunction) {
		tbl.RawSetString(name, L.NewFunction(fn))
	}

	// :where(...)
	method("where", func(L *lua.LState) int {
		start := argIdx(L)
		top := L.GetTop()
		nargs := top - start + 1
		if nargs == 1 {
			// table where
			wv := L.CheckTable(start)
			if f := whereFilter(r, L, qb.t, wv, 1); f != nil {
				qb.filters = append(qb.filters, *f)
			}
		} else if nargs == 2 {
			colName := L.CheckString(start)
			colID, err := resolveCol(qb.t, colName)
			if err != nil {
				L.ArgError(start, err.Error())
			}
			qb.filters = append(qb.filters, cond(r, colID, rec.OpEq, L.Get(start+1)))
		} else if nargs >= 3 {
			colName := L.CheckString(start)
			opStr := L.CheckString(start + 1)
			val := L.Get(start + 2)
			colID, err := resolveCol(qb.t, colName)
			if err != nil {
				L.ArgError(start, err.Error())
			}
			op, ok := symbolOps[opStr]
			if !ok {
				op, ok = whereOps[opStr]
			}
			if !ok {
				L.ArgError(start+1, fmt.Sprintf("unknown operator %q", opStr))
			}
			qb.filters = append(qb.filters, cond(r, colID, op, val))
		}
		L.Push(tbl)
		return 1
	})

	// :where_in(col, list)
	method("where_in", func(L *lua.LState) int {
		start := argIdx(L)
		colName := L.CheckString(start)
		list := L.CheckTable(start + 1)
		colID, err := resolveCol(qb.t, colName)
		if err != nil {
			L.ArgError(start, err.Error())
		}
		qb.filters = append(qb.filters, rec.Filter{Col: colID, Op: rec.OpIn, Value: toAPIList(r, list)})
		L.Push(tbl)
		return 1
	})

	// :where_not_in(col, list)
	method("where_not_in", func(L *lua.LState) int {
		start := argIdx(L)
		colName := L.CheckString(start)
		list := L.CheckTable(start + 1)
		colID, err := resolveCol(qb.t, colName)
		if err != nil {
			L.ArgError(start, err.Error())
		}
		qb.filters = append(qb.filters, rec.Filter{Col: colID, Op: rec.OpNotIn, Value: toAPIList(r, list)})
		L.Push(tbl)
		return 1
	})

	// :where_null(col)
	method("where_null", func(L *lua.LState) int {
		start := argIdx(L)
		colName := L.CheckString(start)
		colID, err := resolveCol(qb.t, colName)
		if err != nil {
			L.ArgError(start, err.Error())
		}
		qb.filters = append(qb.filters, rec.Filter{Col: colID, Op: rec.OpIsNull})
		L.Push(tbl)
		return 1
	})

	// :where_not_null(col)
	method("where_not_null", func(L *lua.LState) int {
		start := argIdx(L)
		colName := L.CheckString(start)
		colID, err := resolveCol(qb.t, colName)
		if err != nil {
			L.ArgError(start, err.Error())
		}
		qb.filters = append(qb.filters, rec.Filter{Col: colID, Op: rec.OpNotNull})
		L.Push(tbl)
		return 1
	})

	// :where_between(col, low, high)
	method("where_between", func(L *lua.LState) int {
		start := argIdx(L)
		colName := L.CheckString(start)
		low := script.FromLua(r, L.Get(start+1))
		high := script.FromLua(r, L.Get(start+2))
		colID, err := resolveCol(qb.t, colName)
		if err != nil {
			L.ArgError(start, err.Error())
		}
		qb.filters = append(qb.filters, rec.Filter{
			Col:   colID,
			Op:    rec.OpBetween,
			Value: []any{recmod.ToAPI(low), recmod.ToAPI(high)},
		})
		L.Push(tbl)
		return 1
	})

	// :where_not(col, val)
	method("where_not", func(L *lua.LState) int {
		start := argIdx(L)
		colName := L.CheckString(start)
		val := L.Get(start + 1)
		colID, err := resolveCol(qb.t, colName)
		if err != nil {
			L.ArgError(start, err.Error())
		}
		qb.filters = append(qb.filters, cond(r, colID, rec.OpNe, val))
		L.Push(tbl)
		return 1
	})

	// :order(...)
	method("order", func(L *lua.LState) int {
		start := argIdx(L)
		v := L.Get(start)
		if vt, ok := v.(*lua.LTable); ok {
			qb.sorts = append(qb.sorts, orderSorts(L, qb.t, vt)...)
		} else {
			for i := start; i <= L.GetTop(); i++ {
				s := L.CheckString(i)
				name, desc := strings.CutPrefix(s, "-")
				colID, err := resolveCol(qb.t, name)
				if err != nil {
					L.ArgError(i, err.Error())
				}
				qb.sorts = append(qb.sorts, rec.Sort{Col: colID, Desc: desc})
			}
		}
		L.Push(tbl)
		return 1
	})

	// :limit(n)
	method("limit", func(L *lua.LState) int {
		start := argIdx(L)
		n := L.CheckInt(start)
		if n <= 0 || n > rec.MaxPage {
			L.ArgError(start, fmt.Sprintf("limit must be between 1 and %d", rec.MaxPage))
		}
		qb.limit = n
		L.Push(tbl)
		return 1
	})

	// :offset(n)
	method("offset", func(L *lua.LState) int {
		start := argIdx(L)
		n := L.CheckInt(start)
		if n < 0 {
			L.ArgError(start, "offset cannot be negative")
		}
		qb.offset = n
		L.Push(tbl)
		return 1
	})

	// :all() and :select()
	execAll := func(L *lua.LState) int {
		rows := qb.executeRows(L, qb.limit)
		out := L.CreateTable(len(rows), 0)
		for _, row := range rows {
			out.Append(recmod.RowToLua(r, qb.t, row))
		}
		L.Push(out)
		return 1
	}
	method("all", execAll)
	method("select", execAll)

	// :first()
	method("first", func(L *lua.LState) int {
		rows := qb.executeRows(L, 1)
		if len(rows) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(recmod.RowToLua(r, qb.t, rows[0]))
		return 1
	})

	// :count()
	method("count", func(L *lua.LState) int {
		n := qb.executeCount()
		L.Push(lua.LNumber(n))
		return 1
	})

	// :exists()
	method("exists", func(L *lua.LState) int {
		rows := qb.executeRows(L, 1)
		L.Push(lua.LBool(len(rows) > 0))
		return 1
	})

	// :pluck(col)
	method("pluck", func(L *lua.LState) int {
		start := argIdx(L)
		colName := L.CheckString(start)
		colID, err := resolveCol(qb.t, colName)
		if err != nil {
			L.ArgError(start, err.Error())
		}
		rows := qb.executeRows(L, qb.limit)
		out := L.CreateTable(len(rows), 0)
		for _, row := range rows {
			if colName == "id" {
				out.Append(lua.LNumber(row.ID))
			} else {
				col, _ := qb.t.Column(colID)
				out.Append(recmod.ValueToLua(r, *col, row.Fields[colID]))
			}
		}
		L.Push(out)
		return 1
	})

	// Aggregations: :sum(col), :avg(col), :min(col), :max(col)
	calcAgg := func(fn string) lua.LGFunction {
		return func(L *lua.LState) int {
			start := argIdx(L)
			colName := L.CheckString(start)
			colID, err := resolveCol(qb.t, colName)
			if err != nil {
				L.ArgError(start, err.Error())
			}
			rows := qb.executeAllRows()
			if len(rows) == 0 {
				if fn == "sum" {
					L.Push(lua.LNumber(0))
				} else {
					L.Push(lua.LNil)
				}
				return 1
			}

			col, _ := qb.t.Column(colID)
			var sum float64
			var count int
			var minVal, maxVal float64
			first := true

			for _, row := range rows {
				var val any
				if colName == "id" {
					val = float64(row.ID)
				} else if col != nil {
					val = row.Fields[colID]
				}
				if val == nil {
					continue
				}
				var num float64
				switch v := val.(type) {
				case int64:
					num = float64(v)
				case float64:
					num = v
				default:
					continue
				}
				sum += num
				count++
				if first {
					minVal = num
					maxVal = num
					first = false
				} else {
					if num < minVal {
						minVal = num
					}
					if num > maxVal {
						maxVal = num
					}
				}
			}

			if count == 0 {
				if fn == "sum" {
					L.Push(lua.LNumber(0))
				} else {
					L.Push(lua.LNil)
				}
				return 1
			}

			switch fn {
			case "sum":
				L.Push(lua.LNumber(sum))
			case "avg":
				L.Push(lua.LNumber(sum / float64(count)))
			case "min":
				L.Push(lua.LNumber(minVal))
			case "max":
				L.Push(lua.LNumber(maxVal))
			default:
				L.Push(lua.LNil)
			}
			return 1
		}
	}

	method("sum", calcAgg("sum"))
	method("avg", calcAgg("avg"))
	method("min", calcAgg("min"))
	method("max", calcAgg("max"))

	L.Push(tbl)
	return 1
}

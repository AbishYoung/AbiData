package db

// The neutral query builder (ADR 0013 §8): db.select, db.update and
// db.delete take a table, a where table and values, never SQL text, so a
// script that uses them runs the same on SQLite and PostgreSQL. They go
// through the records layer: reads are records.list's, and writes update or
// delete each matching row as records.update and records.delete do, with
// validation, row history, version checks and change events, inside
// db.transaction's transaction when there is one.

import (
	"context"
	"fmt"
	"sort"
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
	"contains": rec.OpContains, "starts": rec.OpStarts, "ends": rec.OpEnds, "in": rec.OpIn, "between": rec.OpBetween,
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

// whereFilter turns a where table into a records filter. Each key is a field
// (or id) and is ANDed: a value means equal (db.NULL: empty), and a table of
// operators ({gt = 3, le = 9}) means each of them. _or takes a list of where
// tables, any of which may match.
func whereFilter(r *script.Run, L *lua.LState, t *catalog.Table, v lua.LValue, depth int) *rec.Filter {
	if v == lua.LNil {
		return nil
	}
	wt, ok := v.(*lua.LTable)
	if !ok {
		L.ArgError(1, "where must be a table")
	}
	if depth > rec.MaxFilterDepth {
		L.ArgError(1, "where is nested too deeply")
	}
	var keys []string
	wt.ForEach(func(k, _ lua.LValue) {
		ks, ok := k.(lua.LString)
		if !ok {
			L.ArgError(1, "where keys must be field names")
		}
		keys = append(keys, string(ks))
	})
	sort.Strings(keys) // a stable filter, whatever the table's order
	var and []rec.Filter
	for _, k := range keys {
		val := wt.RawGetString(k)
		if k == "_or" {
			list, ok := val.(*lua.LTable)
			if !ok || list.Len() == 0 {
				L.ArgError(1, "_or must be a list of where tables")
			}
			var or []rec.Filter
			for i := 1; i <= list.Len(); i++ {
				if f := whereFilter(r, L, t, list.RawGetInt(i), depth+1); f != nil {
					or = append(or, *f)
				}
			}
			and = append(and, rec.Filter{Or: or})
			continue
		}
		col := k
		if k != "id" {
			c, ok := t.ColumnByPhysical(k)
			if !ok {
				L.ArgError(1, fmt.Sprintf("table %q has no field %q", t.PhysicalName, k))
			}
			col = c.ID
		}
		ops, isOps := val.(*lua.LTable)
		if !isOps {
			and = append(and, cond(r, col, rec.OpEq, val))
			continue
		}
		if isList(ops) { // {Status = {"Open", "New"}}: any of them
			and = append(and, cond(r, col, rec.OpIn, val))
			continue
		}
		var names []string
		ops.ForEach(func(op, _ lua.LValue) { names = append(names, op.String()) })
		sort.Strings(names)
		for _, name := range names {
			arg := ops.RawGetString(name)
			if name == "is_null" {
				op := rec.OpIsNull
				if arg == lua.LFalse {
					op = rec.OpNotNull
				}
				and = append(and, rec.Filter{Col: col, Op: op})
				continue
			}
			op, ok := whereOps[name]
			if !ok {
				L.ArgError(1, fmt.Sprintf("unknown operator %q for %s (use eq, ne, lt, le, gt, ge, contains, starts, ends, in, between or is_null)", name, k))
			}
			and = append(and, cond(r, col, op, arg))
		}
	}
	if len(and) == 0 {
		return nil
	}
	if len(and) == 1 {
		return &and[0]
	}
	return &rec.Filter{And: and}
}

// cond is one condition; db.NULL compared for equality means empty.
func cond(r *script.Run, col string, op rec.FilterOp, v lua.LValue) rec.Filter {
	g := script.FromLua(r, v)
	if _, null := g.(script.Null); null && (op == rec.OpEq || op == rec.OpNe) {
		if op == rec.OpEq {
			return rec.Filter{Col: col, Op: rec.OpIsNull}
		}
		return rec.Filter{Col: col, Op: rec.OpNotNull}
	}
	if list, ok := g.([]any); ok {
		for i := range list {
			list[i] = recmod.ToAPI(list[i])
		}
		return rec.Filter{Col: col, Op: op, Value: list}
	}
	return rec.Filter{Col: col, Op: op, Value: recmod.ToAPI(g)}
}

func isList(t *lua.LTable) bool { return t.Len() > 0 }

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
		col := name
		if name != "id" {
			c, ok := t.ColumnByPhysical(name)
			if !ok {
				L.ArgError(1, fmt.Sprintf("table %q has no field %q", t.PhysicalName, name))
			}
			col = c.ID
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
	q := rec.ListQuery{Filter: whereFilter(r, L, t, spec.RawGetString("where"), 1), Sort: orderSorts(L, t, spec.RawGetString("order")),
		Limit: intField(L, spec, "limit", DefaultSelectLimit), Offset: intField(L, spec, "offset", 0)}
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
// where is required (pass {} to mean every row).
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
	var err error
	if tx, tctx, ok := d.Tx(); ok {
		err = run(tctx, tx)
	} else {
		// Publish hooks queue on a transaction of their own, run on commit.
		d.SetTx(r.Ctx, nil)
		err = d.Lease.Write(r.Ctx, priority(r), func(tx dbx.Tx) error {
			d.SetTx(r.Ctx, tx)
			return run(r.Ctx, tx)
		})
		d.FinishTx(err == nil)
	}
	if err != nil {
		r.RaiseFault(err)
	}
	out := L.NewTable()
	out.RawSetString("changes", lua.LNumber(changed))
	L.Push(out)
	return 1
}

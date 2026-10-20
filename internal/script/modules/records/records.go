// Package records is the Lua `records` module: typed row reads and writes
// through the row API (OCC, validation messages, row history, change feed).
package records

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/geo"
	rec "abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	"abidata/internal/workspace"
)

func priority(r *script.Run) workspace.Priority {
	if r.Kind == script.Job {
		return workspace.Background
	}
	return workspace.Interactive
}

func init() { script.Register(script.ModuleDef{Doc: doc, Build: build}) }

var whereOps = map[string]rec.FilterOp{
	"eq": rec.OpEq, "ne": rec.OpNe, "lt": rec.OpLt, "le": rec.OpLe, "gt": rec.OpGt, "ge": rec.OpGe,
	"contains": rec.OpContains, "starts": rec.OpStarts, "ends": rec.OpEnds, "in": rec.OpIn,
	"not_in": rec.OpNotIn, "between": rec.OpBetween,
}

var doc = script.ModuleDoc{
	Name:     "records",
	Doc:      "Read and change rows field by field, with the same validation, concurrency checks and history as the datasheet. Fields are named by their field (physical) names.",
	Contexts: []script.Context{script.UIEvent, script.Report, script.Job},
	Funcs: []script.FuncDoc{
		{Name: "get", Signature: "records.get(table, id) → row|nil", Returns: "row table (id, _version and every field; currency as decimal, yes/no as boolean, empty as db.NULL) or nil",
			Doc: "Reads one row by id.", Example: `local c = records.get("Customers", record.Customer)`},
		{Name: "find_one", Signature: "records.find_one(table, where) → row|nil", Returns: "first matching row table or nil",
			Doc: "Finds the first row matching the where filter.", Example: `local u = records.find_one("Customers", {Email = "alice@example.com"})`},
		{Name: "count", Signature: "records.count(table, where?) → number", Returns: "integer count of matching rows",
			Doc: "Counts rows matching the where filter (or all rows if where is omitted).", Example: `local n = records.count("Orders", {Status = "Open"})`},
		{Name: "exists", Signature: "records.exists(table, where?) → boolean", Returns: "true if at least one matching row exists",
			Doc: "Reports whether any rows match the where filter.", Example: `if records.exists("Customers", {City = "Paris"}) then … end`},
		{Name: "insert", Signature: "records.insert(table, values) → id", Errors: []string{"REQUIRED_FIELD", "VALIDATION_FAILED", "DUPLICATE_VALUE", "LINKED_RECORDS"},
			Doc: "Creates a row and returns its id.", Example: `local id = records.insert("Notes", {Text = "Called", Customer = record.id})`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "insert_many", Signature: "records.insert_many(table, rows) → ids", Errors: []string{"REQUIRED_FIELD", "VALIDATION_FAILED", "DUPLICATE_VALUE", "LINKED_RECORDS"},
			Doc: "Creates multiple rows inside a transaction and returns an array of their ids.", Example: `local ids = records.insert_many("Items", {{Name = "A"}, {Name = "B"}})`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "update", Signature: "records.update(table, id, values, {version?}) → version", Errors: []string{"CONCURRENT_MODIFICATION", "VALIDATION_FAILED", "NOT_FOUND"},
			Doc: "Changes fields of a row. With version, fails if the row changed since that version; without it, applies to the current version.", Example: `records.update("Orders", id, {Status = "Shipped"})`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "update_many", Signature: "records.update_many(table, where, values) → {changes}", Errors: []string{"VALIDATION_FAILED", "BAD_REQUEST"},
			Doc: "Updates every row matching where with the given field values.", Example: `records.update_many("Orders", {Status = "Open"}, {Status = "Processing"})`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "upsert", Signature: "records.upsert(table, match, values) → id", Errors: []string{"VALIDATION_FAILED", "REQUIRED_FIELD"},
			Doc: "Finds a row matching match and updates it with values, or inserts a new row merging match and values.", Example: `records.upsert("Counters", {Key = "views"}, {Count = 1})`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "delete", Signature: "records.delete(table, id, {version?})", Errors: []string{"CONCURRENT_MODIFICATION", "LINKED_RECORDS", "NOT_FOUND"},
			Doc: "Deletes a row.", Example: `records.delete("Drafts", id)`, Contexts: []script.Context{script.UIEvent, script.Job}},
	},
}

type ctxt struct {
	r *script.Run
	L *lua.LState
	d *host.Data
}

func (c ctxt) table(name string) (*catalog.Schema, *catalog.Table) {
	sch, err := c.d.Schemas.Get(c.r.Ctx, c.d.Lease)
	if err != nil {
		c.r.RaiseFault(err)
	}
	t, ok := sch.TableByPhysical(name)
	if !ok {
		c.r.RaiseFault(fault.Newf(fault.BadRequest, "Table %q does not exist.", name))
	}
	return sch, t
}

func get(r *script.Run, L *lua.LState) ctxt {
	d, ok := r.Host.(*host.Data)
	if !ok || d == nil || d.Lease == nil || d.Records == nil {
		L.RaiseError("record access is not available here")
	}
	return ctxt{r, L, d}
}

func (c ctxt) checkWrite() {
	if !c.r.Budget.Writes || !c.d.CanWrite {
		c.r.RaiseFault(fault.New(fault.Forbidden, "This script may not change data (insufficient role or read-only context)."))
	}
}

// WhereFilter turns a where table into a records filter.
func WhereFilter(r *script.Run, L *lua.LState, t *catalog.Table, v lua.LValue, depth int) *rec.Filter {
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
	sort.Strings(keys)
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
				if f := WhereFilter(r, L, t, list.RawGetInt(i), depth+1); f != nil {
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
			and = append(and, Cond(r, col, rec.OpEq, val))
			continue
		}
		if ops.Len() > 0 { // array of values
			and = append(and, Cond(r, col, rec.OpIn, val))
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
				L.ArgError(1, fmt.Sprintf("unknown operator %q for %s (use eq, ne, lt, le, gt, ge, contains, starts, ends, in, not_in, between or is_null)", name, k))
			}
			and = append(and, Cond(r, col, op, arg))
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

// Cond builds one filter condition.
func Cond(r *script.Run, col string, op rec.FilterOp, v lua.LValue) rec.Filter {
	g := script.FromLua(r, v)
	if _, null := g.(script.Null); null && (op == rec.OpEq || op == rec.OpNe) {
		if op == rec.OpEq {
			return rec.Filter{Col: col, Op: rec.OpIsNull}
		}
		return rec.Filter{Col: col, Op: rec.OpNotNull}
	}
	if list, ok := g.([]any); ok {
		for i := range list {
			list[i] = ToAPI(list[i])
		}
		return rec.Filter{Col: col, Op: op, Value: list}
	}
	return rec.Filter{Col: col, Op: op, Value: ToAPI(g)}
}

// RowToLua converts an API row to a Lua table keyed by physical field names.
func RowToLua(r *script.Run, t *catalog.Table, row rec.Row) *lua.LTable {
	L := r.L
	out := L.CreateTable(0, len(t.Columns)+1)
	out.RawSetString("id", lua.LNumber(row.ID))
	out.RawSetString("_version", lua.LNumber(row.Version))
	for _, c := range t.Columns {
		if c.Type == catalog.Autonumber {
			continue
		}
		out.RawSetString(c.PhysicalName, ValueToLua(r, c, row.Fields[c.ID]))
	}
	return out
}

// ValueToLua converts one API field value to Lua for column c.
func ValueToLua(r *script.Run, c catalog.Column, v any) lua.LValue {
	if v == nil {
		return r.Sentinel(script.NullName)
	}
	ft := c.Type
	if ft == catalog.Calculated {
		ft = rec.ResultFieldType(c.Options.ResultType)
	}
	if ids, ok := v.([]string); ok {
		t := r.L.CreateTable(len(ids), 0)
		for _, id := range ids {
			t.Append(lua.LString(id))
		}
		return t
	}
	if ft == catalog.Currency {
		if s, ok := v.(string); ok {
			return script.DecimalFactory(r, s)
		}
	}
	if raw, ok := v.(geo.Raw); ok {
		return script.ToLua(r, raw.Object())
	}
	return script.ToLua(r, v)
}

// ValuesFromLua converts a Lua table keyed by physical names into API fields
// keyed by column ID.
func ValuesFromLua(r *script.Run, L *lua.LState, t *catalog.Table, idx int) map[string]any {
	lt := L.CheckTable(idx)
	out := map[string]any{}
	lt.ForEach(func(k, v lua.LValue) {
		name, ok := k.(lua.LString)
		if !ok {
			L.ArgError(idx, "field names must be strings")
		}
		col, ok := t.ColumnByPhysical(string(name))
		if !ok {
			L.ArgError(idx, fmt.Sprintf("table %q has no field %q", t.PhysicalName, string(name)))
		}
		out[col.ID] = ToAPI(script.FromLua(r, v))
	})
	return out
}

// ToAPI converts a value from Lua to the API representation records take.
func ToAPI(v any) any {
	switch x := v.(type) {
	case script.Null:
		return nil
	case int64:
		return json.Number(strconv.FormatInt(x, 10))
	case float64:
		return json.Number(strconv.FormatFloat(x, 'f', -1, 64))
	}
	return v
}

func versionOpt(L *lua.LState, idx int) int64 {
	if L.Get(idx) == lua.LNil {
		return 0
	}
	opts := L.CheckTable(idx)
	if v, ok := opts.RawGetString("version").(lua.LNumber); ok {
		return int64(v)
	}
	return 0
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()

	t.RawSetString("get", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		sch, tb := c.table(L.CheckString(1))
		id := L.CheckInt64(2)
		var row rec.Row
		var err error
		if tx, tctx, ok := c.d.Tx(); ok {
			row, err = rec.GetTx(tctx, tx, sch, tb, id)
		} else {
			row, err = c.d.Records.Get(r.Ctx, c.d.Lease, tb.ID, id)
		}
		if err != nil {
			if fault.Classify(err).Code == fault.NotFound {
				L.Push(lua.LNil)
				return 1
			}
			r.RaiseFault(err)
		}
		L.Push(RowToLua(r, tb, row))
		return 1
	}))

	t.RawSetString("find_one", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		sch, tb := c.table(L.CheckString(1))
		var f *rec.Filter
		if L.GetTop() >= 2 && L.Get(2) != lua.LNil {
			f = WhereFilter(r, L, tb, L.CheckTable(2), 1)
		}
		q := rec.ListQuery{Filter: f, Limit: 1}
		var rows []rec.Row
		var err error
		if tx, tctx, ok := c.d.Tx(); ok {
			rows, _, err = rec.ListTx(tctx, tx, sch, tb, q)
		} else {
			rows, _, err = c.d.Records.List(r.Ctx, c.d.Lease, tb.ID, q)
		}
		if err != nil {
			r.RaiseFault(err)
		}
		if len(rows) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(RowToLua(r, tb, rows[0]))
		return 1
	}))

	t.RawSetString("count", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		_, tb := c.table(L.CheckString(1))
		var f *rec.Filter
		if L.GetTop() >= 2 && L.Get(2) != lua.LNil {
			f = WhereFilter(r, L, tb, L.CheckTable(2), 1)
		}
		var n int64
		var err error
		if tx, tctx, ok := c.d.Tx(); ok {
			n, _, err = rec.CountTx(tctx, tx, tb, f)
		} else {
			n, _, err = c.d.Records.Count(r.Ctx, c.d.Lease, tb.ID, f)
		}
		if err != nil {
			r.RaiseFault(err)
		}
		L.Push(lua.LNumber(n))
		return 1
	}))

	t.RawSetString("exists", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		sch, tb := c.table(L.CheckString(1))
		var f *rec.Filter
		if L.GetTop() >= 2 && L.Get(2) != lua.LNil {
			f = WhereFilter(r, L, tb, L.CheckTable(2), 1)
		}
		q := rec.ListQuery{Filter: f, Limit: 1}
		var rows []rec.Row
		var err error
		if tx, tctx, ok := c.d.Tx(); ok {
			rows, _, err = rec.ListTx(tctx, tx, sch, tb, q)
		} else {
			rows, _, err = c.d.Records.List(r.Ctx, c.d.Lease, tb.ID, q)
		}
		if err != nil {
			r.RaiseFault(err)
		}
		L.Push(lua.LBool(len(rows) > 0))
		return 1
	}))

	t.RawSetString("insert", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		c.checkWrite()
		sch, tb := c.table(L.CheckString(1))
		fields := ValuesFromLua(r, L, tb, 2)
		actor := rec.Actor{UserID: c.d.UserID}
		var row rec.Row
		if tx, tctx, ok := c.d.Tx(); ok {
			res, err := c.d.Records.InsertTx(tctx, tx, string(c.d.Lease.ID()), sch, tb, fields, actor)
			if err != nil {
				r.RaiseFault(err)
			}
			row = res.Row
			c.d.AfterCommit(res.Publish)
		} else {
			var err error
			if row, err = c.d.Records.Insert(r.Ctx, c.d.Lease, tb.ID, fields, actor, nil); err != nil {
				r.RaiseFault(err)
			}
		}
		L.Push(lua.LNumber(row.ID))
		return 1
	}))

	t.RawSetString("insert_many", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		c.checkWrite()
		sch, tb := c.table(L.CheckString(1))
		list := L.CheckTable(2)
		n := list.Len()
		if n == 0 {
			L.Push(L.NewTable())
			return 1
		}
		actor := rec.Actor{UserID: c.d.UserID}
		ws := string(c.d.Lease.ID())
		outIDs := make([]int64, 0, n)

		runBatch := func(ctx context.Context, tx dbx.Tx) error {
			for i := 1; i <= n; i++ {
				rowTable, ok := list.RawGetInt(i).(*lua.LTable)
				if !ok {
					return fault.New(fault.BadRequest, "each item in rows must be a table")
				}
				L.Push(rowTable)
				fields := ValuesFromLua(r, L, tb, L.GetTop())
				L.Pop(1)
				res, err := c.d.Records.InsertTx(ctx, tx, ws, sch, tb, fields, actor)
				if err != nil {
					return err
				}
				outIDs = append(outIDs, res.Row.ID)
				c.d.AfterCommit(res.Publish)
			}
			return nil
		}

		var err error
		if tx, tctx, ok := c.d.Tx(); ok {
			err = runBatch(tctx, tx)
		} else {
			c.d.SetTx(r.Ctx, nil)
			err = c.d.Lease.Write(r.Ctx, priority(r), func(tx dbx.Tx) error {
				c.d.SetTx(r.Ctx, tx)
				return runBatch(r.Ctx, tx)
			})
			c.d.FinishTx(err == nil)
		}
		if err != nil {
			r.RaiseFault(err)
		}

		out := L.CreateTable(len(outIDs), 0)
		for _, id := range outIDs {
			out.Append(lua.LNumber(id))
		}
		L.Push(out)
		return 1
	}))

	t.RawSetString("update", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		c.checkWrite()
		sch, tb := c.table(L.CheckString(1))
		id := L.CheckInt64(2)
		fields := ValuesFromLua(r, L, tb, 3)
		ver := versionOpt(L, 4)
		actor := rec.Actor{UserID: c.d.UserID}
		if tx, tctx, ok := c.d.Tx(); ok {
			if ver == 0 {
				cur, err := rec.GetTx(tctx, tx, sch, tb, id)
				if err != nil {
					r.RaiseFault(err)
				}
				ver = cur.Version
			}
			res, err := c.d.Records.UpdateTx(tctx, tx, string(c.d.Lease.ID()), sch, tb, id, ver, fields, actor)
			if err != nil {
				r.RaiseFault(err)
			}
			c.d.AfterCommit(res.Publish)
			L.Push(lua.LNumber(res.Row.Version))
			return 1
		}
		if ver == 0 {
			cur, err := c.d.Records.Get(r.Ctx, c.d.Lease, tb.ID, id)
			if err != nil {
				r.RaiseFault(err)
			}
			ver = cur.Version
		}
		row, err := c.d.Records.Update(r.Ctx, c.d.Lease, tb.ID, id, ver, fields, actor, nil)
		if err != nil {
			r.RaiseFault(err)
		}
		L.Push(lua.LNumber(row.Version))
		return 1
	}))

	t.RawSetString("update_many", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		c.checkWrite()
		sch, tb := c.table(L.CheckString(1))
		wv := L.CheckTable(2)
		fields := ValuesFromLua(r, L, tb, 3)
		filter := WhereFilter(r, L, tb, wv, 1)

		actor := rec.Actor{UserID: c.d.UserID}
		ws := string(c.d.Lease.ID())
		var changed int

		run := func(ctx context.Context, tx dbx.Tx) error {
			changed = 0
			var match []rec.Row
			for {
				page, more, err := rec.ListTx(ctx, tx, sch, tb, rec.ListQuery{Filter: filter, Limit: rec.MaxPage, Offset: len(match)})
				if err != nil {
					return err
				}
				match = append(match, page...)
				if len(match) > 10000 {
					return fault.New(fault.BadRequest, "More than 10000 rows match; narrow the where.")
				}
				if !more {
					break
				}
			}
			for _, row := range match {
				res, err := c.d.Records.UpdateTx(ctx, tx, ws, sch, tb, row.ID, row.Version, fields, actor)
				if err != nil {
					return err
				}
				c.d.AfterCommit(res.Publish)
				changed++
			}
			return nil
		}

		var err error
		if tx, tctx, ok := c.d.Tx(); ok {
			err = run(tctx, tx)
		} else {
			c.d.SetTx(r.Ctx, nil)
			err = c.d.Lease.Write(r.Ctx, priority(r), func(tx dbx.Tx) error {
				c.d.SetTx(r.Ctx, tx)
				return run(r.Ctx, tx)
			})
			c.d.FinishTx(err == nil)
		}
		if err != nil {
			r.RaiseFault(err)
		}
		out := L.NewTable()
		out.RawSetString("changes", lua.LNumber(changed))
		L.Push(out)
		return 1
	}))

	t.RawSetString("upsert", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		c.checkWrite()
		sch, tb := c.table(L.CheckString(1))
		matchTable := L.CheckTable(2)

		f := WhereFilter(r, L, tb, matchTable, 1)
		updateFields := ValuesFromLua(r, L, tb, 3)
		actor := rec.Actor{UserID: c.d.UserID}
		ws := string(c.d.Lease.ID())
		var resID int64

		runUpsert := func(ctx context.Context, tx dbx.Tx) error {
			rows, _, err := rec.ListTx(ctx, tx, sch, tb, rec.ListQuery{Filter: f, Limit: 1})
			if err != nil {
				return err
			}
			if len(rows) > 0 {
				existing := rows[0]
				res, err := c.d.Records.UpdateTx(ctx, tx, ws, sch, tb, existing.ID, existing.Version, updateFields, actor)
				if err != nil {
					return err
				}
				c.d.AfterCommit(res.Publish)
				resID = existing.ID
				return nil
			}

			// Insert: merge matchTable and updateTable
			combinedFields := ValuesFromLua(r, L, tb, 2)
			for k, v := range updateFields {
				combinedFields[k] = v
			}
			res, err := c.d.Records.InsertTx(ctx, tx, ws, sch, tb, combinedFields, actor)
			if err != nil {
				return err
			}
			c.d.AfterCommit(res.Publish)
			resID = res.Row.ID
			return nil
		}

		var err error
		if tx, tctx, ok := c.d.Tx(); ok {
			err = runUpsert(tctx, tx)
		} else {
			c.d.SetTx(r.Ctx, nil)
			err = c.d.Lease.Write(r.Ctx, priority(r), func(tx dbx.Tx) error {
				c.d.SetTx(r.Ctx, tx)
				return runUpsert(r.Ctx, tx)
			})
			c.d.FinishTx(err == nil)
		}
		if err != nil {
			r.RaiseFault(err)
		}
		L.Push(lua.LNumber(resID))
		return 1
	}))

	t.RawSetString("delete", L.NewFunction(func(L *lua.LState) int {
		c := get(r, L)
		c.checkWrite()
		sch, tb := c.table(L.CheckString(1))
		id := L.CheckInt64(2)
		ver := versionOpt(L, 3)
		actor := rec.Actor{UserID: c.d.UserID}
		if tx, tctx, ok := c.d.Tx(); ok {
			if ver == 0 {
				cur, err := rec.GetTx(tctx, tx, sch, tb, id)
				if err != nil {
					r.RaiseFault(err)
				}
				ver = cur.Version
			}
			res, err := c.d.Records.DeleteTx(tctx, tx, string(c.d.Lease.ID()), sch, tb, id, ver, actor)
			if err != nil {
				r.RaiseFault(err)
			}
			c.d.AfterCommit(res.Publish)
			return 0
		}
		if ver == 0 {
			cur, err := c.d.Records.Get(r.Ctx, c.d.Lease, tb.ID, id)
			if err != nil {
				r.RaiseFault(err)
			}
			ver = cur.Version
		}
		if err := c.d.Records.Delete(r.Ctx, c.d.Lease, tb.ID, id, ver, actor, nil); err != nil {
			r.RaiseFault(err)
		}
		return 0
	}))
	return t
}

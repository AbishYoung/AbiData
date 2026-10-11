// Package records is the Lua `records` module: typed row reads and writes
// through the row API (OCC, validation messages, row history, change feed).
package records

import (
	"encoding/json"
	"fmt"
	"strconv"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/geo"
	rec "abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
)

func init() { script.Register(script.ModuleDef{Doc: doc, Build: build}) }

var doc = script.ModuleDoc{
	Name:     "records",
	Doc:      "Read and change rows field by field, with the same validation, concurrency checks and history as the datasheet. Fields are named by their field (physical) names.",
	Contexts: []script.Context{script.UIEvent, script.Report, script.Job},
	Funcs: []script.FuncDoc{
		{Name: "get", Signature: "records.get(table, id) → row|nil", Returns: "row table (id, _version and every field; currency as decimal, yes/no as boolean, empty as db.NULL) or nil",
			Doc: "Reads one row by id.", Example: `local c = records.get("Customers", record.Customer)`},
		{Name: "insert", Signature: "records.insert(table, values) → id", Errors: []string{"REQUIRED_FIELD", "VALIDATION_FAILED", "DUPLICATE_VALUE", "LINKED_RECORDS"},
			Doc: "Creates a row and returns its id.", Example: `local id = records.insert("Notes", {Text = "Called", Customer = record.id})`, Contexts: []script.Context{script.UIEvent, script.Job}},
		{Name: "update", Signature: "records.update(table, id, values, {version?}) → version", Errors: []string{"CONCURRENT_MODIFICATION", "VALIDATION_FAILED", "NOT_FOUND"},
			Doc: "Changes fields of a row. With version, fails if the row changed since that version; without it, applies to the current version.", Example: `records.update("Orders", id, {Status = "Shipped"})`, Contexts: []script.Context{script.UIEvent, script.Job}},
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
	if ids, ok := v.([]string); ok { // attachments: a list of file ids
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
	if raw, ok := v.(geo.Raw); ok { // geo_point and geo_area: a table mirroring the GeoJSON
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
		out[col.ID] = toAPI(script.FromLua(r, v))
	})
	return out
}

func toAPI(v any) any {
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

// Package staging registers the job-only `staging` module: scratch tables for
// bulk work. A job loads rows into a staging table, can query it with SQL, and
// merges it into a workspace table in 2,000-row background chunks.
package staging

import (
	lua "github.com/yuin/gopher-lua"

	"abidata/internal/fault"
	"abidata/internal/script"
	"abidata/internal/script/host"
)

func init() {
	script.Register(script.ModuleDef{Doc: doc, Build: build})
}

var doc = script.ModuleDoc{
	Name: "staging", Contexts: []script.Context{script.Job},
	Doc: "Scratch tables for bulk loads. staging.create returns a handle with these methods; the staging tables are private to the run and deleted when it ends (at most 8 per run, 1,000,000 rows and 256 MiB each).\n\n" +
		"handle:insert(row): adds one row, given as a table keyed by column name or as an array in column order. Values are text, numbers, booleans or db.NULL; missing columns are empty.\n\n" +
		"handle:query(sql, params?) → rows: runs one SELECT against the table, which is named data (columns are the names given to create, plus n, the insertion number). Use ? placeholders with an array or :name placeholders with a table. Writes, PRAGMA, ATTACH and any other table are refused (SQL_NOT_ALLOWED). Returns an array of row tables keyed by column name; SQL NULL is db.NULL. A result of more than 100,000 rows or 32 MiB is an error.\n\n" +
		"handle:count() → number: the number of rows inserted.\n\n" +
		"handle:merge_into(table_name, {mode, key, chunk, all_or_nothing}?) → {inserted, updated, skipped, error_count, errors}: copies every row into the table. Staging column names must match the table's field names (display or physical name, any case). mode is \"append\" (default), \"upsert\" (key = the name of a field with a unique index; existing rows with the same key are updated) or \"replace\" (delete all rows, then insert; one transaction up to 50,000 rows). Rows are converted exactly like row API values (text \"12.50\" for currency, ISO dates, yes/no words) and written in chunks of chunk rows (default 2,000, at most 10,000) so interactive saves are served between chunks; rows the database rejects are skipped and listed in errors (the first 100, each {row, column, code, message}). With all_or_nothing = true the whole merge is one transaction that rolls back on the first bad row and raises an error. merge_into cannot run inside db.transaction.",
	Funcs: []script.FuncDoc{{
		Name: "create", Signature: "staging.create({columns = {...}}) → handle",
		Params:  []script.ParamDoc{{Name: "options", Type: "table", Doc: "columns: array of column names (letters, digits, underscores; at most 256; n is reserved)."}},
		Returns: "A staging handle with the methods insert, query, count and merge_into (see the module description).",
		Errors:  []string{"BAD_REQUEST: invalid or repeated column names", "QUOTA_EXCEEDED: too many staging tables in this run"},
		Doc:     "Creates an empty staging table for this run.",
		Example: `local st = staging.create({columns = {"sku", "qty"}})
st:insert({sku = "A-1", qty = 5})
st:insert({"B-2", 7})
local rows = st:query("SELECT sku FROM data WHERE qty > :min", {min = 5})
local res = st:merge_into("Stock", {mode = "upsert", key = "sku"})
log.info("merged", {inserted = res.inserted, updated = res.updated})`,
	}},
}

func build(r *script.Run) lua.LValue {
	L := r.L
	store := func() host.StagingStore {
		d, _ := r.Host.(*host.Data)
		if d == nil || d.Staging == nil {
			r.RaiseFault(fault.New(fault.Forbidden, "staging is only available in jobs."))
			return nil
		}
		return d.Staging
	}
	fail := func(err error) int { r.RaiseFault(err); return 0 }
	t := L.NewTable()
	t.RawSetString("create", L.NewFunction(func(L *lua.LState) int {
		opts := L.CheckTable(1)
		ct, ok := opts.RawGetString("columns").(*lua.LTable)
		if !ok {
			L.ArgError(1, "columns must be an array of names")
		}
		var cols []string
		for i := 1; i <= ct.Len(); i++ {
			s, ok := ct.RawGetInt(i).(lua.LString)
			if !ok {
				L.ArgError(1, "columns must be an array of names")
			}
			cols = append(cols, string(s))
		}
		st := store()
		h, err := st.Create(r.Ctx, cols)
		if err != nil {
			return fail(err)
		}
		ht := L.NewTable()
		ht.RawSetString("insert", L.NewFunction(func(L *lua.LState) int {
			row := L.CheckTable(2)
			if err := st.Insert(r.Ctx, h, script.FromLua(r, row)); err != nil {
				return fail(err)
			}
			return 0
		}))
		ht.RawSetString("count", L.NewFunction(func(L *lua.LState) int {
			n, err := st.Count(r.Ctx, h)
			if err != nil {
				return fail(err)
			}
			L.Push(lua.LNumber(n))
			return 1
		}))
		ht.RawSetString("query", L.NewFunction(func(L *lua.LState) int {
			sql := L.CheckString(2)
			var params any
			if p := L.OptTable(3, nil); p != nil {
				params = script.FromLua(r, p)
				if m, ok := params.(map[string]any); ok && len(m) == 0 {
					params = []any{}
				}
			}
			names, rows, err := st.Query(r.Ctx, h, sql, params)
			if err != nil {
				return fail(err)
			}
			out := L.CreateTable(len(rows), 0)
			for _, row := range rows {
				rt := L.CreateTable(0, len(names))
				for i, name := range names {
					if row[i] == nil {
						rt.RawSetString(name, r.Sentinel(script.NullName))
					} else {
						rt.RawSetString(name, script.ToLua(r, row[i]))
					}
				}
				out.Append(rt)
			}
			L.Push(out)
			return 1
		}))
		ht.RawSetString("merge_into", L.NewFunction(func(L *lua.LState) int {
			table := L.CheckString(2)
			var opts map[string]any
			if o := L.OptTable(3, nil); o != nil {
				m, ok := script.FromLua(r, o).(map[string]any)
				if !ok && o.Len() > 0 {
					L.ArgError(3, "options must be a table with string keys")
				}
				opts = m
			}
			res, err := st.Merge(r.Ctx, h, table, opts)
			if err != nil {
				return fail(err)
			}
			L.Push(script.ToLua(r, res))
			return 1
		}))
		L.Push(ht)
		return 1
	}))
	return t
}

// Package data provides tabular dataset manipulation, grouped aggregation,
// pivoting, sorting and filtering for row-table lists in Lua scripts.
package data

import (
	"math"
	"sort"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

func init() {
	script.Register(script.ModuleDef{Doc: doc, Build: build})
}

var allContexts = []script.Context{script.UIEvent, script.Report, script.Job}

var doc = script.ModuleDoc{
	Name:     "data",
	Doc:      "Tabular dataset manipulation: grouping, multi-metric aggregation, pivot tables, sorting and transformations on row arrays.",
	Contexts: allContexts,
	Funcs: []script.FuncDoc{
		{
			Name: "group_by", Signature: "data.group_by(rows, key) → table",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "key", Type: "string", Doc: "Field name to group by."},
			},
			Returns: "table mapping each distinct group key value to an array of matching rows",
			Doc:     "Groups an array of rows by a key field.",
			Example: `local byRegion = data.group_by(customers, "Region")`,
		},
		{
			Name: "aggregate", Signature: "data.aggregate(rows, spec) → rows",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "spec", Type: "table", Doc: "{by?: string|array, metrics: table}: metrics maps output column name to agg function (\"count\", \"sum\", \"avg\", \"min\", \"max\", \"median\", \"first\", \"last\") or {field, fn}."},
			},
			Returns: "array of aggregated row tables",
			Doc:     "Performs grouped rollups with multiple metric aggregations in a single pass.",
			Example: `local summary = data.aggregate(orders, {
  by = "Region",
  metrics = {
    order_count = "count",
    total_sales = {field = "Total", fn = "sum"},
    avg_sales = {field = "Total", fn = "avg"},
  }
})`,
		},
		{
			Name: "summarize", Signature: "data.summarize(rows, spec) → rows",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "spec", Type: "table", Doc: "Aggregation specification (alias for data.aggregate)."},
			},
			Returns: "array of aggregated row tables",
			Doc:     "Alias for data.aggregate.",
			Example: `local summary = data.summarize(orders, {by = "Category", metrics = {revenue = "sum"}})`,
		},
		{
			Name: "pivot", Signature: "data.pivot(rows, spec) → rows",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "spec", Type: "table", Doc: "{row: string, col: string, value: string, agg?: string}."},
			},
			Returns: "array of pivot table row tables (with row key, column keys, and total)",
			Doc:     "Generates a two-dimensional pivot table from tabular rows.",
			Example: `local pt = data.pivot(sales, {row = "Region", col = "Quarter", value = "Amount", agg = "sum"})`,
		},
		{
			Name: "sort_by", Signature: "data.sort_by(rows, spec [, desc]) → rows",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "spec", Type: "string|table", Doc: "Field name or array of field names (prefix with \"-\" for descending)."},
				{Name: "desc", Type: "boolean", Opt: true, Doc: "True to sort descending (when spec is a single field name)."},
			},
			Returns: "new array containing sorted rows",
			Doc:     "Sorts an array of row tables by one or multiple fields.",
			Example: `local sorted = data.sort_by(products, {"Category", "-Price"})`,
		},
		{
			Name: "pluck", Signature: "data.pluck(rows, field) → array",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "field", Type: "string", Doc: "Field name to extract."},
			},
			Returns: "1-indexed array of values",
			Doc:     "Extracts a single column from an array of row tables.",
			Example: `local ids = data.pluck(users, "id")`,
		},
		{
			Name: "index_by", Signature: "data.index_by(rows, key) → table",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "key", Type: "string", Doc: "Field name whose values serve as keys."},
			},
			Returns: "table mapping key values to their corresponding row tables",
			Doc:     "Indexes an array of rows by a unique key field.",
			Example: `local userMap = data.index_by(users, "id")`,
		},
		{
			Name: "distinct", Signature: "data.distinct(rows [, field]) → array",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables or array of scalar values."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name to extract distinct values of."},
			},
			Returns: "array of distinct values or deduplicated rows",
			Doc:     "Returns unique elements or distinct values of a field.",
			Example: `local cities = data.distinct(customers, "City")`,
		},
		{
			Name: "frequencies", Signature: "data.frequencies(rows [, field]) → array",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables or scalar values."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name to count frequencies for."},
			},
			Returns: "array of {value, count, ratio} sorted descending by count",
			Doc:     "Computes frequency counts and proportions.",
			Example: `local statusCounts = data.frequencies(orders, "Status")`,
		},
		{
			Name: "top_n", Signature: "data.top_n(rows, n, field [, desc]) → rows",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "n", Type: "number", Doc: "Maximum number of rows to return."},
				{Name: "field", Type: "string", Doc: "Field name to rank by."},
				{Name: "desc", Type: "boolean", Opt: true, Doc: "True for highest first (default true)."},
			},
			Returns: "array of up to n row tables",
			Doc:     "Returns the top n rows ranked by a field.",
			Example: `local top5 = data.top_n(customers, 5, "TotalSpent")`,
		},
		{
			Name: "filter", Signature: "data.filter(rows, predicate) → rows",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "predicate", Type: "function|table", Doc: "Function returning boolean or table of matching key-values."},
			},
			Returns: "filtered array of row tables",
			Doc:     "Filters rows using a predicate function or match table.",
			Example: `local active = data.filter(users, {Active = true})`,
		},
		{
			Name: "slice", Signature: "data.slice(rows, start, count) → rows",
			Params: []script.ParamDoc{
				{Name: "rows", Type: "table", Doc: "Array of row tables."},
				{Name: "start", Type: "number", Doc: "1-indexed starting position."},
				{Name: "count", Type: "number", Doc: "Number of rows to take."},
			},
			Returns: "sub-array of row tables",
			Doc:     "Extracts a slice of rows.",
			Example: `local page1 = data.slice(rows, 1, 10)`,
		},
	},
}

func toFloat(v lua.LValue) (float64, bool) {
	switch num := v.(type) {
	case lua.LNumber:
		f := float64(num)
		if !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, true
		}
	case lua.LString:
		if f, err := strconv.ParseFloat(string(num), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, true
		}
	}
	return 0, false
}

func compareLua(a, b lua.LValue) int {
	af, aOk := toFloat(a)
	bf, bOk := toFloat(b)
	if aOk && bOk {
		if af < bf {
			return -1
		}
		if af > bf {
			return 1
		}
		return 0
	}
	as := strings.ToLower(a.String())
	bs := strings.ToLower(b.String())
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	fn := func(name string, f lua.LGFunction) { t.RawSetString(name, L.NewFunction(f)) }

	fn("group_by", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		key := L.CheckString(2)
		n := rows.Len()
		groups := map[string]*lua.LTable{}
		out := L.NewTable()

		for i := 1; i <= n; i++ {
			row, ok := rows.RawGetInt(i).(*lua.LTable)
			if !ok {
				continue
			}
			val := row.RawGetString(key).String()
			arr, exists := groups[val]
			if !exists {
				arr = L.NewTable()
				groups[val] = arr
				out.RawSetString(val, arr)
			}
			arr.Append(row)
		}
		L.Push(out)
		return 1
	})

	doAggregate := func(L *lua.LState) int {
		rows := L.CheckTable(1)
		spec := L.CheckTable(2)
		nRows := rows.Len()

		// Parse "by" field(s)
		var byFields []string
		byVal := spec.RawGetString("by")
		switch bv := byVal.(type) {
		case lua.LString:
			byFields = append(byFields, string(bv))
		case *lua.LTable:
			for i := 1; i <= bv.Len(); i++ {
				byFields = append(byFields, bv.RawGetInt(i).String())
			}
		}

		// Parse metrics
		metricsTable, ok := spec.RawGetString("metrics").(*lua.LTable)
		if !ok {
			L.ArgError(2, "metrics must be a table")
		}

		type metricSpec struct {
			outField string
			srcField string
			fn       string
		}
		var metrics []metricSpec
		metricsTable.ForEach(func(k, v lua.LValue) {
			outCol := k.String()
			srcCol := outCol
			fnName := "count"
			switch mv := v.(type) {
			case lua.LString:
				fnName = strings.ToLower(string(mv))
				if fnName == "count" {
					srcCol = "*"
				}
			case *lua.LTable:
				if f := mv.RawGetString("field"); f != lua.LNil {
					srcCol = f.String()
				}
				if fnV := mv.RawGetString("fn"); fnV != lua.LNil {
					fnName = strings.ToLower(fnV.String())
				}
				if fnName == "count" && mv.RawGetString("field") == lua.LNil {
					srcCol = "*"
				}
			}
			metrics = append(metrics, metricSpec{outField: outCol, srcField: srcCol, fn: fnName})
		})

		type groupData struct {
			keyVals map[string]lua.LValue
			rows    []*lua.LTable
		}
		var groupOrder []string
		groups := map[string]*groupData{}

		for i := 1; i <= nRows; i++ {
			row, ok := rows.RawGetInt(i).(*lua.LTable)
			if !ok {
				continue
			}
			var groupKeyParts []string
			keyVals := map[string]lua.LValue{}
			for _, bf := range byFields {
				v := row.RawGetString(bf)
				keyVals[bf] = v
				groupKeyParts = append(groupKeyParts, v.String())
			}
			groupKey := strings.Join(groupKeyParts, "\x00")
			gd, exists := groups[groupKey]
			if !exists {
				gd = &groupData{keyVals: keyVals}
				groups[groupKey] = gd
				groupOrder = append(groupOrder, groupKey)
			}
			gd.rows = append(gd.rows, row)
		}

		out := L.CreateTable(len(groupOrder), 0)
		for _, gk := range groupOrder {
			gd := groups[gk]
			resRow := L.NewTable()
			for k, v := range gd.keyVals {
				resRow.RawSetString(k, v)
			}

			for _, m := range metrics {
				var resultVal = lua.LNil
				var numVals []float64
				for _, r := range gd.rows {
					val := r.RawGetString(m.srcField)
					if f, ok := toFloat(val); ok {
						numVals = append(numVals, f)
					}
				}

				switch m.fn {
				case "count":
					if m.srcField == "*" || m.srcField == "count" || m.srcField == "" {
						resultVal = lua.LNumber(len(gd.rows))
					} else {
						cnt := 0
						for _, r := range gd.rows {
							v := r.RawGetString(m.srcField)
							if v != lua.LNil {
								cnt++
							}
						}
						resultVal = lua.LNumber(cnt)
					}
				case "sum":
					var s float64
					for _, v := range numVals {
						s += v
					}
					resultVal = lua.LNumber(s)
				case "avg", "mean":
					if len(numVals) > 0 {
						var s float64
						for _, v := range numVals {
							s += v
						}
						resultVal = lua.LNumber(s / float64(len(numVals)))
					}
				case "min":
					if len(numVals) > 0 {
						minV := numVals[0]
						for _, v := range numVals[1:] {
							if v < minV {
								minV = v
							}
						}
						resultVal = lua.LNumber(minV)
					}
				case "max":
					if len(numVals) > 0 {
						maxV := numVals[0]
						for _, v := range numVals[1:] {
							if v > maxV {
								maxV = v
							}
						}
						resultVal = lua.LNumber(maxV)
					}
				case "median":
					if len(numVals) > 0 {
						sort.Float64s(numVals)
						n := len(numVals)
						if n%2 == 1 {
							resultVal = lua.LNumber(numVals[n/2])
						} else {
							resultVal = lua.LNumber((numVals[n/2-1] + numVals[n/2]) / 2.0)
						}
					}
				case "first":
					if len(gd.rows) > 0 {
						resultVal = gd.rows[0].RawGetString(m.srcField)
					}
				case "last":
					if len(gd.rows) > 0 {
						resultVal = gd.rows[len(gd.rows)-1].RawGetString(m.srcField)
					}
				}
				resRow.RawSetString(m.outField, resultVal)
			}
			out.Append(resRow)
		}

		L.Push(out)
		return 1
	}

	fn("aggregate", doAggregate)
	fn("summarize", doAggregate)

	fn("pivot", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		spec := L.CheckTable(2)
		optStr := func(keys ...string) string {
			for _, k := range keys {
				if v := spec.RawGetString(k); v != lua.LNil && v.String() != "" {
					return v.String()
				}
			}
			return ""
		}
		rowField := optStr("rows", "row")
		colField := optStr("cols", "col")
		valField := optStr("val", "value", "field")
		aggFn := "sum"
		if a := spec.RawGetString("agg"); a != lua.LNil && a.String() != "" {
			aggFn = strings.ToLower(a.String())
		}
		if rowField == "" || colField == "" || valField == "" {
			L.ArgError(2, "row, col and value are required in pivot spec")
		}

		type cellData struct {
			nums []float64
		}
		rowKeys := []string{}
		colKeys := []string{}
		rowSeen := map[string]bool{}
		colSeen := map[string]bool{}
		grid := map[string]map[string]*cellData{}

		n := rows.Len()
		for i := 1; i <= n; i++ {
			r, ok := rows.RawGetInt(i).(*lua.LTable)
			if !ok {
				continue
			}
			rk := r.RawGetString(rowField).String()
			ck := r.RawGetString(colField).String()
			if !rowSeen[rk] {
				rowSeen[rk] = true
				rowKeys = append(rowKeys, rk)
			}
			if !colSeen[ck] {
				colSeen[ck] = true
				colKeys = append(colKeys, ck)
			}
			if grid[rk] == nil {
				grid[rk] = map[string]*cellData{}
			}
			if grid[rk][ck] == nil {
				grid[rk][ck] = &cellData{}
			}
			if f, ok := toFloat(r.RawGetString(valField)); ok {
				grid[rk][ck].nums = append(grid[rk][ck].nums, f)
			}
		}
		sort.Strings(colKeys)

		calcCell := func(nums []float64) float64 {
			if len(nums) == 0 {
				return 0
			}
			switch aggFn {
			case "count":
				return float64(len(nums))
			case "avg", "mean":
				var s float64
				for _, v := range nums {
					s += v
				}
				return s / float64(len(nums))
			case "min":
				m := nums[0]
				for _, v := range nums[1:] {
					if v < m {
						m = v
					}
				}
				return m
			case "max":
				m := nums[0]
				for _, v := range nums[1:] {
					if v > m {
						m = v
					}
				}
				return m
			default: // sum
				var s float64
				for _, v := range nums {
					s += v
				}
				return s
			}
		}

		out := L.CreateTable(len(rowKeys), 0)
		for _, rk := range rowKeys {
			rowTbl := L.NewTable()
			rowTbl.RawSetString(rowField, lua.LString(rk))
			var rowTotal float64
			for _, ck := range colKeys {
				var cellVal float64
				if cd := grid[rk][ck]; cd != nil {
					cellVal = calcCell(cd.nums)
				}
				rowTbl.RawSetString(ck, lua.LNumber(cellVal))
				rowTotal += cellVal
			}
			rowTbl.RawSetString("total", lua.LNumber(rowTotal))
			out.Append(rowTbl)
		}

		L.Push(out)
		return 1
	})

	fn("sort_by", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		type sortKey struct {
			field string
			desc  bool
		}
		var keys []sortKey
		if L.Get(2).Type() == lua.LTTable {
			tbl := L.CheckTable(2)
			nKeys := tbl.Len()
			for i := 1; i <= nKeys; i++ {
				kStr := tbl.RawGetInt(i).String()
				desc := false
				if strings.HasPrefix(kStr, "-") {
					desc = true
					kStr = strings.TrimPrefix(kStr, "-")
				} else if strings.HasPrefix(kStr, "+") {
					kStr = strings.TrimPrefix(kStr, "+")
				}
				keys = append(keys, sortKey{field: kStr, desc: desc})
			}
		} else {
			field := L.CheckString(2)
			desc := false
			if strings.HasPrefix(field, "-") {
				desc = true
				field = strings.TrimPrefix(field, "-")
			} else if strings.HasPrefix(field, "+") {
				field = strings.TrimPrefix(field, "+")
			}
			if L.GetTop() >= 3 && L.CheckBool(3) {
				desc = true
			}
			keys = append(keys, sortKey{field: field, desc: desc})
		}

		n := rows.Len()
		list := make([]*lua.LTable, 0, n)
		for i := 1; i <= n; i++ {
			if r, ok := rows.RawGetInt(i).(*lua.LTable); ok {
				list = append(list, r)
			}
		}

		sort.SliceStable(list, func(i, j int) bool {
			for _, k := range keys {
				cmp := compareLua(list[i].RawGetString(k.field), list[j].RawGetString(k.field))
				if cmp != 0 {
					if k.desc {
						return cmp > 0
					}
					return cmp < 0
				}
			}
			return false
		})

		out := L.CreateTable(len(list), 0)
		for _, r := range list {
			out.Append(r)
		}
		L.Push(out)
		return 1
	})

	fn("pluck", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		field := L.CheckString(2)
		n := rows.Len()
		out := L.CreateTable(n, 0)
		for i := 1; i <= n; i++ {
			if r, ok := rows.RawGetInt(i).(*lua.LTable); ok {
				out.Append(r.RawGetString(field))
			}
		}
		L.Push(out)
		return 1
	})

	fn("index_by", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		key := L.CheckString(2)
		n := rows.Len()
		out := L.NewTable()
		for i := 1; i <= n; i++ {
			if r, ok := rows.RawGetInt(i).(*lua.LTable); ok {
				k := r.RawGetString(key).String()
				out.RawSetString(k, r)
			}
		}
		L.Push(out)
		return 1
	})

	fn("distinct", func(L *lua.LState) int {
		tbl := L.CheckTable(1)
		var field string
		if L.GetTop() >= 2 && L.Get(2).Type() == lua.LTString {
			field = L.CheckString(2)
		}
		seen := map[string]bool{}
		out := L.NewTable()
		n := tbl.Len()
		for i := 1; i <= n; i++ {
			item := tbl.RawGetInt(i)
			targetVal := item
			if field != "" {
				if r, ok := item.(*lua.LTable); ok {
					targetVal = r.RawGetString(field)
				}
			}
			key := targetVal.String()
			if !seen[key] {
				seen[key] = true
				out.Append(targetVal)
			}
		}
		L.Push(out)
		return 1
	})

	fn("frequencies", func(L *lua.LState) int {
		tbl := L.CheckTable(1)
		var field string
		if L.GetTop() >= 2 && L.Get(2).Type() == lua.LTString {
			field = L.CheckString(2)
		}
		counts := map[string]int{}
		repVal := map[string]lua.LValue{}
		var keys []string
		total := 0
		n := tbl.Len()

		for i := 1; i <= n; i++ {
			item := tbl.RawGetInt(i)
			targetVal := item
			if field != "" {
				if r, ok := item.(*lua.LTable); ok {
					targetVal = r.RawGetString(field)
				}
			}
			sKey := targetVal.String()
			if counts[sKey] == 0 {
				keys = append(keys, sKey)
				repVal[sKey] = targetVal
			}
			counts[sKey]++
			total++
		}

		sort.SliceStable(keys, func(i, j int) bool {
			return counts[keys[i]] > counts[keys[j]]
		})

		out := L.CreateTable(len(keys), 0)
		for _, k := range keys {
			c := counts[k]
			item := L.NewTable()
			item.RawSetString("value", repVal[k])
			item.RawSetString("count", lua.LNumber(c))
			ratio := 0.0
			if total > 0 {
				ratio = float64(c) / float64(total)
			}
			item.RawSetString("ratio", lua.LNumber(ratio))
			out.Append(item)
		}
		L.Push(out)
		return 1
	})

	fn("top_n", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		topCount := L.CheckInt(2)
		field := L.CheckString(3)
		desc := true
		if L.GetTop() >= 4 {
			desc = L.CheckBool(4)
		}

		n := rows.Len()
		list := make([]*lua.LTable, 0, n)
		for i := 1; i <= n; i++ {
			if r, ok := rows.RawGetInt(i).(*lua.LTable); ok {
				list = append(list, r)
			}
		}

		sort.SliceStable(list, func(i, j int) bool {
			cmp := compareLua(list[i].RawGetString(field), list[j].RawGetString(field))
			if desc {
				return cmp > 0
			}
			return cmp < 0
		})

		if topCount > len(list) {
			topCount = len(list)
		}
		if topCount < 0 {
			topCount = 0
		}

		out := L.CreateTable(topCount, 0)
		for i := 0; i < topCount; i++ {
			out.Append(list[i])
		}
		L.Push(out)
		return 1
	})

	fn("filter", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		predVal := L.Get(2)
		n := rows.Len()
		out := L.NewTable()

		switch pv := predVal.(type) {
		case *lua.LFunction:
			for i := 1; i <= n; i++ {
				r := rows.RawGetInt(i)
				L.Push(pv)
				L.Push(r)
				if err := L.PCall(1, 1, nil); err != nil {
					L.RaiseError("data.filter: %s", err.Error())
				}
				ret := L.Get(-1)
				L.Pop(1)
				if lua.LVAsBool(ret) {
					out.Append(r)
				}
			}
		case *lua.LTable:
			for i := 1; i <= n; i++ {
				r, ok := rows.RawGetInt(i).(*lua.LTable)
				if !ok {
					continue
				}
				matches := true
				pv.ForEach(func(k, v lua.LValue) {
					if r.RawGetString(k.String()).String() != v.String() {
						matches = false
					}
				})
				if matches {
					out.Append(r)
				}
			}
		default:
			L.ArgError(2, "predicate must be a function or match table")
		}
		L.Push(out)
		return 1
	})

	fn("slice", func(L *lua.LState) int {
		rows := L.CheckTable(1)
		start := L.CheckInt(2)
		count := L.CheckInt(3)
		n := rows.Len()
		out := L.NewTable()

		if start < 1 {
			start = 1
		}
		end := start + count - 1
		if end > n {
			end = n
		}
		for i := start; i <= end; i++ {
			out.Append(rows.RawGetInt(i))
		}
		L.Push(out)
		return 1
	})

	return t
}

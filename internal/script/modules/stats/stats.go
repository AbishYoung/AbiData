// Package stats provides descriptive statistics, regressions, moving averages
// and distributions for numeric arrays and tabular data in Lua scripts.
package stats

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

func init() {
	script.Register(script.ModuleDef{Doc: doc, Build: build})
}

// MaxHistogramBins caps stats.histogram's bins.
const MaxHistogramBins = 10000

var allContexts = []script.Context{script.UIEvent, script.Report, script.Job}

var doc = script.ModuleDoc{
	Name:     "stats",
	Doc:      "Descriptive statistics, linear regression, time-series moving averages and distribution analysis. All functions accept either an array of numbers or (rows, field_name).",
	Contexts: allContexts,
	Funcs: []script.FuncDoc{
		{
			Name: "sum", Signature: "stats.sum(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "sum of elements, or nil if empty",
			Doc:     "Calculates the sum of numbers.",
			Example: `local total = stats.sum({10, 20, 30})`,
		},
		{
			Name: "mean", Signature: "stats.mean(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "arithmetic mean (average), or nil if empty",
			Doc:     "Calculates the arithmetic mean.",
			Example: `local avg = stats.mean(orders, "Total")`,
		},
		{
			Name: "median", Signature: "stats.median(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "median value, or nil if empty",
			Doc:     "Calculates the median (50th percentile).",
			Example: `local med = stats.median({1, 3, 3, 6, 7, 8, 9})`,
		},
		{
			Name: "mode", Signature: "stats.mode(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "most frequent value, or nil if empty",
			Doc:     "Finds the mode (most common value).",
			Example: `local m = stats.mode({1, 2, 2, 3})`,
		},
		{
			Name: "min", Signature: "stats.min(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "minimum value, or nil if empty",
			Doc:     "Finds the minimum value.",
			Example: `local low = stats.min(rates)`,
		},
		{
			Name: "max", Signature: "stats.max(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "maximum value, or nil if empty",
			Doc:     "Finds the maximum value.",
			Example: `local high = stats.max(rates)`,
		},
		{
			Name: "range", Signature: "stats.range(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "difference between max and min, or nil if empty",
			Doc:     "Calculates the range (max - min).",
			Example: `local span = stats.range({10, 2, 8})`,
		},
		{
			Name: "variance", Signature: "stats.variance(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "sample variance (n-1 denominator), or nil if count < 2",
			Doc:     "Calculates the unbiased sample variance.",
			Example: `local v = stats.variance({2, 4, 4, 4, 5, 5, 7, 9})`,
		},
		{
			Name: "pvariance", Signature: "stats.pvariance(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "population variance (n denominator), or nil if empty",
			Doc:     "Calculates the population variance.",
			Example: `local pv = stats.pvariance({2, 4, 4, 4, 5, 5, 7, 9})`,
		},
		{
			Name: "stdev", Signature: "stats.stdev(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "sample standard deviation, or nil if count < 2",
			Doc:     "Calculates the sample standard deviation.",
			Example: `local s = stats.stdev(sales, "Amount")`,
		},
		{
			Name: "pstdev", Signature: "stats.pstdev(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "population standard deviation, or nil if empty",
			Doc:     "Calculates the population standard deviation.",
			Example: `local ps = stats.pstdev(populationScores)`,
		},
		{
			Name: "percentile", Signature: "stats.percentile(xs, p [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "p", Type: "number", Doc: "Percentile (0 to 100, or 0.0 to 1.0)."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "interpolated percentile value, or nil if empty",
			Doc:     "Calculates the p-th percentile using linear interpolation.",
			Example: `local p95 = stats.percentile(latencies, 95)`,
		},
		{
			Name: "quantiles", Signature: "stats.quantiles(xs, probs [, field]) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "probs", Type: "table", Doc: "Array of probabilities (e.g. {0.25, 0.5, 0.75})."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "array of quantile values",
			Doc:     "Calculates multiple percentiles in a single pass.",
			Example: `local q = stats.quantiles(scores, {0.25, 0.5, 0.75})`,
		},
		{
			Name: "iqr", Signature: "stats.iqr(xs [, field]) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "interquartile range (Q75 - Q25), or nil if empty",
			Doc:     "Calculates the interquartile range.",
			Example: `local spread = stats.iqr(prices)`,
		},
		{
			Name: "summary", Signature: "stats.summary(xs [, field]) → table|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "{count, sum, mean, stdev, min, q25, median, q75, max, iqr} or nil if empty",
			Doc:     "Returns a comprehensive summary table of descriptive statistics.",
			Example: `local s = stats.summary(orders, "Total")
log.info("stats", {avg = s.mean, median = s.median, iqr = s.iqr})`,
		},
		{
			Name: "zscores", Signature: "stats.zscores(xs [, field]) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "array of standardized z-scores ((x - mean) / stdev)",
			Doc:     "Calculates the z-score for each value.",
			Example: `local z = stats.zscores({10, 20, 30, 40, 50})`,
		},
		{
			Name: "correl", Signature: "stats.correl(xs, ys) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "First array of numbers."},
				{Name: "ys", Type: "table", Doc: "Second array of numbers."},
			},
			Returns: "Pearson correlation coefficient r (-1.0 to 1.0), or nil if undefined",
			Doc:     "Calculates the Pearson product-moment correlation coefficient.",
			Example: `local r = stats.correl(adSpend, revenue)`,
		},
		{
			Name: "covariance", Signature: "stats.covariance(xs, ys) → number|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "First array of numbers."},
				{Name: "ys", Type: "table", Doc: "Second array of numbers."},
			},
			Returns: "sample covariance, or nil if count < 2",
			Doc:     "Calculates the sample covariance between two variables.",
			Example: `local cov = stats.covariance(xs, ys)`,
		},
		{
			Name: "linreg", Signature: "stats.linreg(xs, ys) → table|nil",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Independent variable values (x)."},
				{Name: "ys", Type: "table", Doc: "Dependent variable values (y)."},
			},
			Returns: "{slope, intercept, r, r2} or nil if insufficient points",
			Doc:     "Calculates ordinary least-squares linear regression (y = slope * x + intercept).",
			Example: `local fit = stats.linreg(x, y)
local forecast = fit.slope * nextX + fit.intercept`,
		},
		{
			Name: "moving_avg", Signature: "stats.moving_avg(xs, window) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers."},
				{Name: "window", Type: "number", Doc: "Window size in elements."},
			},
			Returns: "array of simple moving averages of length (#xs - window + 1)",
			Doc:     "Calculates the simple moving average over a sliding window.",
			Example: `local sma = stats.moving_avg(dailyRevenue, 7)`,
		},
		{
			Name: "ema", Signature: "stats.ema(xs, alpha) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers."},
				{Name: "alpha", Type: "number", Doc: "Smoothing factor between 0.0 and 1.0."},
			},
			Returns: "array of exponential moving averages of same length as xs",
			Doc:     "Calculates the exponential moving average (EMA).",
			Example: `local ema = stats.ema(prices, 0.2)`,
		},
		{
			Name: "cumsum", Signature: "stats.cumsum(xs) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers."},
			},
			Returns: "array of cumulative sums",
			Doc:     "Calculates the cumulative sum.",
			Example: `local running = stats.cumsum({1, 2, 3, 4}) -- {1, 3, 6, 10}`,
		},
		{
			Name: "diffs", Signature: "stats.diffs(xs) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers."},
			},
			Returns: "array of differences x[i] - x[i-1]",
			Doc:     "Calculates discrete differences between consecutive elements.",
			Example: `local d = stats.diffs({10, 15, 12}) -- {5, -3}`,
		},
		{
			Name: "pct_change", Signature: "stats.pct_change(xs) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers."},
			},
			Returns: "array of percentage changes (x[i] - x[i-1]) / x[i-1]",
			Doc:     "Calculates percentage changes between consecutive elements.",
			Example: `local p = stats.pct_change({100, 110, 99}) -- {0.1, -0.1}`,
		},
		{
			Name: "histogram", Signature: "stats.histogram(xs, bins [, field]) → table",
			Params: []script.ParamDoc{
				{Name: "xs", Type: "table", Doc: "Array of numbers or array of row tables."},
				{Name: "bins", Type: "number", Doc: "Number of equal-width bins (at least 1, at most 10000)."},
				{Name: "field", Type: "string", Opt: true, Doc: "Field name if xs is an array of row tables."},
			},
			Returns: "array of bin tables: { {min, max, count, ratio}, ... }",
			Doc:     "Computes equal-width frequency bins across the dataset's range.",
			Example: `local bins = stats.histogram(ages, 5)`,
		},
	},
}

func extractFloats(L *lua.LState, argIdx int) []float64 {
	tbl := L.CheckTable(argIdx)
	var field string
	if L.GetTop() >= argIdx+1 && L.Get(argIdx+1).Type() == lua.LTString {
		field = L.CheckString(argIdx + 1)
	}
	n := tbl.Len()
	out := make([]float64, 0, n)
	for i := 1; i <= n; i++ {
		val := tbl.RawGetInt(i)
		if field != "" {
			if rowTbl, ok := val.(*lua.LTable); ok {
				val = rowTbl.RawGetString(field)
			} else {
				continue
			}
		}
		switch num := val.(type) {
		case lua.LNumber:
			f := float64(num)
			if !math.IsNaN(f) && !math.IsInf(f, 0) {
				out = append(out, f)
			}
		case lua.LString:
			if f, err := strconv.ParseFloat(string(num), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				out = append(out, f)
			}
		}
	}
	return out
}

func calcPercentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p > 1.0 {
		p = p / 100.0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1.0 {
		return sorted[n-1]
	}
	idx := p * float64(n-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return sorted[lower]
	}
	fraction := idx - float64(lower)
	return sorted[lower] + fraction*(sorted[upper]-sorted[lower])
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	fn := func(name string, f lua.LGFunction) { t.RawSetString(name, L.NewFunction(f)) }

	fn("sum", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		L.Push(lua.LNumber(s))
		return 1
	})

	fn("mean", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		L.Push(lua.LNumber(s / float64(len(vals))))
		return 1
	})

	fn("median", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		sort.Float64s(vals)
		n := len(vals)
		if n%2 == 1 {
			L.Push(lua.LNumber(vals[n/2]))
		} else {
			m := (vals[n/2-1] + vals[n/2]) / 2.0
			L.Push(lua.LNumber(m))
		}
		return 1
	})

	fn("mode", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		counts := map[float64]int{}
		maxCount := 0
		var modeVal float64
		for _, v := range vals {
			counts[v]++
			if counts[v] > maxCount {
				maxCount = counts[v]
				modeVal = v
			}
		}
		L.Push(lua.LNumber(modeVal))
		return 1
	})

	fn("min", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		m := vals[0]
		for _, v := range vals[1:] {
			if v < m {
				m = v
			}
		}
		L.Push(lua.LNumber(m))
		return 1
	})

	fn("max", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		m := vals[0]
		for _, v := range vals[1:] {
			if v > m {
				m = v
			}
		}
		L.Push(lua.LNumber(m))
		return 1
	})

	fn("range", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		minV, maxV := vals[0], vals[0]
		for _, v := range vals[1:] {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
		L.Push(lua.LNumber(maxV - minV))
		return 1
	})

	fn("variance", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		n := len(vals)
		if n < 2 {
			L.Push(lua.LNil)
			return 1
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		mean := s / float64(n)
		var sqDiff float64
		for _, v := range vals {
			d := v - mean
			sqDiff += d * d
		}
		L.Push(lua.LNumber(sqDiff / float64(n-1)))
		return 1
	})

	fn("pvariance", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		n := len(vals)
		if n == 0 {
			L.Push(lua.LNil)
			return 1
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		mean := s / float64(n)
		var sqDiff float64
		for _, v := range vals {
			d := v - mean
			sqDiff += d * d
		}
		L.Push(lua.LNumber(sqDiff / float64(n)))
		return 1
	})

	fn("stdev", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		n := len(vals)
		if n < 2 {
			L.Push(lua.LNil)
			return 1
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		mean := s / float64(n)
		var sqDiff float64
		for _, v := range vals {
			d := v - mean
			sqDiff += d * d
		}
		variance := sqDiff / float64(n-1)
		L.Push(lua.LNumber(math.Sqrt(variance)))
		return 1
	})

	fn("pstdev", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		n := len(vals)
		if n == 0 {
			L.Push(lua.LNil)
			return 1
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		mean := s / float64(n)
		var sqDiff float64
		for _, v := range vals {
			d := v - mean
			sqDiff += d * d
		}
		variance := sqDiff / float64(n)
		L.Push(lua.LNumber(math.Sqrt(variance)))
		return 1
	})

	fn("percentile", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		p := L.CheckNumber(2)
		sort.Float64s(vals)
		res := calcPercentile(vals, float64(p))
		L.Push(lua.LNumber(res))
		return 1
	})

	fn("quantiles", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(L.NewTable())
			return 1
		}
		probsTable := L.CheckTable(2)
		sort.Float64s(vals)
		out := L.CreateTable(probsTable.Len(), 0)
		for i := 1; i <= probsTable.Len(); i++ {
			p := float64(probsTable.RawGetInt(i).(lua.LNumber))
			q := calcPercentile(vals, p)
			out.Append(lua.LNumber(q))
		}
		L.Push(out)
		return 1
	})

	fn("iqr", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		if len(vals) == 0 {
			L.Push(lua.LNil)
			return 1
		}
		sort.Float64s(vals)
		q25 := calcPercentile(vals, 0.25)
		q75 := calcPercentile(vals, 0.75)
		L.Push(lua.LNumber(q75 - q25))
		return 1
	})

	fn("summary", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		n := len(vals)
		if n == 0 {
			L.Push(lua.LNil)
			return 1
		}
		sort.Float64s(vals)
		var s float64
		for _, v := range vals {
			s += v
		}
		mean := s / float64(n)
		var stdev float64
		if n >= 2 {
			var sqDiff float64
			for _, v := range vals {
				d := v - mean
				sqDiff += d * d
			}
			stdev = math.Sqrt(sqDiff / float64(n-1))
		}
		q25 := calcPercentile(vals, 0.25)
		median := calcPercentile(vals, 0.50)
		q75 := calcPercentile(vals, 0.75)
		iqr := q75 - q25

		res := L.NewTable()
		res.RawSetString("count", lua.LNumber(n))
		res.RawSetString("sum", lua.LNumber(s))
		res.RawSetString("mean", lua.LNumber(mean))
		res.RawSetString("stdev", lua.LNumber(stdev))
		res.RawSetString("min", lua.LNumber(vals[0]))
		res.RawSetString("q25", lua.LNumber(q25))
		res.RawSetString("median", lua.LNumber(median))
		res.RawSetString("q75", lua.LNumber(q75))
		res.RawSetString("max", lua.LNumber(vals[n-1]))
		res.RawSetString("iqr", lua.LNumber(iqr))
		L.Push(res)
		return 1
	})

	fn("zscores", func(L *lua.LState) int {
		vals := extractFloats(L, 1)
		n := len(vals)
		out := L.CreateTable(n, 0)
		if n < 2 {
			for range vals {
				out.Append(lua.LNumber(0))
			}
			L.Push(out)
			return 1
		}
		var s float64
		for _, v := range vals {
			s += v
		}
		mean := s / float64(n)
		var sqDiff float64
		for _, v := range vals {
			d := v - mean
			sqDiff += d * d
		}
		stdev := math.Sqrt(sqDiff / float64(n-1))
		if stdev == 0 {
			for range vals {
				out.Append(lua.LNumber(0))
			}
		} else {
			for _, v := range vals {
				out.Append(lua.LNumber((v - mean) / stdev))
			}
		}
		L.Push(out)
		return 1
	})

	fn("correl", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		ys := extractFloats(L, 2)
		n := len(xs)
		if n != len(ys) || n < 2 {
			L.Push(lua.LNil)
			return 1
		}
		var sx, sy float64
		for i := 0; i < n; i++ {
			sx += xs[i]
			sy += ys[i]
		}
		mx, my := sx/float64(n), sy/float64(n)
		var sxy, sxx, syy float64
		for i := 0; i < n; i++ {
			dx := xs[i] - mx
			dy := ys[i] - my
			sxy += dx * dy
			sxx += dx * dx
			syy += dy * dy
		}
		denom := math.Sqrt(sxx * syy)
		if denom == 0 {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(sxy / denom))
		return 1
	})

	fn("covariance", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		ys := extractFloats(L, 2)
		n := len(xs)
		if n != len(ys) || n < 2 {
			L.Push(lua.LNil)
			return 1
		}
		var sx, sy float64
		for i := 0; i < n; i++ {
			sx += xs[i]
			sy += ys[i]
		}
		mx, my := sx/float64(n), sy/float64(n)
		var sxy float64
		for i := 0; i < n; i++ {
			sxy += (xs[i] - mx) * (ys[i] - my)
		}
		L.Push(lua.LNumber(sxy / float64(n-1)))
		return 1
	})

	fn("linreg", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		ys := extractFloats(L, 2)
		n := len(xs)
		if n != len(ys) || n < 2 {
			L.Push(lua.LNil)
			return 1
		}
		var sx, sy float64
		for i := 0; i < n; i++ {
			sx += xs[i]
			sy += ys[i]
		}
		mx, my := sx/float64(n), sy/float64(n)
		var sxy, sxx, syy float64
		for i := 0; i < n; i++ {
			dx := xs[i] - mx
			dy := ys[i] - my
			sxy += dx * dy
			sxx += dx * dx
			syy += dy * dy
		}
		if sxx == 0 {
			L.Push(lua.LNil)
			return 1
		}
		slope := sxy / sxx
		intercept := my - slope*mx
		rVal := 0.0
		denom := math.Sqrt(sxx * syy)
		if denom > 0 {
			rVal = sxy / denom
		}
		res := L.NewTable()
		res.RawSetString("slope", lua.LNumber(slope))
		res.RawSetString("intercept", lua.LNumber(intercept))
		res.RawSetString("r", lua.LNumber(rVal))
		res.RawSetString("r2", lua.LNumber(rVal*rVal))
		L.Push(res)
		return 1
	})

	fn("moving_avg", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		w := L.CheckInt(2)
		n := len(xs)
		if w <= 0 || w > n {
			L.Push(L.NewTable())
			return 1
		}
		outLen := n - w + 1
		out := L.CreateTable(outLen, 0)
		var windowSum float64
		for i := 0; i < w; i++ {
			windowSum += xs[i]
		}
		out.Append(lua.LNumber(windowSum / float64(w)))
		for i := w; i < n; i++ {
			windowSum += xs[i] - xs[i-w]
			out.Append(lua.LNumber(windowSum / float64(w)))
		}
		L.Push(out)
		return 1
	})

	fn("ema", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		alpha := float64(L.CheckNumber(2))
		if alpha <= 0 || alpha > 1 {
			L.ArgError(2, "alpha must be between 0.0 and 1.0")
		}
		n := len(xs)
		out := L.CreateTable(n, 0)
		if n == 0 {
			L.Push(out)
			return 1
		}
		current := xs[0]
		out.Append(lua.LNumber(current))
		for i := 1; i < n; i++ {
			current = alpha*xs[i] + (1.0-alpha)*current
			out.Append(lua.LNumber(current))
		}
		L.Push(out)
		return 1
	})

	fn("cumsum", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		n := len(xs)
		out := L.CreateTable(n, 0)
		var sum float64
		for _, v := range xs {
			sum += v
			out.Append(lua.LNumber(sum))
		}
		L.Push(out)
		return 1
	})

	fn("diffs", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		n := len(xs)
		if n <= 1 {
			L.Push(L.NewTable())
			return 1
		}
		out := L.CreateTable(n-1, 0)
		for i := 1; i < n; i++ {
			out.Append(lua.LNumber(xs[i] - xs[i-1]))
		}
		L.Push(out)
		return 1
	})

	fn("pct_change", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		n := len(xs)
		if n <= 1 {
			L.Push(L.NewTable())
			return 1
		}
		out := L.CreateTable(n-1, 0)
		for i := 1; i < n; i++ {
			prev := xs[i-1]
			if prev == 0 {
				out.Append(lua.LNumber(0))
			} else {
				out.Append(lua.LNumber((xs[i] - prev) / prev))
			}
		}
		L.Push(out)
		return 1
	})

	fn("histogram", func(L *lua.LState) int {
		xs := extractFloats(L, 1)
		bins := L.CheckInt(2)
		// The bins are built here, outside the VM's instruction and memory
		// accounting, so their number is capped.
		if bins <= 0 || bins > MaxHistogramBins {
			L.ArgError(2, fmt.Sprintf("bins must be between 1 and %d", MaxHistogramBins))
		}
		n := len(xs)
		out := L.CreateTable(bins, 0)
		if n == 0 {
			L.Push(out)
			return 1
		}
		minV, maxV := xs[0], xs[0]
		for _, v := range xs[1:] {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}

		binWidth := (maxV - minV) / float64(bins)
		if binWidth == 0 {
			binWidth = 1.0
		}
		counts := make([]int, bins)
		for _, v := range xs {
			idx := int((v - minV) / binWidth)
			if idx >= bins {
				idx = bins - 1
			}
			if idx < 0 {
				idx = 0
			}
			counts[idx]++
		}

		for i := 0; i < bins; i++ {
			b := L.NewTable()
			bMin := minV + float64(i)*binWidth
			bMax := minV + float64(i+1)*binWidth
			b.RawSetString("min", lua.LNumber(bMin))
			b.RawSetString("max", lua.LNumber(bMax))
			b.RawSetString("count", lua.LNumber(counts[i]))
			b.RawSetString("ratio", lua.LNumber(float64(counts[i])/float64(n)))
			out.Append(b)
		}
		L.Push(out)
		return 1
	})

	return t
}

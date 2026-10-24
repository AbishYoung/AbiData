package all_test

import (
	"math"
	"testing"

	"abidata/internal/script"
)

func num(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

func TestStatsModule(t *testing.T) {
	for _, ctx := range script.AllContexts {
		res, err := runWith(t, ctx, nil, `
			local xs = {10, 20, 30, 40, 50}
			local s = stats.sum(xs)
			local avg = stats.mean(xs)
			local med = stats.median(xs)
			local mn = stats.min(xs)
			local mx = stats.max(xs)
			local rng = stats.range(xs)
			local var_s = stats.variance(xs)
			local sd = stats.stdev(xs)
			local p50 = stats.percentile(xs, 50)
			local qs = stats.quantiles(xs, {0.5})
			local q50 = qs[1]
			local iqr_v = stats.iqr(xs)
			local summ = stats.summary(xs)
			local md = stats.mode({1, 2, 2, 3, 4})
			local z = stats.zscores({10, 20, 30})

			-- Bivariate & Regressions
			local cov = stats.covariance({1, 2, 3}, {1, 2, 3})
			local corr = stats.correl({1, 2, 3}, {2, 4, 6})
			local reg = stats.linreg({1, 2, 3, 4, 5}, {2, 4, 6, 8, 10})

			-- Moving / Sequence operations
			local sma = stats.moving_avg({1, 2, 3, 4, 5}, 3)
			local ema = stats.ema({1, 2, 3, 4, 5}, 0.5)
			local csum = stats.cumsum({1, 2, 3, 4})
			local dfs = stats.diffs({10, 15, 12, 20})
			local pct = stats.pct_change({100, 110, 99})
			local hist = stats.histogram({1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 2)

			-- Row-table inputs
			local rows = {
				{name = "Alice", score = 80},
				{name = "Bob", score = 90},
				{name = "Charlie", score = 70},
			}
			local row_sum = stats.sum(rows, "score")
			local row_mean = stats.mean(rows, "score")

			return s, avg, med, mn, mx, rng, var_s, sd, p50, q50, iqr_v,
			       summ.count, summ.sum, summ.mean, md, #z, cov, corr, reg.slope, reg.intercept, reg.r2,
			       #sma, sma[1], sma[2], sma[3], #ema, #csum, csum[4], #dfs, dfs[1], dfs[2], dfs[3],
			       #pct, pct[1], pct[2], #hist, hist[1].count, hist[2].count, row_sum, row_mean
		`)
		if err != nil {
			t.Fatalf("context %s: %v", ctx, err)
		}

		v := res.Values
		if v[0] != int64(150) || v[1] != int64(30) || v[2] != int64(30) || v[3] != int64(10) || v[4] != int64(50) || v[5] != int64(40) {
			t.Fatalf("context %s basic stats mismatch: %v", ctx, v[0:6])
		}
		if v[6] != int64(250) && v[6] != 250.0 { // sample variance: (400+100+0+100+400)/4 = 250
			t.Fatalf("context %s variance mismatch: %v", ctx, v[6])
		}
		if math.Abs(num(v[7])-math.Sqrt(250.0)) > 1e-6 {
			t.Fatalf("context %s stddev mismatch: %v", ctx, v[7])
		}
		if num(v[8]) != 30 || num(v[9]) != 30 {
			t.Fatalf("context %s percentile/quantile mismatch: %v, %v", ctx, v[8], v[9])
		}
		// summary count, sum, mean, mode
		if num(v[11]) != 5 || num(v[12]) != 150 || num(v[13]) != 30 || num(v[14]) != 2 {
			t.Fatalf("context %s summary/mode mismatch: %v", ctx, v[11:15])
		}
		// zscores len
		if num(v[15]) != 3 {
			t.Fatalf("context %s zscores length mismatch: %v", ctx, v[15])
		}
		// cov, corr, reg
		if math.Abs(num(v[16])-1.0) > 1e-6 || math.Abs(num(v[17])-1.0) > 1e-6 || math.Abs(num(v[18])-2.0) > 1e-6 || math.Abs(num(v[19])) > 1e-6 || math.Abs(num(v[20])-1.0) > 1e-6 {
			t.Fatalf("context %s bivariate mismatch: cov=%v, corr=%v, slope=%v, int=%v, r2=%v", ctx, v[16], v[17], v[18], v[19], v[20])
		}
		// sma: {2, 3, 4}
		if v[21] != int64(3) || v[22] != int64(2) || v[23] != int64(3) || v[24] != int64(4) {
			t.Fatalf("context %s sma mismatch: %v", ctx, v[21:25])
		}
		// csum: length 4, last is 10
		if v[26] != int64(4) || v[27] != int64(10) {
			t.Fatalf("context %s csum mismatch: %v", ctx, v[26:28])
		}
		// dfs: 5, -3, 8
		if v[28] != int64(3) || v[29] != int64(5) || v[30] != int64(-3) || v[31] != int64(8) {
			t.Fatalf("context %s diffs mismatch: %v", ctx, v[28:32])
		}
		// pct: 0.1, -0.1
		if v[32] != int64(2) || math.Abs(v[33].(float64)-0.1) > 1e-6 || math.Abs(v[34].(float64)-(-0.1)) > 1e-6 {
			t.Fatalf("context %s pct_change mismatch: %v", ctx, v[32:35])
		}
		// hist: 2 bins with count 5 each
		if v[35] != int64(2) || v[36] != int64(5) || v[37] != int64(5) {
			t.Fatalf("context %s histogram mismatch: %v", ctx, v[35:38])
		}
		// row extraction: sum 240, mean 80
		if v[38] != int64(240) || v[39] != int64(80) {
			t.Fatalf("context %s row-table stats mismatch: %v", ctx, v[38:40])
		}
	}
}

func TestStatsEdgeCases(t *testing.T) {
	res, err := runWith(t, script.UIEvent, nil, `
		local empty = {}
		local one = {42}
		return stats.sum(empty) == nil,
		       stats.mean(empty) == nil,
		       stats.median(empty) == nil,
		       stats.min(empty) == nil,
		       stats.max(empty) == nil,
		       stats.variance(one) == nil,
		       stats.stdev(one) == nil,
		       stats.sum(one),
		       stats.mean(one),
		       stats.min(one),
		       stats.max(one),
		       stats.correl({1, 2}, {1}) == nil,
		       stats.linreg({1}, {1}) == nil
	`)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{true, true, true, true, true, true, true, int64(42), int64(42), int64(42), int64(42), true, true}
	for i, w := range want {
		if res.Values[i] != w {
			t.Fatalf("edge case [%d]: got %v, want %v", i, res.Values[i], w)
		}
	}
}

func TestDataModule(t *testing.T) {
	for _, ctx := range script.AllContexts {
		res, err := runWith(t, ctx, nil, `
			local employees = {
				{id = 1, name = "Alice", dept = "Eng", role = "Lead", salary = 120000},
				{id = 2, name = "Bob", dept = "Eng", role = "Dev", salary = 90000},
				{id = 3, name = "Charlie", dept = "Sales", role = "Rep", salary = 70000},
				{id = 4, name = "Diana", dept = "Sales", role = "Lead", salary = 110000},
				{id = 5, name = "Evan", dept = "Eng", role = "Dev", salary = 85000},
			}

			-- group_by
			local by_dept = data.group_by(employees, "dept")

			-- aggregate / summarize
			local dept_summary = data.aggregate(employees, {
				by = "dept",
				metrics = {
					emp_count = "count",
					total_salary = {field = "salary", fn = "sum"},
					avg_salary = {field = "salary", fn = "avg"},
					min_salary = {field = "salary", fn = "min"},
					max_salary = {field = "salary", fn = "max"},
				}
			})
			-- sort the summary to have deterministic order
			local sorted_summary = data.sort_by(dept_summary, "dept")

			-- total dataset rollup (no "by")
			local total_rollup = data.summarize(employees, {
				metrics = {
					total_headcount = "count",
					overall_total = {field = "salary", fn = "sum"},
					median_salary = {field = "salary", fn = "median"},
				}
			})

			-- pivot
			local piv = data.pivot(employees, {
				rows = "dept",
				cols = "role",
				val = "salary",
				agg = "sum",
			})
			local sorted_piv = data.sort_by(piv, "dept")

			-- sort_by single key and multi-key with +/-
			local sorted_desc = data.sort_by(employees, "-salary")
			local sorted_multi = data.sort_by(employees, {"dept", "-salary"})

			-- pluck, index_by, distinct, frequencies, top_n, filter, slice
			local names = data.pluck(employees, "name")
			local indexed = data.index_by(employees, "id")
			local depts = data.distinct(employees, "dept")
			local freqs = data.frequencies(employees, "dept")
			local top2 = data.top_n(employees, 2, "salary", true)
			local eng_only = data.filter(employees, function(e) return e.dept == "Eng" end)
			local sliced = data.slice(employees, 2, 3)

			return #by_dept.Eng, #by_dept.Sales,
			       #sorted_summary, sorted_summary[1].dept, sorted_summary[1].emp_count, sorted_summary[1].total_salary, sorted_summary[1].avg_salary,
			       sorted_summary[2].dept, sorted_summary[2].emp_count, sorted_summary[2].total_salary,
			       #total_rollup, total_rollup[1].total_headcount, total_rollup[1].overall_total, total_rollup[1].median_salary,
			       #sorted_piv, sorted_piv[1].dept, sorted_piv[1].Dev, sorted_piv[1].Lead,
			       sorted_desc[1].name, sorted_desc[5].name,
			       sorted_multi[1].name, sorted_multi[2].name,
			       #names, names[1], names[3],
			       indexed["2"].name,
			       #depts, #freqs, freqs[1].value, freqs[1].count, freqs[2].value, freqs[2].count,
			       #top2, top2[1].name, top2[2].name,
			       #eng_only, #sliced, sliced[1].name, sliced[3].name
		`)
		if err != nil {
			t.Fatalf("context %s: %v", ctx, err)
		}

		v := res.Values
		// by_dept counts
		if v[0] != int64(3) || v[1] != int64(2) {
			t.Fatalf("context %s by_dept mismatch: %v, %v", ctx, v[0], v[1])
		}
		// sorted_summary: Eng has 3, total 295000, avg ≈ 98333.33
		if v[2] != int64(2) || v[3] != "Eng" || v[4] != int64(3) || v[5] != int64(295000) {
			t.Fatalf("context %s summary[1] mismatch: %v", ctx, v[2:6])
		}
		if math.Abs(v[6].(float64)-98333.333333) > 1 {
			t.Fatalf("context %s summary[1] avg mismatch: %v", ctx, v[6])
		}
		// Sales has 2, total 180000
		if v[7] != "Sales" || v[8] != int64(2) || v[9] != int64(180000) {
			t.Fatalf("context %s summary[2] mismatch: %v", ctx, v[7:10])
		}
		// total rollup: headcount 5, total 475000, median 90000
		if v[10] != int64(1) || v[11] != int64(5) || v[12] != int64(475000) || v[13] != int64(90000) {
			t.Fatalf("context %s total_rollup mismatch: %v", ctx, v[10:14])
		}
		// pivot: Eng Dev sum (90000+85000=175000), Lead sum (120000)
		if v[14] != int64(2) || v[15] != "Eng" || v[16] != int64(175000) || v[17] != int64(120000) {
			t.Fatalf("context %s pivot mismatch: %v", ctx, v[14:18])
		}
		// sorted_desc: top Alice (120k), bottom Charlie (70k)
		if v[18] != "Alice" || v[19] != "Charlie" {
			t.Fatalf("context %s sorted_desc mismatch: %v, %v", ctx, v[18], v[19])
		}
		// sorted_multi: Eng ordered by -salary => Alice (120k), then Bob (90k)
		if v[20] != "Alice" || v[21] != "Bob" {
			t.Fatalf("context %s sorted_multi mismatch: %v, %v", ctx, v[20], v[21])
		}
		// pluck: 5 names, 1st Alice, 3rd Charlie
		if v[22] != int64(5) || v[23] != "Alice" || v[24] != "Charlie" {
			t.Fatalf("context %s pluck mismatch: %v", ctx, v[22:25])
		}
		// index_by: id "2" is Bob
		if v[25] != "Bob" {
			t.Fatalf("context %s index_by mismatch: %v", ctx, v[25])
		}
		// distinct & freqs
		if v[26] != int64(2) || v[27] != int64(2) || v[28] != "Eng" || v[29] != int64(3) || v[30] != "Sales" || v[31] != int64(2) {
			t.Fatalf("context %s distinct/freqs mismatch: %v", ctx, v[26:32])
		}
		// top2: Alice (120k), Diana (110k)
		if v[32] != int64(2) || v[33] != "Alice" || v[34] != "Diana" {
			t.Fatalf("context %s top2 mismatch: %v", ctx, v[32:35])
		}
		// filter: Eng count 3; slice 2..4 => 3 items (Bob, Charlie, Diana)
		if v[35] != int64(3) || v[36] != int64(3) || v[37] != "Bob" || v[38] != "Diana" {
			t.Fatalf("context %s filter/slice mismatch: %v", ctx, v[35:39])
		}
	}
}

func TestDataEdgeCases(t *testing.T) {
	res, err := runWith(t, script.UIEvent, nil, `
		local empty = {}
		local g = data.group_by(empty, "dept")
		local s = data.aggregate(empty, {by = "dept", metrics = {count = "count"}})
		local p = data.pivot(empty, {rows = "a", cols = "b", val = "c"})
		local srt = data.sort_by(empty, "col")
		local plk = data.pluck(empty, "col")
		local idx = data.index_by(empty, "col")
		local dst = data.distinct(empty, "col")
		local frq = data.frequencies(empty, "col")
		local tp = data.top_n(empty, 5, "col")
		local flt = data.filter(empty, function() return true end)
		local slc = data.slice(empty, 1, 10)

		return json.encode(g), #s, #p, #srt, #plk, json.encode(idx), #dst, json.encode(frq), #tp, #flt, #slc
	`)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"{}", int64(0), int64(0), int64(0), int64(0), "{}", int64(0), "{}", int64(0), int64(0), int64(0)}
	for i, w := range want {
		if res.Values[i] != w {
			t.Fatalf("data edge case [%d]: got %v, want %v", i, res.Values[i], w)
		}
	}
}

// TestStatsHistogramBinsCapped: the bins are allocated outside the VM's
// accounting, so a script cannot ask for millions of them.
func TestStatsHistogramBinsCapped(t *testing.T) {
	for _, bins := range []string{"0", "10001", "2147483647"} {
		if _, err := runWith(t, script.UIEvent, nil, `return #stats.histogram({1, 2, 3}, `+bins+`)`); err == nil {
			t.Fatalf("histogram with %s bins succeeded", bins)
		}
	}
	res, err := runWith(t, script.UIEvent, nil, `return #stats.histogram({1, 2, 3}, 10000)`)
	if err != nil || res.Values[0] != int64(10000) {
		t.Fatalf("10000 bins: %v %v", res, err)
	}
}

package exprlang

import (
	"sort"
	"strings"

	"abidata/internal/sqlgen"
)

// suggest returns `; did you mean "x"?` for the closest candidate within a
// small edit distance, or "".
func suggest(name string, candidates []string) string {
	best, bestD := "", 3
	if len(name) <= 3 {
		bestD = 2
	}
	lower := strings.ToLower(name)
	for _, c := range candidates {
		d := distance(lower, strings.ToLower(c))
		if d < bestD || d == bestD && best != "" && c < best {
			best, bestD = c, d
		}
	}
	if best == "" || strings.EqualFold(best, name) {
		return ""
	}
	return `; did you mean "` + best + `"?`
}

// distance is the Levenshtein distance between a and b (bytes).
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// functionNames lists what an expression in mode may call, for suggestions
// and editor completion.
func functionNames(m Mode) []string {
	var out []string
	for _, f := range Functions(m) {
		out = append(out, f.Name)
	}
	return out
}

// Functions lists the catalog entries an expression in mode may call, sorted
// by name. Query expressions also get the aggregates.
func Functions(m Mode) []sqlgen.FuncDef {
	var out []sqlgen.FuncDef
	for _, n := range sqlgen.FuncNames() {
		f, _ := sqlgen.LookupFunc(n)
		if m == Stored && (!f.Deterministic || f.Aggregate) {
			continue
		}
		out = append(out, f)
	}
	if m == Query {
		for n := range aggs {
			out = append(out, sqlgen.FuncDef{Name: n, Aggregate: true, MinArgs: 1, MaxArgs: 1, TimeArg: -1})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

package exprlang

import (
	"fmt"
	"sort"
	"strings"

	"abidata/internal/sqlgen"
)

// FuncDoc documents one function for the expression reference and editor
// completion. Every catalog function has one (TestEveryFunctionIsDocumented).
type FuncDoc struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Summary   string `json:"summary"`
	// Stored reports whether calculated fields, validation rules and defaults
	// may use it; QueryOnly functions work only in query expression columns.
	Stored  bool   `json:"stored"`
	Group   string `json:"group"`
	Example string `json:"example,omitempty"`
}

type doc struct{ sig, summary, group, example string }

var docs = map[string]doc{
	// Text
	"char":         {"char(code, …)", "The text made of the given Unicode code points.", "Text", "char(65, 66) is 'AB'"},
	"concat":       {"concat(value, …)", "Joins the values as text, skipping NULLs.", "Text", "concat(First, ' ', Last)"},
	"concat_ws":    {"concat_ws(separator, value, …)", "Joins the values with the separator, skipping NULLs.", "Text", "concat_ws(', ', City, Country)"},
	"format":       {"format(pattern, value, …)", "Formats values like printf.", "Text", ""},
	"printf":       {"printf(pattern, value, …)", "Formats values: %d, %f, %s, %.2f, %05d and the other printf patterns.", "Text", "printf('%.2f', Price)"},
	"hex":          {"hex(value)", "The value's bytes as upper-case hexadecimal.", "Text", ""},
	"lower":        {"lower(text)", "The text in lower case (ASCII letters).", "Text", ""},
	"upper":        {"upper(text)", "The text in upper case (ASCII letters).", "Text", ""},
	"ltrim":        {"ltrim(text [, chars])", "Removes spaces (or the given characters) from the start.", "Text", ""},
	"rtrim":        {"rtrim(text [, chars])", "Removes spaces (or the given characters) from the end.", "Text", ""},
	"trim":         {"trim(text [, chars])", "Removes spaces (or the given characters) from both ends.", "Text", "trim(Code)"},
	"quote":        {"quote(value)", "The value written as a SQL literal.", "Text", ""},
	"replace":      {"replace(text, find, with)", "Replaces every occurrence of find.", "Text", "replace(Phone, ' ', '')"},
	"substr":       {"substr(text, start [, length])", "Part of the text; start counts from 1, a negative start from the end.", "Text", "substr(Code, 1, 3)"},
	"substring":    {"substring(text, start [, length])", "The same as substr.", "Text", ""},
	"typeof":       {"typeof(value)", "The storage class: 'null', 'integer', 'real', 'text' or 'blob'.", "Text", ""},
	"instr":        {"instr(text, find)", "The position of the first occurrence of find (from 1), or 0.", "Text", ""},
	"length":       {"length(text)", "The number of characters.", "Text", "length(Code) > 0"},
	"octet_length": {"octet_length(text)", "The number of bytes.", "Text", ""},
	"unicode":      {"unicode(text)", "The code point of the first character.", "Text", ""},
	"unhex":        {"unhex(text [, ignore])", "The bytes the hexadecimal text stands for.", "Text", ""},
	"glob":         {"glob(pattern, text)", "Case-sensitive match with * and ?, like text GLOB pattern.", "Text", ""},
	"like":         {"like(pattern, text [, escape])", "Case-insensitive match with % and _, like text LIKE pattern.", "Text", ""},
	// Numbers
	"abs":     {"abs(number)", "The number without its sign.", "Numbers", ""},
	"round":   {"round(number [, digits])", "Rounded to the given number of decimal places (default 0).", "Numbers", "round(Price * 1.2, 2)"},
	"ceil":    {"ceil(number)", "The smallest whole number not below it.", "Numbers", ""},
	"ceiling": {"ceiling(number)", "The same as ceil.", "Numbers", ""},
	"floor":   {"floor(number)", "The largest whole number not above it.", "Numbers", ""},
	"trunc":   {"trunc(number)", "The whole part, towards zero.", "Numbers", ""},
	"mod":     {"mod(a, b)", "The remainder of a divided by b, as a real number.", "Numbers", ""},
	"sign":    {"sign(number)", "-1, 0 or 1.", "Numbers", ""},
	"sqrt":    {"sqrt(number)", "The square root.", "Numbers", ""},
	"pow":     {"pow(base, exponent)", "base raised to exponent.", "Numbers", ""},
	"power":   {"power(base, exponent)", "The same as pow.", "Numbers", ""},
	"exp":     {"exp(number)", "e raised to the number.", "Numbers", ""},
	"ln":      {"ln(number)", "The natural logarithm.", "Numbers", ""},
	"log":     {"log([base,] number)", "The base-10 logarithm, or the logarithm in the given base.", "Numbers", ""},
	"log10":   {"log10(number)", "The base-10 logarithm.", "Numbers", ""},
	"log2":    {"log2(number)", "The base-2 logarithm.", "Numbers", ""},
	"pi":      {"pi()", "π.", "Numbers", ""},
	"degrees": {"degrees(radians)", "Converts radians to degrees.", "Numbers", ""},
	"radians": {"radians(degrees)", "Converts degrees to radians.", "Numbers", ""},
	"sin":     {"sin(radians)", "Sine.", "Numbers", ""},
	"cos":     {"cos(radians)", "Cosine.", "Numbers", ""},
	"tan":     {"tan(radians)", "Tangent.", "Numbers", ""},
	"asin":    {"asin(number)", "Arcsine, in radians.", "Numbers", ""},
	"acos":    {"acos(number)", "Arccosine, in radians.", "Numbers", ""},
	"atan":    {"atan(number)", "Arctangent, in radians.", "Numbers", ""},
	"atan2":   {"atan2(y, x)", "The angle of the point (x, y), in radians.", "Numbers", ""},
	"sinh":    {"sinh(number)", "Hyperbolic sine.", "Numbers", ""},
	"cosh":    {"cosh(number)", "Hyperbolic cosine.", "Numbers", ""},
	"tanh":    {"tanh(number)", "Hyperbolic tangent.", "Numbers", ""},
	"asinh":   {"asinh(number)", "Inverse hyperbolic sine.", "Numbers", ""},
	"acosh":   {"acosh(number)", "Inverse hyperbolic cosine.", "Numbers", ""},
	"atanh":   {"atanh(number)", "Inverse hyperbolic tangent.", "Numbers", ""},
	"random":  {"random()", "A random integer. Queries only.", "Numbers", ""},
	"max":     {"max(a, b, …)", "The largest of the values (NULL if any is NULL). In a query, max(column) is the largest value in the group.", "Choices", ""},
	"min":     {"min(a, b, …)", "The smallest of the values (NULL if any is NULL). In a query, min(column) is the smallest value in the group.", "Choices", ""},
	// Choices and NULLs
	"coalesce":   {"coalesce(a, b, …)", "The first value that is not NULL.", "Choices", "coalesce(Discount, 0)"},
	"ifnull":     {"ifnull(a, b)", "a, or b when a is NULL.", "Choices", "ifnull(Nickname, Name)"},
	"nullif":     {"nullif(a, b)", "NULL when a equals b, otherwise a.", "Choices", "Total / nullif(Count, 0)"},
	"iif":        {"iif(condition, then, else)", "then when the condition is true, otherwise else.", "Choices", "iif(Qty > 10, 'bulk', 'single')"},
	"if":         {"if(condition, then [, else …])", "The same as iif; more condition and value pairs may follow.", "Choices", ""},
	"likely":     {"likely(value)", "The value; a hint for the query planner.", "Choices", ""},
	"unlikely":   {"unlikely(value)", "The value; a hint for the query planner.", "Choices", ""},
	"likelihood": {"likelihood(value, probability)", "The value; a hint for the query planner.", "Choices", ""},
	// Dates
	"date":      {"date(time [, modifier …])", "The date as YYYY-MM-DD. Modifiers such as '+1 day', 'start of month'. date('now') is today (defaults only).", "Dates", "date(Ordered, '+30 days')"},
	"time":      {"time(time [, modifier …])", "The time as HH:MM:SS.", "Dates", ""},
	"datetime":  {"datetime(time [, modifier …])", "The date and time as YYYY-MM-DD HH:MM:SS.", "Dates", ""},
	"julianday": {"julianday(time [, modifier …])", "The Julian day number; subtract two to get the days between dates.", "Dates", "julianday(Shipped) - julianday(Ordered)"},
	"unixepoch": {"unixepoch(time [, modifier …])", "Seconds since 1970-01-01.", "Dates", ""},
	"strftime":  {"strftime(pattern, time [, modifier …])", "Formats a date: %Y year, %m month, %d day, %H:%M:%S time, %W week, %w weekday.", "Dates", "strftime('%Y-%m', Ordered)"},
	"timediff":  {"timediff(a, b)", "The time from b to a, as ±YYYY-MM-DD HH:MM:SS.SSS.", "Dates", ""},
	// JSON
	"json":                {"json(text)", "The JSON text, minified; an error if it is not JSON.", "JSON", ""},
	"json_valid":          {"json_valid(text [, flags])", "1 when the text is valid JSON.", "JSON", ""},
	"json_type":           {"json_type(json [, path])", "The JSON type at the path: 'object', 'array', 'text', 'integer', 'real', 'true', 'false' or 'null'.", "JSON", ""},
	"json_extract":        {"json_extract(json, path, …)", "The value at the path, such as '$.items[0].name'.", "JSON", "json_extract(Data, '$.colour')"},
	"json_array":          {"json_array(value, …)", "A JSON array of the values.", "JSON", ""},
	"json_object":         {"json_object(key, value, …)", "A JSON object of the key and value pairs.", "JSON", ""},
	"json_array_length":   {"json_array_length(json [, path])", "The number of elements in the array.", "JSON", ""},
	"json_insert":         {"json_insert(json, path, value, …)", "Adds values where the paths don't exist yet.", "JSON", ""},
	"json_replace":        {"json_replace(json, path, value, …)", "Replaces values where the paths exist.", "JSON", ""},
	"json_set":            {"json_set(json, path, value, …)", "Sets values, adding or replacing.", "JSON", ""},
	"json_remove":         {"json_remove(json, path, …)", "Removes the values at the paths.", "JSON", ""},
	"json_patch":          {"json_patch(target, patch)", "Applies an RFC 7396 merge patch.", "JSON", ""},
	"json_quote":          {"json_quote(value)", "The value as a JSON value.", "JSON", ""},
	"json_error_position": {"json_error_position(text)", "0 for valid JSON, otherwise the position of the first error.", "JSON", ""},
	// Locations and areas (ADR 0010)
	"geo_point":      {"geo_point(lat, lon)", "A location from latitude and longitude.", "Locations", ""},
	"geo_lat":        {"geo_lat(point)", "The latitude of a location.", "Locations", ""},
	"geo_lon":        {"geo_lon(point)", "The longitude of a location.", "Locations", ""},
	"geo_distance_m": {"geo_distance_m(a, b)", "The distance between two locations, in metres.", "Locations", "geo_distance_m(Spot, Depot) < 500"},
	"geo_within":     {"geo_within(point, area)", "1 when the location is inside the area.", "Locations", "geo_within(Spot, Zone)"},
	"geo_area_m2":    {"geo_area_m2(area)", "The area's size in square metres.", "Locations", "geo_area_m2(Fence)"},
	"geo_centroid":   {"geo_centroid(area)", "The area's centre.", "Locations", ""},
	"geo_bbox":       {"geo_bbox(area)", "The area's bounding box, as [west, south, east, north].", "Locations", ""},
	// Aggregates (queries only)
	"group_concat": {"group_concat(value [, separator])", "The group's values joined with commas (or the separator). Queries only.", "Totals", ""},
	"total":        {"total(number)", "The sum of the group's values, 0.0 when there are none. Queries only.", "Totals", ""},
	"count":        {"count(value) / count(*) / count(DISTINCT value)", "The number of values (or rows) in the group. Queries only.", "Totals", ""},
	"sum":          {"sum(number)", "The sum of the group's values. Queries only.", "Totals", ""},
	"avg":          {"avg(number)", "The average of the group's values. Queries only.", "Totals", ""},
}

// Docs lists every function with its documentation, by group and name.
func Docs() []FuncDoc {
	var out []FuncDoc
	for _, f := range Functions(Query) {
		d := docs[f.Name]
		out = append(out, FuncDoc{Name: f.Name, Signature: d.sig, Summary: d.summary, Group: d.group, Example: d.example,
			Stored: stored(f)})
	}
	order := map[string]int{"Text": 1, "Numbers": 2, "Choices": 3, "Dates": 4, "JSON": 5, "Locations": 6, "Totals": 7}
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].Group] != order[out[j].Group] {
			return order[out[i].Group] < order[out[j].Group]
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Markdown renders the expression reference page.
func Markdown() string {
	var b strings.Builder
	b.WriteString(`---
title: Expressions
order: 30
summary: The expression language for calculated fields, validation rules, defaults and query expression columns, with every function.
---

<!-- Generated by tools/gen/exprdocs from internal/exprlang; do not edit. -->

# Expressions

Calculated fields, validation rules, default values and expression columns in the visual query designer are written as **expressions**: SQL-style formulas such as ` + "`Quantity * Unit_price`" + ` or ` + "`length(Code) > 0`" + `. They are checked as you save, and an error says where the problem is and suggests a fix, for example ` + "`\"ifnul\" is not a function; did you mean \"ifnull\"?`" + `.

## What you can write

- **Fields** by name: ` + "`Quantity`" + `, or in double quotes when the name has spaces: ` + "`\"Unit price\"`" + `. Case doesn't matter. In a query, a field of a particular source is written ` + "`o.Quantity`" + `.
- **Values**: numbers (` + "`42`, `1.5`, `2e3`, `0x1F`" + `), text in single quotes (` + "`'it''s'`" + ` for a quote inside), ` + "`NULL`, `TRUE`, `FALSE`" + `.
- **Operators**, in order of precedence from tightest: ` + "`||`" + ` (join text); ` + "`*` `/` `%`" + `; ` + "`+` `-`" + `; ` + "`<` `<=` `>` `>=`" + `; ` + "`=` `<>` `IS` `IS NOT` `IN (…)` `LIKE` `GLOB` `BETWEEN … AND …` `IS NULL`" + `; ` + "`NOT`; `AND`; `OR`" + `. Use parentheses when in doubt.
- ` + "`CASE WHEN … THEN … ELSE … END`" + `, ` + "`CASE value WHEN … THEN … END`" + ` and ` + "`CAST(value AS TEXT | INTEGER | REAL)`" + `.
- ` + "`CURRENT_DATE`, `CURRENT_TIME`, `CURRENT_TIMESTAMP`" + ` (UTC).
- **Functions** from the list below.

There are no subqueries, statements, comments or parameters. Integer division truncates: ` + "`7 / 2`" + ` is 3, ` + "`7 / 2.0`" + ` is 3.5.

## Where each kind of expression may be used

| | Fields | Clock (` + "`'now'`" + `, ` + "`CURRENT_DATE`" + `) | Totals (` + "`sum`, `count`" + ` …) | ` + "`random()`" + ` |
|---|---|---|---|---|
| Calculated field | this table's | no | no | no |
| Validation rule | this table's | no | no | no |
| Default value | none | yes | no | no |
| Query expression column | the query's sources | yes | yes | yes |

Calculated fields and validation rules must give the same answer every time for the same record, so they cannot read the clock or use random numbers.

## Expressions written before this check

Expressions saved by an earlier version of AbiData were plain SQLite SQL. Those that fall outside this language keep working, and the table designer marks them **needs attention** with the reason. Rewrite them when you next edit the field; a changed expression must follow the rules on this page.

## Functions

`)
	group := ""
	for _, d := range Docs() {
		if d.Group != group {
			group = d.Group
			fmt.Fprintf(&b, "\n### %s\n\n| Function | What it gives | Example |\n|---|---|---|\n", group)
		}
		ex := ""
		if d.Example != "" {
			ex = "`" + d.Example + "`"
		}
		sum := d.Summary
		if !d.Stored && !strings.Contains(sum, "Queries only") {
			sum += " Queries only."
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", strings.ReplaceAll(d.Signature, "|", `\|`), sum, ex)
	}
	return b.String()
}

// stored reports whether f may appear in a stored expression.
func stored(f sqlgen.FuncDef) bool { return f.Deterministic && !f.Aggregate }

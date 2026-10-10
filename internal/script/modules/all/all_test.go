package all_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"abidata/internal/ids"
	"abidata/internal/script"
)

var runner = script.NewRunner(4, 2)

func exec(t *testing.T, src string, budget script.Budget) (script.ExecResult, error) {
	t.Helper()
	proto, err := script.Compile(src, "t")
	if err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return runner.Exec(context.Background(), script.ExecRequest{
		Kind: script.Job, Workspace: ids.NewWorkspaceID(), Proto: proto, TZ: ny, Budget: budget,
	})
}

// eval runs src and returns its results.
func eval(t *testing.T, src string) []any {
	t.Helper()
	res, err := exec(t, src, script.Budget{})
	if err != nil {
		t.Fatalf("%s\n=> %v", src, err)
	}
	return res.Values
}

func expect(t *testing.T, src string, want ...any) {
	t.Helper()
	got := eval(t, src)
	if len(got) != len(want) {
		t.Fatalf("%s\n=> %#v, want %#v", src, got, want)
	}
	for i := range want {
		if f, ok := want[i].(float64); ok && f == float64(int64(f)) {
			want[i] = int64(f)
		}
		if got[i] != want[i] {
			t.Fatalf("%s\n=> [%d] %#v, want %#v", src, i, got[i], want[i])
		}
	}
}

func expectErr(t *testing.T, src, contains string) {
	t.Helper()
	_, err := exec(t, src, script.Budget{})
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("%s\n=> error %v, want containing %q", src, err, contains)
	}
}

func TestDecimal(t *testing.T) {
	expect(t, `return decimal.new("0.1") + decimal.new("0.2") == decimal.new("0.3")`, true)
	expect(t, `return (decimal.new("0.1") + 0.2):tostring(), tostring(decimal.new("1.500")), (decimal.new(3) * "1.5"):tostring()`, "0.3", "1.5", "4.5")
	expect(t, `return (decimal.new(10) / 3):tostring(), (decimal.new(1) / 8):tostring()`, "3.3333333333333333", "0.125")
	expect(t, `return decimal.new("2.5"):round():tostring(), decimal.new("-2.5"):round():tostring(), decimal.new("1.005"):round(2):tostring(), decimal.new("1234"):round(-2):tostring()`, "3", "-3", "1.01", "1200")
	expect(t, `return decimal.new("1.25"):tonumber(), (-decimal.new(2)):tostring(), (decimal.new(7) % 3):tostring(), (decimal.new(-7) % 3):tostring()`, 1.25, "-2", "1", "2")
	expect(t, `return decimal.new("1e3"):tostring(), decimal.new(1e21):tostring(), decimal.new("  5 "):tostring()`, "1000", "1000000000000000000000", "5")
	expect(t, `local a, b = decimal.new(1), decimal.new(2) return a < b, a <= a, a > b, a >= b, a ~= b, a == decimal.new("1.0")`, true, true, false, false, true, true)
	// Documented Lua 5.1 limits: == with a number is false; ordering with a number raises.
	expect(t, `return decimal.new(1) == 1, pcall(function() return decimal.new(1) < 2 end)`, false, false, "t:1: attempt to compare userdata with number")
	expect(t, `return "x=" .. decimal.new("1.50") .. "!", decimal.is_decimal(decimal.new(1)), decimal.is_decimal(1), decimal.is_decimal("1")`, "x=1.5!", true, false, false)
	expect(t, `return getmetatable(decimal.new(1))`, "locked")
	expect(t, `return json.encode({decimal.new("12.30")})`, `["12.3"]`)
	expectErr(t, `return decimal.new(1) / 0`, "division by zero")
	expectErr(t, `return decimal.new(1) % 0`, "by zero")
	expectErr(t, `return decimal.new("abc")`, "invalid decimal")
	expectErr(t, `return decimal.new({})`, "cannot convert table")
	expectErr(t, `return decimal.new()`, "bad argument")
	expectErr(t, `return decimal.new(0/0)`, "NaN")
	expectErr(t, `return decimal.new(1) + {}`, "cannot convert table")
	expectErr(t, `return decimal.new(1):round(1000)`, "places")
	expectErr(t, `local x = decimal.new("99999999999") for i = 1, 40 do x = x * x end`, "out of range")
	// FromLua uses GoValue.
	if v := eval(t, `return decimal.new("12.340")`); v[0] != "12.34" {
		t.Fatalf("GoValue: %#v", v)
	}
}

func TestDatetime(t *testing.T) {
	expect(t, `return datetime.add("2026-03-08T06:30:00.000Z", {days = 1})`, "2026-03-09T05:30:00.000Z")
	expect(t, `return datetime.add("2026-03-08T06:30:00.000Z", {hours = 24})`, "2026-03-09T06:30:00.000Z")
	expect(t, `return datetime.add("2026-01-31", {months = 1}), datetime.add("2026-01-31", {years = 1, days = 1}), datetime.add("2026-01-31", {hours = 1})`, "2026-02-28", "2027-02-01", "2026-01-31T06:00:00.000Z")
	expect(t, `return datetime.add("2026-03-31T12:00:00.000Z", {months = -1, weeks = 1}, "UTC")`, "2026-03-07T12:00:00.000Z")
	expect(t, `return datetime.parse("2026-03-08 21:30", "America/New_York"), datetime.parse("2026-01-02")`, "2026-03-09T01:30:00.000Z", "2026-01-02T05:00:00.000Z")
	expect(t, `return datetime.parse("2026-01-02T10:00:00+02:00"), datetime.parse("2026-01-02 10:00:30", "UTC")`, "2026-01-02T08:00:00.000Z", "2026-01-02T10:00:30.000Z")
	expect(t, `local ts, err = datetime.parse("nonsense") return ts, err ~= nil`, nil, true)
	expect(t, `return datetime.format("2026-03-09T05:30:00.000Z", "%Y-%m-%d %H:%M:%S %y %b %B %a %A %p %I %j %Z %z %%")`, "2026-03-09 01:30:00 26 Mar March Mon Monday AM 01 068 EDT -0400 %")
	expect(t, `return datetime.format("2026-03-09T17:05:00.000Z", "%I%p %Z", "Asia/Tokyo")`, "02AM JST")
	expect(t, `return datetime.diff("2026-01-01", "2026-03-15", "days", "UTC"), datetime.diff("2026-01-01T00:00:00Z", "2026-01-01T01:30:00Z", "hours")`, 73.0, 1.5)
	expect(t, `return datetime.diff("2026-01-31", "2026-02-28", "months"), datetime.diff("2026-01-31", "2026-03-31", "months"), datetime.diff("2026-03-31", "2026-01-01", "months"), datetime.diff("2020-06-15", "2026-06-14", "years"), datetime.diff("2020-06-15", "2026-06-15", "years")`, 1.0, 2.0, -2.0, 5.0, 6.0)
	expect(t, `return datetime.diff("2026-01-08", "2026-01-01", "weeks", "UTC"), datetime.diff("2026-01-01T00:00:00Z", "2026-01-01T00:02:00Z", "minutes"), datetime.diff("2026-01-01T00:00:00Z", "2026-01-01T00:00:03Z", "seconds")`, -1.0, 2.0, 3.0)
	expect(t, `local n = datetime.now() return #n, n:sub(-1), #datetime.today(), #datetime.today("Asia/Tokyo")`, 24.0, "Z", 10.0, 10.0)
	expectErr(t, `return datetime.format("2026-01-01", "%Q")`, "unknown directive")
	expectErr(t, `return datetime.today("Mars/Base")`, "unknown time zone")
	expectErr(t, `return datetime.diff("2026-01-01", "2026-01-02", "fortnights")`, "unknown unit")
	expectErr(t, `return datetime.add("2026-01-01", {days = 1.5})`, "whole number")
	expectErr(t, `return datetime.add("2026-01-01", {decades = 1})`, "unknown key")
	expectErr(t, `return datetime.add("bad", {days = 1})`, "cannot parse")
	expectErr(t, `return datetime.add("9999-12-31", {years = 5})`, "out of range")
}

func TestJSON(t *testing.T) {
	expect(t, `return json.encode({1, 2, 3}), json.encode({}), json.encode(json.array({})), json.encode(json.object({1, 2})), json.encode({b = 1, a = "x", c = {d = true}})`,
		`[1,2,3]`, `{}`, `[]`, `{"1":1,"2":2}`, `{"a":"x","b":1,"c":{"d":true}}`)
	expect(t, `return json.encode({json.null, db.NULL, 1.5, 2^53, -3, 1e300 / 1e290}), json.encode("a\"b\n<é\1")`, `[null,null,1.5,9007199254740992,-3,10000000000]`, `"a\"b\n<é\u0001"`)
	expect(t, `return json.encode({a = {1, {b = 2}}}, {pretty = true})`, "{\n  \"a\": [\n    1,\n    {\n      \"b\": 2\n    }\n  ]\n}")
	expect(t, `local v = json.decode('{"a":[1,2,{"b":null}],"e":[],"o":{},"big":12345678901234567890,"f":1.5,"s":"x"}')
		return json.encode(v), v.a[3].b == json.null, v.big, v.f`,
		`{"a":[1,2,{"b":null}],"big":"12345678901234567890","e":[],"f":1.5,"o":{},"s":"x"}`, true, "12345678901234567890", 1.5)
	expect(t, `local v, err = json.decode('{"a":') return v, type(err)`, nil, "string")
	expect(t, `local v, err = json.decode('1 2') return v, type(err)`, nil, "string")
	expect(t, `return json.decode("null") == json.null, json.decode("3"), json.decode('"x"'), json.decode("true")`, true, 3.0, "x", true)
	expect(t, `local t = setmetatable(json.array({}), nil) return json.encode(t)`, `{}`)
	expect(t, `return json.null == json.null, tostring(json.null)`, true, "json.null")
	expectErr(t, `return json.encode(function() end)`, "cannot encode")
	expectErr(t, `return json.encode({[true] = 1})`, "keys must be")
	expectErr(t, `return json.encode(0/0)`, "NaN")
	expectErr(t, `return json.encode(1/0)`, "infinity")
	expectErr(t, `local a = {} a.self = a return json.encode(a)`, "cyclic")
	expectErr(t, `local a = {} local c = a for i = 1, 150 do c.x = {} c = c.x end return json.encode(a)`, "deeper")
	expectErr(t, `return json.encode({}, {bogus = 1})`, "unknown option")
	expectErr(t, `return json.encode(json.array({a = 1}))`, "array has keys")
	expectErr(t, `return json.array(5)`, "bad argument")
	// Deep decode input is rejected, not crashed.
	expect(t, `local v, err = json.decode(string.rep("[", 150) .. string.rep("]", 150)) return v, type(err)`, nil, "string")
}

func TestCSV(t *testing.T) {
	expect(t, `local r = csv.parse("name,qty\nbolt,3\n\"a,b\",\"x\"\"y\"\n") return #r, r[1].name, r[1].qty, r[2].name, r[2].qty`, 2.0, "bolt", "3", "a,b", `x"y`)
	expect(t, `local r = csv.parse("\239\187\191a,,a,a\n1,2,3,4,5\n6\n") return r[1].a, r[1].column_2, r[1].a_2, r[1].a_3, r[1].column_5, r[2].a, r[2].column_2`, "1", "2", "3", "4", "5", "6", "")
	expect(t, `local r = csv.parse("a;b\n 1 ; 2 \n", {header = false, delimiter = ";", trim = true}) return #r, r[2][1], r[2][2], #r[1]`, 2.0, "1", "2", 2.0)
	expect(t, `local n = 0 for row in csv.rows("k\n1\n2\n3\n") do n = n + tonumber(row.k) end return n`, 6.0)
	expect(t, `local r, err = csv.parse('a\n"x') return r, err`, nil, `line 2: extraneous or missing " in quoted-field`)
	expect(t, `local r, err = csv.parse('a\nx"y') return r, err`, nil, `line 2: bare " in non-quoted-field`)
	expect(t, `return #csv.parse("")`, 0.0)
	expect(t, `return csv.encode({{"a", "b,c"}, {1, 2.5, true, nil, db.NULL, decimal.new("1.50")}})`, "a,\"b,c\"\n1,2.5,true,,,1.5\n")
	expect(t, `return csv.encode({{sku = "A1", qty = 2, note = 'say "hi"\nnow'}}, {columns = {"sku", "qty", "note"}}), csv.encode({{sku = "A1"}}, {columns = {"sku"}, header = false, delimiter = ";"}), csv.encode({}, {columns = {"x", "y"}})`,
		"sku,qty,note\nA1,2,\"say \"\"hi\"\"\nnow\"\n", "A1\n", "x,y\n")
	expect(t, `local rows = {{a = "1", b = "x y"}, {a = "2", b = "q,r"}}
		local back = csv.parse(csv.encode(rows, {columns = {"a", "b"}}))
		return #back, back[1].a, back[2].b`, 2.0, "1", "q,r")
	expectErr(t, `return csv.encode({{a = 1}})`, "columns is required")
	expectErr(t, `return csv.encode({{{}}})`, "unsupported value")
	expectErr(t, `return csv.parse("a", {delimiter = "ab"})`, "delimiter")
	expectErr(t, `return csv.parse("a", {bogus = true})`, "unknown option")
	expectErr(t, `for r in csv.rows('a\n"x') do end`, "line 2")
}

func TestLog(t *testing.T) {
	res, err := exec(t, `log.debug("d") log.info(42, {user = "ann", n = 3, dec = decimal.new("1.5"), nested = {1, 2}}) log.warn(nil) log.error(string.rep("é", 5000))
		print(pcall(log.info, "m", {1, 2}))`, script.Budget{})
	if err != nil {
		t.Fatal(err)
	}
	var levels []string
	for _, l := range res.Logs {
		levels = append(levels, l.Level)
	}
	if strings.Join(levels, ",") != "debug,info,warn,error,info" {
		t.Fatalf("levels %v", levels)
	}
	if m := res.Logs[1]; m.Message != "42" || m.Fields["user"] != "ann" || m.Fields["n"] != int64(3) || m.Fields["dec"] != "1.5" {
		t.Fatalf("%+v", m)
	}
	if m := res.Logs[2].Message; m != "nil" {
		t.Fatal(m)
	}
	if m := res.Logs[3].Message; len(m) > 8192+len("…") || !strings.HasSuffix(m, "…") {
		t.Fatalf("truncation: %d bytes", len(m))
	}
	if !strings.Contains(res.Logs[4].Message, "false") {
		t.Fatalf("bad fields accepted: %q", res.Logs[4].Message)
	}
	expectErr(t, `log.info("x", "notatable")`, "fields")
	expectErr(t, `log.info("x", {a = string.rep("x", 9000)})`, "too large")
}

func TestStringLimits(t *testing.T) {
	small := script.Budget{Wall: 10 * time.Second, MaxString: 1024}
	for name, src := range map[string]string{
		"json": `local t = {} for i = 1, 1000 do t[i] = "item" .. i end local ok = pcall(json.encode, t) return ok`,
		"csv":  `local t = {} for i = 1, 1000 do t[i] = {"item" .. i} end local ok = pcall(csv.encode, t) return ok`,
		"fmt":  `return datetime.format("2026-01-01", string.rep("%A", 200))`,
	} {
		_, err := exec(t, src, small)
		var se *script.Error
		if !errors.As(err, &se) || se.Kind != script.KindLimit {
			t.Fatalf("%s: want KindLimit, got %v", name, err)
		}
	}
}

func TestParity(t *testing.T) {
	want := map[string][]string{}
	for _, d := range script.Manifest() {
		switch d.Name {
		case "decimal", "datetime", "json", "csv", "log":
		default:
			continue
		}
		for _, f := range d.Funcs {
			want[d.Name] = append(want[d.Name], f.Name)
		}
		for _, v := range d.Values {
			want[d.Name] = append(want[d.Name], v.Name)
		}
		sort.Strings(want[d.Name])
	}
	if len(want) != 5 {
		t.Fatalf("modules missing from manifest: %v", want)
	}
	for mod, names := range want {
		res, err := exec(t, `local k = {} for name in pairs(`+mod+`) do k[#k+1] = name end table.sort(k) return table.concat(k, ",")`, script.Budget{})
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Values[0].(string); got != strings.Join(names, ",") {
			t.Errorf("%s: built %q, documented %q", mod, got, strings.Join(names, ","))
		}
		for _, d := range script.Manifest() {
			if d.Name != mod {
				continue
			}
			if len(d.Doc) == 0 || len(d.Contexts) != 3 {
				t.Errorf("%s: incomplete module doc", mod)
			}
			for _, f := range d.Funcs {
				if f.Signature == "" || f.Doc == "" || f.Example == "" || f.Returns == "" && f.Name != "debug" && f.Name != "info" && f.Name != "warn" && f.Name != "error" {
					t.Errorf("%s.%s: incomplete function doc", mod, f.Name)
				}
			}
		}
	}
}

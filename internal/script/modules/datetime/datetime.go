// Package datetime registers the `datetime` script module. Timestamps are ISO
// UTC strings ("2006-01-02T15:04:05.000Z"); dates are "2006-01-02".
package datetime

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	_ "time/tzdata" // embed the IANA database so tz names work on any host

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

const (
	tsLayout   = "2006-01-02T15:04:05.000Z"
	dateLayout = "2006-01-02"
	maxUnits   = 1e9 // bound on years/months/days/weeks in add
	maxSeconds = 1e11
)

var allCtx = []script.Context{script.UIEvent, script.Report, script.Job}

func init() {
	script.Register(script.ModuleDef{Doc: doc(), Build: build})
}

func doc() script.ModuleDoc {
	tz := script.ParamDoc{Name: "tz", Type: "string", Opt: true, Doc: "IANA time zone name such as \"America/New_York\". Defaults to the workspace time zone."}
	return script.ModuleDoc{
		Name: "datetime",
		Doc: "Dates and times. A timestamp is an ISO 8601 UTC string like \"2026-03-09T05:30:00.000Z\"; a date is \"2026-01-31\". " +
			"Timestamps and dates are plain strings, so they sort and compare correctly with < and ==. " +
			"Calendar arithmetic and local-time parsing use the workspace time zone unless a tz argument is given. Invalid tz names raise an error.",
		Contexts: allCtx,
		Funcs: []script.FuncDoc{
			{
				Name: "now", Signature: "datetime.now()", Returns: "string",
				Doc:     "Returns the current time as an ISO UTC timestamp (millisecond precision).",
				Example: "local started = datetime.now()  -- \"2026-03-09T05:30:00.123Z\"",
			},
			{
				Name: "today", Signature: "datetime.today(tz?)", Params: []script.ParamDoc{tz}, Returns: "string",
				Errors:  []string{"unknown time zone"},
				Doc:     "Returns today's date (YYYY-MM-DD) in the time zone.",
				Example: "local d = datetime.today(\"Europe/Paris\")",
			},
			{
				Name: "parse", Signature: "datetime.parse(s, tz?)",
				Params: []script.ParamDoc{
					{Name: "s", Type: "string", Doc: "RFC 3339 (any offset), \"YYYY-MM-DD\" (midnight in tz), \"YYYY-MM-DD HH:MM\" or \"YYYY-MM-DD HH:MM:SS\" (local time in tz)."},
					tz,
				},
				Returns: "string|nil, string|nil",
				Errors:  []string{"unknown time zone", "s is not a string"},
				Doc:     "Parses a date/time into an ISO UTC timestamp. On failure returns nil and an error message instead of raising.",
				Example: "local ts, err = datetime.parse(\"2026-03-08 21:30\", \"America/New_York\")\nif not ts then error(err) end",
			},
			{
				Name: "format", Signature: "datetime.format(ts, layout, tz?)",
				Params: []script.ParamDoc{
					{Name: "ts", Type: "string", Doc: "A timestamp or date (any form accepted by datetime.parse)."},
					{Name: "layout", Type: "string", Doc: "strftime-style layout. Supported: %Y %m %d %H %M %S %y %b %B %a %A %p %I %j %Z %z %%."},
					tz,
				},
				Returns: "string",
				Errors:  []string{"ts cannot be parsed", "unknown directive in layout", "unknown time zone", "result exceeds the maximum string size (SCRIPT_LIMIT_EXCEEDED)"},
				Doc:     "Formats a time in the time zone. %Z is the zone abbreviation and %z the numeric offset such as +0100.",
				Example: "return datetime.format(\"2026-03-09T05:30:00.000Z\", \"%d %b %Y %I:%M %p\", \"America/New_York\")  -- \"09 Mar 2026 01:30 AM\"",
			},
			{
				Name: "add", Signature: "datetime.add(ts, spec, tz?)",
				Params: []script.ParamDoc{
					{Name: "ts", Type: "string", Doc: "A timestamp or date."},
					{Name: "spec", Type: "table", Doc: "Any of years, months, days, weeks (whole numbers, may be negative) and hours, minutes, seconds (fractions allowed)."},
					tz,
				},
				Returns: "string",
				Errors:  []string{"ts cannot be parsed", "unknown key in spec", "non-integer years/months/days/weeks", "result out of range", "unknown time zone"},
				Doc:     "Adds calendar and clock amounts. years, months, days and weeks use calendar arithmetic in the time zone so local clock time is kept across daylight-saving changes; adding months clamps to the end of shorter months (Jan 31 + 1 month = Feb 28). hours, minutes and seconds are exact elapsed time. When ts is a date and only years/months/days/weeks are given the result is a date, otherwise a timestamp.",
				Example: "datetime.add(\"2026-03-08T06:30:00.000Z\", {days = 1}, \"America/New_York\")  -- \"2026-03-09T05:30:00.000Z\"",
			},
			{
				Name: "diff", Signature: "datetime.diff(a, b, unit, tz?)",
				Params: []script.ParamDoc{
					{Name: "a", Type: "string", Doc: "Start timestamp or date."},
					{Name: "b", Type: "string", Doc: "End timestamp or date."},
					{Name: "unit", Type: "string", Doc: "One of \"seconds\", \"minutes\", \"hours\", \"days\", \"weeks\", \"months\", \"years\"."},
					tz,
				},
				Returns: "number",
				Errors:  []string{"a or b cannot be parsed", "unknown unit", "unknown time zone"},
				Doc:     "Returns b - a in the unit. seconds to weeks are exact elapsed time (a day is 24 hours) and may be fractional. months and years count whole calendar months/years in the time zone, truncated toward zero.",
				Example: "datetime.diff(\"2026-01-01\", \"2026-03-15\", \"days\")  -- 73",
			},
		},
	}
}

type mod struct {
	r    *script.Run
	locs map[string]*time.Location
}

func build(r *script.Run) lua.LValue {
	m := &mod{r: r, locs: map[string]*time.Location{}}
	L := r.L
	t := L.NewTable()
	t.RawSetString("now", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(time.Now().UTC().Format(tsLayout)))
		return 1
	}))
	t.RawSetString("today", L.NewFunction(func(L *lua.LState) int {
		loc := m.loc(L, 1)
		L.Push(lua.LString(time.Now().In(loc).Format(dateLayout)))
		return 1
	}))
	t.RawSetString("parse", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		loc := m.loc(L, 2)
		tm, _, err := parseTime(s, loc)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(tm.UTC().Format(tsLayout)))
		return 1
	}))
	t.RawSetString("format", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		layout := L.CheckString(2)
		loc := m.loc(L, 3)
		tm := mustParse(L, 1, s, loc)
		out, err := strftime(tm.In(loc), layout, r.Budget.MaxString)
		if err != nil {
			if errors.Is(err, errTooLong) {
				r.RaiseLimit("datetime.format result exceeds max string size")
			}
			L.ArgError(2, err.Error())
		}
		L.Push(lua.LString(out))
		return 1
	}))
	t.RawSetString("add", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		spec := L.CheckTable(2)
		loc := m.loc(L, 3)
		L.Push(lua.LString(add(L, s, spec, loc)))
		return 1
	}))
	t.RawSetString("diff", L.NewFunction(func(L *lua.LState) int {
		as, bs := L.CheckString(1), L.CheckString(2)
		unit := L.CheckString(3)
		loc := m.loc(L, 4)
		a, b := mustParse(L, 1, as, loc), mustParse(L, 2, bs, loc)
		L.Push(lua.LNumber(diff(L, a, b, unit, loc)))
		return 1
	}))
	return t
}

// loc resolves the optional tz argument at index n (default: workspace tz).
func (m *mod) loc(L *lua.LState, n int) *time.Location {
	v := L.Get(n)
	if v == lua.LNil {
		if m.r.TZ != nil {
			return m.r.TZ
		}
		return time.UTC
	}
	name, ok := v.(lua.LString)
	if !ok {
		L.ArgError(n, "time zone name (string) expected, got "+v.Type().String())
	}
	if l, ok := m.locs[string(name)]; ok {
		return l
	}
	if name == "" || strings.EqualFold(string(name), "local") {
		L.ArgError(n, fmt.Sprintf("unknown time zone %q", string(name)))
	}
	l, err := time.LoadLocation(string(name))
	if err != nil {
		L.ArgError(n, fmt.Sprintf("unknown time zone %q", string(name)))
	}
	m.locs[string(name)] = l
	return l
}

func mustParse(L *lua.LState, arg int, s string, loc *time.Location) time.Time {
	tm, _, err := parseTime(s, loc)
	if err != nil {
		L.ArgError(arg, err.Error())
	}
	return tm
}

// parseTime accepts the forms documented on datetime.parse. isDate reports a
// bare YYYY-MM-DD input.
func parseTime(s string, loc *time.Location) (tm time.Time, isDate bool, err error) {
	s = strings.TrimSpace(s)
	if tm, err = time.Parse(time.RFC3339, s); err == nil {
		return tm, false, nil
	}
	for _, f := range []struct {
		layout string
		date   bool
	}{{dateLayout, true}, {"2006-01-02 15:04", false}, {"2006-01-02 15:04:05", false}} {
		if tm, err = time.ParseInLocation(f.layout, s, loc); err == nil {
			return tm, f.date, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("cannot parse %q: expected RFC 3339, YYYY-MM-DD, YYYY-MM-DD HH:MM or YYYY-MM-DD HH:MM:SS", truncate(s, 40))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func daysIn(y int, mo time.Month) int {
	return time.Date(y, mo+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// addMonths shifts tm by n calendar months, clamping the day to the month end.
func addMonths(tm time.Time, n int, loc *time.Location) time.Time {
	y, mo, d := tm.Date()
	hh, mi, ss := tm.Clock()
	total := y*12 + int(mo) - 1 + n
	ny, nm := total/12, total%12
	if nm < 0 {
		nm += 12
		ny--
	}
	if dim := daysIn(ny, time.Month(nm+1)); d > dim {
		d = dim
	}
	return time.Date(ny, time.Month(nm+1), d, hh, mi, ss, tm.Nanosecond(), loc)
}

func add(L *lua.LState, s string, spec *lua.LTable, loc *time.Location) string {
	tm, isDate, err := parseTime(s, loc)
	if err != nil {
		L.ArgError(1, err.Error())
	}
	var years, months, days, weeks int
	var secs float64
	calendarOnly := true
	spec.ForEach(func(k, v lua.LValue) {
		key, ok := k.(lua.LString)
		if !ok {
			L.ArgError(2, "spec keys must be strings")
		}
		n, ok := v.(lua.LNumber)
		if !ok {
			L.ArgError(2, fmt.Sprintf("%s must be a number", key))
		}
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			L.ArgError(2, fmt.Sprintf("%s must be finite", key))
		}
		switch key {
		case "years", "months", "days", "weeks":
			if f != math.Trunc(f) {
				L.ArgError(2, fmt.Sprintf("%s must be a whole number", key))
			}
			if math.Abs(f) > maxUnits {
				L.ArgError(2, fmt.Sprintf("%s is out of range", key))
			}
			switch key {
			case "years":
				years = int(f)
			case "months":
				months = int(f)
			case "days":
				days = int(f)
			case "weeks":
				weeks = int(f)
			}
		case "hours":
			secs += f * 3600
			calendarOnly = false
		case "minutes":
			secs += f * 60
			calendarOnly = false
		case "seconds":
			secs += f
			calendarOnly = false
		default:
			L.ArgError(2, fmt.Sprintf("unknown key %q (use years, months, days, weeks, hours, minutes, seconds)", string(key)))
		}
	})
	if math.Abs(secs) > maxSeconds {
		L.ArgError(2, "time amount is out of range")
	}
	dateOnly := isDate && calendarOnly
	base := tm.In(loc)
	if dateOnly {
		base = time.Date(tm.Year(), tm.Month(), tm.Day(), 0, 0, 0, 0, time.UTC)
		loc = time.UTC
	}
	res := addMonths(base, years*12+months, loc).AddDate(0, 0, days+7*weeks)
	res = res.Add(time.Duration(math.Round(secs * 1e9)))
	if y := res.UTC().Year(); y < 1 || y > 9999 {
		L.RaiseError("datetime.add: result out of range")
	}
	if dateOnly {
		return res.Format(dateLayout)
	}
	return res.UTC().Format(tsLayout)
}

func diff(L *lua.LState, a, b time.Time, unit string, loc *time.Location) float64 {
	secs := float64(b.Unix()-a.Unix()) + float64(b.Nanosecond()-a.Nanosecond())/1e9
	switch unit {
	case "seconds":
		return secs
	case "minutes":
		return secs / 60
	case "hours":
		return secs / 3600
	case "days":
		return secs / 86400
	case "weeks":
		return secs / (7 * 86400)
	case "months", "years":
		a, b = a.In(loc), b.In(loc)
		n := (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
		if n > 0 && b.Before(addMonths(a, n, loc)) {
			n--
		} else if n < 0 && b.After(addMonths(a, n, loc)) {
			n++
		}
		if unit == "years" {
			return float64(n / 12)
		}
		return float64(n)
	}
	L.ArgError(3, fmt.Sprintf("unknown unit %q (use seconds, minutes, hours, days, weeks, months or years)", unit))
	return 0
}

package query

import (
	"encoding/json"
	"strings"
	"testing"

	"abidata/internal/fault"
)

func TestCheckParams(t *testing.T) {
	n := func(s string) any { return json.Number(s) }
	declared := func(typ string) []Param { return []Param{{Name: "p", Type: typ}} }
	cases := []struct {
		typ  string
		val  any
		want string // "" = accepted, otherwise a fragment of the message
	}{
		{"integer", n("5"), ""}, {"integer", "7", ""}, {"integer", n("1.5"), "whole number"}, {"integer", "abc", "whole number"}, {"integer", "", "whole number"},
		{"integer", true, "whole number"}, {"integer", n("99999999999999999999"), "whole number"},
		{"number", n("1.5"), ""}, {"number", "2.5e3", ""}, {"number", "x", "a number"}, {"number", "NaN", "a number"}, {"number", "0x10", "a number"},
		{"currency", "12.50", ""}, {"currency", n("12.5"), ""}, {"currency", "12.12345", "amount such as 12.50"}, {"currency", "abc", "amount such as 12.50"}, {"currency", "1,000", "amount such as 12.50"},
		{"yes_no", true, ""}, {"yes_no", "yes", ""}, {"yes_no", n("1"), ""}, {"yes_no", "maybe", "yes or no"}, {"yes_no", n("2"), "yes or no"},
		{"date", "2026-06-01", ""}, {"date", "06/01/2026", "a date"}, {"date", "2026-02-30", "a date"}, {"date", "", "a date"}, {"date", n("20260601"), "a date"},
		{"datetime", "2026-06-01T09:30:00Z", ""}, {"datetime", "2026-06-01", "a date and time"},
		{"text", "anything", ""}, {"text", n("5"), ""},
		{"date", nil, ""}, // empty binds as NULL
	}
	for _, c := range cases {
		err := CheckParams(declared(c.typ), Params{"p": c.val})
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s %#v: unexpected error %v", c.typ, c.val, err)
		case c.want != "":
			fe := fault.Classify(err)
			if err == nil || fe.Code != fault.BadRequest || !strings.Contains(fe.Message, c.want) || !strings.Contains(fe.Message, "Parameter p ") {
				t.Errorf("%s %#v: want error containing %q, got %v", c.typ, c.val, c.want, err)
			}
		}
	}
	// Undeclared and missing parameters are not this check's business.
	if err := CheckParams(declared("date"), Params{"other": "x"}); err != nil {
		t.Fatal(err)
	}
}

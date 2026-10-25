package reports

import "testing"

func TestRuleMatch(t *testing.T) {
	neg := []FormatRule{{Operator: "<", Value: "0", Color: "var(--paper-danger)", Bold: true, Label: "negative"}}
	cases := []struct {
		rules []FormatRule
		raw   any
		hit   bool
	}{
		{neg, "-5.00", true},
		{neg, int64(-1), true},
		{neg, float64(-0.5), true},
		{neg, "5", false},
		{neg, nil, false},
		{[]FormatRule{{Operator: ">=", Value: "100"}}, "100", true},
		{[]FormatRule{{Operator: ">=", Value: "100"}}, "99.99", false},
		{[]FormatRule{{Operator: "=", Value: "overdue"}}, "OVERDUE", true},
		{[]FormatRule{{Operator: "!=", Value: "ok"}}, "late", true},
		{[]FormatRule{{Operator: "!=", Value: "ok"}}, nil, true},
		{[]FormatRule{{Operator: "is_empty"}}, "  ", true},
		{[]FormatRule{{Operator: "is_not_empty"}}, "x", true},
		{[]FormatRule{{Operator: "is_not_empty"}}, nil, false},
		{[]FormatRule{{Operator: "<", Value: "m"}}, "apple", true},
	}
	for i, c := range cases {
		if got := ruleMatch(c.rules, c.raw) != nil; got != c.hit {
			t.Errorf("case %d (%v): got %v want %v", i, c.raw, got, c.hit)
		}
	}
	fx := ruleMatch(neg, "-1")
	if fx == nil || fx.Label != "negative" || !fx.Bold {
		t.Fatalf("fx %+v", fx)
	}
	if fx := ruleMatch([]FormatRule{{Operator: "is_empty", Background: "#fee"}}, ""); fx == nil || fx.Label != "highlighted" {
		t.Fatalf("default label: %+v", fx)
	}
	// first match wins
	two := []FormatRule{{Operator: "<", Value: "0", Label: "a"}, {Operator: "<", Value: "10", Label: "b"}}
	if ruleMatch(two, "-1").Label != "a" {
		t.Fatal("first rule must win")
	}
}

func TestStyleValidation(t *testing.T) {
	ok := map[string]string{"border_top": "solid", "border_bottom": "double", "font_family": "serif", "vertical_align": "middle", "text_decoration": "underline", "border_width": "2", "border_color": "#333", "color": "var(--paper-danger)"}
	if err := validateStyle("c", ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]string{
		{"border_bottom": "groove"}, {"font_family": "comic"}, {"border_width": "9"}, {"color": "url(javascript:x)"}, {"background": "red;position:fixed"}, {"vertical_align": "sideways"},
	} {
		if validateStyle("c", bad) == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
	if validateRules("c", []FormatRule{{Operator: "~", Value: "1"}}) == nil || validateRules("c", []FormatRule{{Operator: "<"}}) == nil ||
		validateRules("c", []FormatRule{{Operator: "<", Value: "1", Color: "expression(1)"}}) == nil {
		t.Fatal("bad rules accepted")
	}
	many := make([]FormatRule, MaxRules+1)
	for i := range many {
		many[i] = FormatRule{Operator: "is_empty"}
	}
	if validateRules("c", many) == nil {
		t.Fatal("too many rules accepted")
	}
	if validateBandColors("detail", &Band{AlternateBackground: "x;y"}) == nil {
		t.Fatal("bad band colour accepted")
	}
}

func TestRulesInStream(t *testing.T) {
	f := setup(t)
	f.seed(t, [][5]string{
		{"West", "Cy", "1000.10", "3", "2026-03-01"},
		{"East", "Al", "-250.50", "1", "2026-03-02"},
	})
	lay := salesLayout(f, "")
	lay.Bands.Detail.AlternateBackground = "var(--paper-zebra)"
	lay.Bands.Detail.Controls[1].Rules = []FormatRule{{Operator: "<", Value: "0", Color: "var(--paper-danger)", Bold: true, Label: "loss"}}
	var withFx, details int
	for _, in := range f.run(t, lay) {
		if in.Band != "detail" {
			continue
		}
		details++
		if fx, ok := in.Fx["amt"]; ok {
			withFx++
			if fx.Color != "var(--paper-danger)" || !fx.Bold || fx.Label != "loss" || in.Values["amt"] != "-250.50" {
				t.Fatalf("fx %+v values %v", fx, in.Values)
			}
		}
	}
	if details != 2 || withFx != 1 {
		t.Fatalf("details=%d with fx=%d", details, withFx)
	}
}

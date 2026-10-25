package reports

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCurrencyOptions(t *testing.T) {
	two, zero := 2, 0
	cases := []struct {
		name string
		o    CurrencyOptions
		in   string
		want string
	}{
		{"default prefix", CurrencyOptions{}, "1234.5", "$1,234.50"},
		{"euro suffix spaced", CurrencyOptions{Symbol: "€", Placement: "suffix", Spaced: true}, "1234.5", "1,234.50 €"},
		{"minus", CurrencyOptions{Symbol: "£"}, "-1234.5", "-£1,234.50"},
		{"paren", CurrencyOptions{Negative: "paren"}, "-1234.5", "($1,234.50)"},
		{"sign after", CurrencyOptions{Negative: "sign_after"}, "-5", "$5.00-"},
		{"suffix paren", CurrencyOptions{Symbol: "CHF", Placement: "suffix", Spaced: true, Negative: "paren"}, "-7", "(7.00 CHF)"},
		{"zero decimals", CurrencyOptions{Symbol: "¥", Decimals: &zero}, "1999.6", "¥2,000"},
		{"explicit two", CurrencyOptions{Decimals: &two}, "0", "$0.00"},
		{"rounds to zero loses sign", CurrencyOptions{Negative: "paren"}, "-0.001", "$0.00"},
	}
	for _, c := range cases {
		d, _ := decimal.NewFromString(c.in)
		if got := c.o.render(d); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestPatternMasks(t *testing.T) {
	cases := []struct {
		mask, in, want string
	}{
		{`#,##0.00;(#,##0.00);"-"`, "1234.5", "1,234.50"},
		{`#,##0.00;(#,##0.00);"-"`, "-1234.5", "(1,234.50)"},
		{`#,##0.00;(#,##0.00);"-"`, "0", "-"},
		{`#,##0.00`, "-3", "-3.00"},
		{`0000`, "42", "0042"},
		{`#.00`, "0.5", ".50"},
		{`0.0%`, "0.256", "25.6%"},
		{`"$"#,##0`, "1500", "$1,500"},
		{`#,##0 "kg"`, "1500", "1,500 kg"},
		{`#,##0;-#,##0;`, "0", ""},
	}
	for _, c := range cases {
		mk, err := parseMask(c.mask)
		if err != nil {
			t.Fatalf("%s: %v", c.mask, err)
		}
		d, _ := decimal.NewFromString(c.in)
		if got := applyMask(mk, d); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.mask, c.in, got, c.want)
		}
	}
	for _, bad := range []string{"abc", "0;0;0;0", "0.00000000000"} {
		if _, err := parseMask(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestDatePatterns(t *testing.T) {
	tm := time.Date(2026, 3, 9, 14, 5, 7, 0, time.UTC)
	cases := map[string]string{
		"YYYY-MM-DD":          "2026-03-09",
		"DD/MM/YYYY":          "09/03/2026",
		"MM/DD/YYYY":          "03/09/2026",
		"MMM D, YYYY":         "Mar 9, 2026",
		"YYYY-MM-DD HH:mm:ss": "2026-03-09 14:05:07",
		"dddd, MMMM D":        "Monday, March 9",
		"h:mm A":              "2:05 PM",
		"[Week of] D MMM":     "Week of 9 Mar",
	}
	for pat, want := range cases {
		if got := formatDatePattern(tm, pat); got != want {
			t.Errorf("%s: got %q want %q", pat, got, want)
		}
	}
}

func TestFormatControlPrefixSuffix(t *testing.T) {
	c := ReportControl{Format: "fixed:1", Suffix: " kg", Prefix: "~"}
	if got := formatControl(c, int64(3), nil, time.UTC); got != "~3.0 kg" {
		t.Fatalf("got %v", got)
	}
	if got := formatControl(c, nil, nil, time.UTC); got != "" {
		t.Fatalf("empty value must stay empty, got %v", got)
	}
	p := ReportControl{Format: "pattern:#,##0;(#,##0)"}
	if got := formatControl(p, "-1234", nil, time.UTC); got != "(1,234)" {
		t.Fatalf("got %v", got)
	}
	cur := ReportControl{Format: "currency", Currency: &CurrencyOptions{Symbol: "€", Placement: "suffix", Negative: "paren"}}
	if got := formatControl(cur, "-12.5", nil, time.UTC); got != "(12.50€)" {
		t.Fatalf("got %v", got)
	}
}

func TestValidateFormatOptions(t *testing.T) {
	if validateFormat("c", "pattern:not a mask") == nil {
		t.Fatal("mask without digits must be rejected")
	}
	if validateFormat("c", "pattern:#,##0.00;(#,##0.00)") != nil || validateFormat("c", "date:short") != nil || validateFormat("c", "weird") != nil {
		t.Fatal("valid and legacy formats must pass")
	}
	long := ""
	for i := 0; i < 40; i++ {
		long += "x"
	}
	if validateAffixes("c", &ReportControl{Suffix: long}) == nil {
		t.Fatal("overlong suffix must be rejected")
	}
	bad := 9
	if validateCurrency("c", &CurrencyOptions{Decimals: &bad}) == nil || validateCurrency("c", &CurrencyOptions{Negative: "x"}) == nil {
		t.Fatal("bad currency options must be rejected")
	}
}

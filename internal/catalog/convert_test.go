package catalog

import (
	"math"
	"testing"
)

func TestConvertValue(t *testing.T) {
	type tc struct {
		name     string
		from, to FieldType
		in       any
		want     any
		ok       bool
	}
	i := func(n int64) any { return n }
	tests := []tc{
		// identity and nil
		{"identity", Integer, Integer, i(5), i(5), true},
		{"identity nil", Date, Date, nil, nil, true},
		{"identity calc", Calculated, Calculated, "x", "x", true},
		{"nil to text", Integer, ShortText, nil, nil, true},
		{"nil to yes_no", Integer, YesNo, nil, i(0), true},
		{"nil to integer", ShortText, Integer, nil, nil, true},
		{"nil to autonumber", Integer, Autonumber, nil, nil, false},
		{"nil to calculated", Integer, Calculated, nil, nil, false},
		{"int to autonumber", Integer, Autonumber, i(1), nil, false},
		{"text to calculated", ShortText, Calculated, "a", nil, false},

		// to text
		{"int to short", Integer, ShortText, i(42), "42", true},
		{"neg int to long", Integer, LongText, i(-42), "-42", true},
		{"int min to text", Integer, LongText, i(math.MinInt64), "-9223372036854775808", true},
		{"2^53+1 to text", Integer, LongText, i(1<<53 + 1), "9007199254740993", true},
		{"float to text", Number, ShortText, 1.5, "1.5", true},
		{"float shortest", Number, ShortText, 0.1, "0.1", true},
		{"float big", Number, ShortText, 1e21, "1e+21", true},
		{"float integral", Number, ShortText, 3.0, "3", true},
		{"currency to text", Currency, ShortText, i(123400), "12.34", true},
		{"currency to text 4", Currency, LongText, i(-1), "-0.0001", true},
		{"currency whole", Currency, LongText, i(50000), "5", true},
		{"currency zero", Currency, LongText, i(0), "0", true},
		{"yes to text", YesNo, ShortText, i(1), "Yes", true},
		{"no to text", YesNo, LongText, i(0), "No", true},
		{"date to text", Date, ShortText, "2026-03-01", "2026-03-01", true},
		{"datetime to text", DateTime, LongText, "2026-03-01T00:00:00.000Z", "2026-03-01T00:00:00.000Z", true},
		{"short to long", ShortText, LongText, "abc", "abc", true},
		{"long to short", LongText, ShortText, "abc", "abc", true},
		{"bytes to text", LongText, ShortText, []byte("hi"), "hi", true},
		{"bad bytes to text", LongText, ShortText, []byte{0xff}, nil, false},
		{"NUL to text", LongText, ShortText, "a\x00", nil, false},
		{"autonumber to text", Autonumber, ShortText, i(7), "7", true},
		{"calculated to long", Calculated, LongText, 2.5, "2.5", true},
		{"lookup to text", Lookup, ShortText, i(9), "9", true},

		// hyperlink
		{"text to link", ShortText, Hyperlink, "https://a.b/c", "https://a.b/c", true},
		{"text to link http", LongText, Hyperlink, "http://a", "http://a", true},
		{"text to link bad", ShortText, Hyperlink, "a.b", nil, false},
		{"text to link no host", ShortText, Hyperlink, "https://", nil, false},
		{"int to link", Integer, Hyperlink, i(5), nil, false},
		{"link to text", Hyperlink, ShortText, "https://a.b", "https://a.b", true},
		{"link to long", Hyperlink, LongText, "https://a.b", "https://a.b", true},

		// to integer
		{"int to lookup", Integer, Lookup, i(5), i(5), true},
		{"lookup to int", Lookup, Integer, i(5), i(5), true},
		{"autonumber to int", Autonumber, Integer, i(5), i(5), true},
		{"float integral", Number, Integer, 3.0, i(3), true},
		{"float negative integral", Number, Integer, -3.0, i(-3), true},
		{"float frac", Number, Integer, 3.5, nil, false},
		{"float too big", Number, Integer, 1e19, nil, false},
		{"float 2^63", Number, Integer, 9223372036854775808.0, nil, false},
		{"float -2^63", Number, Integer, -9223372036854775808.0, i(math.MinInt64), true},
		{"float NaN", Number, Integer, math.NaN(), nil, false},
		{"float Inf", Number, Integer, math.Inf(1), nil, false},
		{"text int", ShortText, Integer, "42", i(42), true},
		{"text int spaces", ShortText, Integer, "  42  ", i(42), true},
		{"text int plus", ShortText, Integer, "+42", i(42), true},
		{"text int neg", ShortText, Integer, "-42", i(-42), true},
		{"text int max", LongText, Integer, "9223372036854775807", i(math.MaxInt64), true},
		{"text int overflow", LongText, Integer, "9223372036854775808", nil, false},
		{"text 2^53+1", LongText, Integer, "9007199254740993", i(1<<53 + 1), true},
		{"text zero fraction", ShortText, Integer, "3.0", i(3), true},
		{"text exp", ShortText, Integer, "1e3", i(1000), true},
		{"text fraction", ShortText, Integer, "3.5", nil, false},
		{"text junk", ShortText, Integer, "abc", nil, false},
		{"text empty", ShortText, Integer, "", nil, false},
		{"text NaN", ShortText, Integer, "NaN", nil, false},
		{"text huge exp", ShortText, Integer, "1e999999999", nil, false},
		{"currency divisible", Currency, Integer, i(50000), i(5), true},
		{"currency negative divisible", Currency, Integer, i(-50000), i(-5), true},
		{"currency not divisible", Currency, Integer, i(50001), nil, false},
		{"yes to int", YesNo, Integer, i(1), i(1), true},
		{"date to int", Date, Integer, "2026-03-01", nil, false},
		{"datetime to int", DateTime, Integer, "2026-03-01T00:00:00.000Z", nil, false},
		{"text to lookup", ShortText, Lookup, " 7 ", i(7), true},

		// to number
		{"int to number", Integer, Number, i(5), 5.0, true},
		{"float to number", Calculated, Number, 1.5, 1.5, true},
		{"text to number", ShortText, Number, " 1.25 ", 1.25, true},
		{"text exp to number", ShortText, Number, "1e3", 1000.0, true},
		{"text junk to number", ShortText, Number, "1,5", nil, false},
		{"text NaN to number", ShortText, Number, "NaN", nil, false},
		{"text Inf to number", ShortText, Number, "Inf", nil, false},
		{"text overflow to number", ShortText, Number, "1e999", nil, false},
		{"currency to number", Currency, Number, i(123400), 12.34, true},
		{"currency tiny to number", Currency, Number, i(-1), -0.0001, true},
		{"yes to number", YesNo, Number, i(0), 0.0, true},
		{"date to number", Date, Number, "2026-03-01", nil, false},
		{"datetime to number", DateTime, Number, "2026-03-01T00:00:00.000Z", nil, false},

		// to currency
		{"int to currency", Integer, Currency, i(5), i(50000), true},
		{"neg int to currency", Integer, Currency, i(-5), i(-50000), true},
		{"int overflow to currency", Integer, Currency, i(math.MaxInt64), nil, false},
		{"int min overflow to currency", Integer, Currency, i(math.MinInt64), nil, false},
		{"int max fit", Integer, Currency, i(math.MaxInt64 / 10000), i(math.MaxInt64 / 10000 * 10000), true},
		{"float to currency", Number, Currency, 12.34, i(123400), true},
		{"float 0.1", Number, Currency, 0.1, i(1000), true},
		{"float 0.0001", Number, Currency, 0.0001, i(1), true},
		{"float negative", Number, Currency, -2.5, i(-25000), true},
		{"float 5 places", Number, Currency, 0.00005, nil, false},
		{"float 12.34567", Number, Currency, 12.34567, nil, false},
		{"float huge", Number, Currency, 1e300, nil, false},
		{"float NaN to currency", Number, Currency, math.NaN(), nil, false},
		{"text to currency", ShortText, Currency, "12.34", i(123400), true},
		{"text dollar", ShortText, Currency, "$12.34", i(123400), true},
		{"text euro", ShortText, Currency, "€1,234.50", i(12345000), true},
		{"text pound neg", ShortText, Currency, "-£1,234,567.89", i(-12345678900), true},
		{"text sign then symbol", ShortText, Currency, "-$5", i(-50000), true},
		{"text thousands", LongText, Currency, "1,000", i(10000000), true},
		{"text bad thousands", ShortText, Currency, "1,00", nil, false},
		{"text european", ShortText, Currency, "1,5", nil, false},
		{"text bad thousands 2", ShortText, Currency, "1234,567", nil, false},
		{"text comma in fraction", ShortText, Currency, "1.2,345", nil, false},
		{"text 5 places", ShortText, Currency, "0.12345", nil, false},
		{"text -0.0001", ShortText, Currency, "-0.0001", i(-1), true},
		{"text overflow", ShortText, Currency, "922337203685477.5808", nil, false},
		{"text max", ShortText, Currency, "922337203685477.5807", i(math.MaxInt64), true},
		{"text junk currency", ShortText, Currency, "abc", nil, false},
		{"text symbol only", ShortText, Currency, "$", nil, false},
		{"yes to currency", YesNo, Currency, i(1), nil, false},
		{"date to currency", Date, Currency, "2026-03-01", nil, false},

		// to yes_no
		{"int 0", Integer, YesNo, i(0), i(0), true},
		{"int 1", Integer, YesNo, i(1), i(1), true},
		{"int 2", Integer, YesNo, i(2), nil, false},
		{"int -1", Integer, YesNo, i(-1), nil, false},
		{"float 1", Number, YesNo, 1.0, i(1), true},
		{"float 0.5", Number, YesNo, 0.5, nil, false},
		{"currency 0", Currency, YesNo, i(0), i(0), true},
		{"currency 1.00", Currency, YesNo, i(10000), i(1), true},
		{"currency 0.0001", Currency, YesNo, i(1), nil, false},
		{"text true", ShortText, YesNo, "true", i(1), true},
		{"text TRUE", ShortText, YesNo, " TRUE ", i(1), true},
		{"text Yes", ShortText, YesNo, "Yes", i(1), true},
		{"text y", ShortText, YesNo, "y", i(1), true},
		{"text 1", ShortText, YesNo, "1", i(1), true},
		{"text on", ShortText, YesNo, "ON", i(1), true},
		{"text false", ShortText, YesNo, "False", i(0), true},
		{"text no", ShortText, YesNo, "no", i(0), true},
		{"text n", ShortText, YesNo, "N", i(0), true},
		{"text 0", ShortText, YesNo, "0", i(0), true},
		{"text off", ShortText, YesNo, "off", i(0), true},
		{"text maybe", ShortText, YesNo, "maybe", nil, false},
		{"text empty", ShortText, YesNo, "", nil, false},
		{"date to yes", Date, YesNo, "2026-03-01", nil, false},

		// to date
		{"text date", ShortText, Date, "2026-03-01", "2026-03-01", true},
		{"text date spaces", ShortText, Date, " 2026-03-01 ", "2026-03-01", true},
		{"text leap day", ShortText, Date, "2028-02-29", "2028-02-29", true},
		{"text non-leap", ShortText, Date, "2026-02-29", nil, false},
		{"text feb 30", ShortText, Date, "2026-02-30", nil, false},
		{"text junk date", ShortText, Date, "03/01/2026", nil, false},
		{"text midnight dt", ShortText, Date, "2026-03-01T00:00:00Z", "2026-03-01", true},
		{"text midnight dt ms", LongText, Date, "2026-03-01T00:00:00.000Z", "2026-03-01", true},
		{"text midnight offset", ShortText, Date, "2026-03-01T02:00:00+02:00", "2026-03-01", true},
		{"text midnight offset prev day", ShortText, Date, "2026-03-01T00:00:00+02:00", nil, false},
		{"text non-midnight dt", ShortText, Date, "2026-03-01T00:00:01Z", nil, false},
		{"text sub-ms dt", ShortText, Date, "2026-03-01T00:00:00.0005Z", "2026-03-01", true},
		{"text 1ms dt", ShortText, Date, "2026-03-01T00:00:00.001Z", nil, false},
		{"datetime midnight to date", DateTime, Date, "2026-03-01T00:00:00.000Z", "2026-03-01", true},
		{"datetime noon to date", DateTime, Date, "2026-03-01T12:00:00.000Z", nil, false},
		{"int to date", Integer, Date, i(20260301), nil, false},
		{"float to date", Number, Date, 1.0, nil, false},

		// to datetime
		{"date to datetime", Date, DateTime, "2026-03-01", "2026-03-01T00:00:00.000Z", true},
		{"leap date to datetime", Date, DateTime, "2028-02-29", "2028-02-29T00:00:00.000Z", true},
		{"text date to datetime", ShortText, DateTime, "2026-03-01", "2026-03-01T00:00:00.000Z", true},
		{"text rfc to datetime", ShortText, DateTime, "2026-03-01T12:30:45+05:30", "2026-03-01T07:00:45.000Z", true},
		{"text rfc frac to datetime", LongText, DateTime, "2026-03-01T12:30:45.123789Z", "2026-03-01T12:30:45.123Z", true},
		{"text offset negative", LongText, DateTime, "2026-03-01T23:00:00-05:00", "2026-03-02T04:00:00.000Z", true},
		{"text junk to datetime", ShortText, DateTime, "yesterday", nil, false},
		{"text bad day to datetime", ShortText, DateTime, "2026-02-30T00:00:00Z", nil, false},
		{"text no zone to datetime", ShortText, DateTime, "2026-03-01T12:30:45", nil, false},
		{"int to datetime", Integer, DateTime, i(1), nil, false},
		{"yes to datetime", YesNo, DateTime, i(1), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ConvertValue(tt.from, tt.to, tt.in)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v (got %#v)", ok, tt.ok, got)
			}
			if !ok {
				if got != nil {
					t.Fatalf("not ok but value %#v", got)
				}
				return
			}
			if f, isF := tt.want.(float64); isF {
				g, isG := got.(float64)
				if !isG || math.Abs(g-f) > 1e-12 {
					t.Fatalf("got %#v, want %#v", got, tt.want)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("got %#v (%T), want %#v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestConvertNegativeZeroNumber(t *testing.T) {
	got, ok := ConvertValue(ShortText, Number, "-0")
	if !ok || math.Signbit(got.(float64)) {
		t.Fatalf("got %#v %v", got, ok)
	}
}

// Every type pair must handle every storage kind without panicking.
func TestConvertValueNoPanic(t *testing.T) {
	values := []any{nil, int64(0), int64(math.MinInt64), int64(math.MaxInt64), 0.0, -1.5, math.NaN(), math.Inf(-1), "", "x", "2026-01-01", []byte("b"), []byte{0xff}}
	for _, from := range FieldTypes {
		for _, to := range FieldTypes {
			for _, v := range values {
				ConvertValue(from, to, v)
			}
		}
	}
}

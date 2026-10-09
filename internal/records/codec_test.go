package records

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
)

func col(t catalog.FieldType, name string) catalog.Column {
	return catalog.Column{Type: t, DisplayName: name}
}

func jn(s string) json.Number { return json.Number(s) }

func TestDecodeValue(t *testing.T) {
	short5 := catalog.Column{Type: catalog.ShortText, DisplayName: "Code", Options: catalog.TypeOptions{MaxLength: 5}}
	type tc struct {
		name string
		c    catalog.Column
		in   any
		want any
		bad  bool
	}
	tests := []tc{
		// nil
		{"nil integer", col(catalog.Integer, "N"), nil, nil, false},
		{"nil yes_no", col(catalog.YesNo, "N"), nil, nil, false},
		{"nil currency", col(catalog.Currency, "N"), nil, nil, false},
		{"nil autonumber", col(catalog.Autonumber, "id"), nil, nil, true},
		{"nil calculated", col(catalog.Calculated, "C"), nil, nil, true},
		{"set calculated", col(catalog.Calculated, "C"), "x", nil, true},
		{"set autonumber", col(catalog.Autonumber, "id"), jn("1"), nil, true},

		// integer
		{"int ok", col(catalog.Integer, "N"), jn("42"), int64(42), false},
		{"int negative", col(catalog.Integer, "N"), jn("-7"), int64(-7), false},
		{"int exp", col(catalog.Integer, "N"), jn("1e3"), int64(1000), false},
		{"int 2^53+1 number", col(catalog.Integer, "N"), jn("9007199254740993"), int64(9007199254740993), false},
		{"int 2^53+1 string", col(catalog.Integer, "N"), "9007199254740993", int64(9007199254740993), false},
		{"int 2^53-1", col(catalog.Integer, "N"), jn("9007199254740991"), int64(9007199254740991), false},
		{"int max", col(catalog.Integer, "N"), jn("9223372036854775807"), int64(math.MaxInt64), false},
		{"int min", col(catalog.Integer, "N"), "-9223372036854775808", int64(math.MinInt64), false},
		{"int max+1", col(catalog.Integer, "N"), jn("9223372036854775808"), nil, true},
		{"int fraction", col(catalog.Integer, "N"), jn("1.5"), nil, true},
		{"int zero fraction", col(catalog.Integer, "N"), jn("2.0"), int64(2), false},
		{"int huge exp", col(catalog.Integer, "N"), jn("1e999999999"), nil, true},
		{"int text", col(catalog.Integer, "N"), "abc", nil, true},
		{"int spaces", col(catalog.Integer, "N"), " 42 ", nil, true},
		{"int bool", col(catalog.Integer, "N"), true, nil, true},
		{"int array", col(catalog.Integer, "N"), []any{}, nil, true},
		{"int object", col(catalog.Integer, "N"), map[string]any{}, nil, true},
		{"int NaN string", col(catalog.Integer, "N"), "NaN", nil, true},
		{"int hex", col(catalog.Integer, "N"), "0x10", nil, true},
		{"lookup ok", col(catalog.Lookup, "Parent"), jn("12"), int64(12), false},
		{"lookup frac", col(catalog.Lookup, "Parent"), jn("12.5"), nil, true},

		// number
		{"num ok", col(catalog.Number, "N"), jn("1.25"), 1.25, false},
		{"num string", col(catalog.Number, "N"), "-3.5e2", -350.0, false},
		{"num int", col(catalog.Number, "N"), jn("3"), 3.0, false},
		{"num neg zero", col(catalog.Number, "N"), jn("-0"), 0.0, false},
		{"num NaN", col(catalog.Number, "N"), "NaN", nil, true},
		{"num Inf", col(catalog.Number, "N"), "Infinity", nil, true},
		{"num inf lower", col(catalog.Number, "N"), "inf", nil, true},
		{"num overflow", col(catalog.Number, "N"), jn("1e999"), nil, true},
		{"num hex float", col(catalog.Number, "N"), "0x1p3", nil, true},
		{"num underscore", col(catalog.Number, "N"), "1_0", nil, true},
		{"num bool", col(catalog.Number, "N"), false, nil, true},
		{"num empty", col(catalog.Number, "N"), "", nil, true},

		// currency
		{"cur ok", col(catalog.Currency, "Price"), jn("12.34"), int64(123400), false},
		{"cur negative small", col(catalog.Currency, "Price"), jn("-0.0001"), int64(-1), false},
		{"cur 4 places", col(catalog.Currency, "Price"), "0.1234", int64(1234), false},
		{"cur 5 places", col(catalog.Currency, "Price"), jn("0.12345"), nil, true},
		{"cur exp", col(catalog.Currency, "Price"), jn("1e3"), int64(10000000), false},
		{"cur trailing zeros", col(catalog.Currency, "Price"), jn("1.230000"), int64(12300), false},
		{"cur max", col(catalog.Currency, "Price"), "922337203685477.5807", int64(math.MaxInt64), false},
		{"cur max+1", col(catalog.Currency, "Price"), "922337203685477.5808", nil, true},
		{"cur min", col(catalog.Currency, "Price"), "-922337203685477.5808", int64(math.MinInt64), false},
		{"cur overflow", col(catalog.Currency, "Price"), jn("1e20"), nil, true},
		{"cur symbol", col(catalog.Currency, "Price"), "$5", nil, true},
		{"cur bool", col(catalog.Currency, "Price"), true, nil, true},

		// yes_no
		{"yn true", col(catalog.YesNo, "Ok"), true, int64(1), false},
		{"yn false", col(catalog.YesNo, "Ok"), false, int64(0), false},
		{"yn 1", col(catalog.YesNo, "Ok"), jn("1"), int64(1), false},
		{"yn 0", col(catalog.YesNo, "Ok"), jn("0"), int64(0), false},
		{"yn 2", col(catalog.YesNo, "Ok"), jn("2"), nil, true},
		{"yn string", col(catalog.YesNo, "Ok"), "true", nil, true},

		// date
		{"date ok", col(catalog.Date, "D"), "2026-03-01", "2026-03-01", false},
		{"date leap day", col(catalog.Date, "D"), "2028-02-29", "2028-02-29", false},
		{"date non-leap", col(catalog.Date, "D"), "2026-02-29", nil, true},
		{"date feb 30", col(catalog.Date, "D"), "2026-02-30", nil, true},
		{"date month 13", col(catalog.Date, "D"), "2026-13-01", nil, true},
		{"date short", col(catalog.Date, "D"), "2026-3-1", nil, true},
		{"date with time", col(catalog.Date, "D"), "2026-03-01T00:00:00Z", nil, true},
		{"date spaces", col(catalog.Date, "D"), " 2026-03-01", nil, true},
		{"date number", col(catalog.Date, "D"), jn("20260301"), nil, true},

		// datetime
		{"dt utc", col(catalog.DateTime, "T"), "2026-03-01T12:30:45Z", "2026-03-01T12:30:45.000Z", false},
		{"dt ms", col(catalog.DateTime, "T"), "2026-03-01T12:30:45.123Z", "2026-03-01T12:30:45.123Z", false},
		{"dt truncates", col(catalog.DateTime, "T"), "2026-03-01T12:30:45.123987654Z", "2026-03-01T12:30:45.123Z", false},
		{"dt offset +", col(catalog.DateTime, "T"), "2026-03-01T12:30:45+05:30", "2026-03-01T07:00:45.000Z", false},
		{"dt offset -", col(catalog.DateTime, "T"), "2026-03-01T23:30:00-02:00", "2026-03-02T01:30:00.000Z", false},
		{"dt leap day", col(catalog.DateTime, "T"), "2028-02-29T00:00:00Z", "2028-02-29T00:00:00.000Z", false},
		{"dt invalid day", col(catalog.DateTime, "T"), "2026-02-30T00:00:00Z", nil, true},
		{"dt no zone", col(catalog.DateTime, "T"), "2026-03-01T12:30:45", nil, true},
		{"dt date only", col(catalog.DateTime, "T"), "2026-03-01", nil, true},
		{"dt year underflow", col(catalog.DateTime, "T"), "0000-01-01T00:00:00+05:00", nil, true},
		{"dt hour 24", col(catalog.DateTime, "T"), "2026-03-01T24:00:00Z", nil, true},
		{"dt number", col(catalog.DateTime, "T"), jn("1"), nil, true},

		// short_text
		{"short ok", short5, "abcde", "abcde", false},
		{"short over", short5, "abcdef", nil, true},
		{"short unicode at limit", short5, "héllo", "héllo", false},
		{"short unicode emoji at limit", short5, "😀😀😀😀😀", "😀😀😀😀😀", false},
		{"short unicode over", short5, "😀😀😀😀😀😀", nil, true},
		{"short default 255", col(catalog.ShortText, "S"), strings.Repeat("é", 255), strings.Repeat("é", 255), false},
		{"short default 256", col(catalog.ShortText, "S"), strings.Repeat("é", 256), nil, true},
		{"short NUL", short5, "a\x00b", nil, true},
		{"short invalid utf8", short5, "a\xffb", nil, true},
		{"short number", short5, jn("1"), nil, true},
		{"short empty", short5, "", "", false},

		// long_text
		{"long ok", col(catalog.LongText, "L"), strings.Repeat("x", 100000), strings.Repeat("x", 100000), false},
		{"long NUL", col(catalog.LongText, "L"), "a\x00", nil, true},
		{"long bool", col(catalog.LongText, "L"), true, nil, true},

		// hyperlink
		{"link http", col(catalog.Hyperlink, "U"), "http://example.com", "http://example.com", false},
		{"link https path", col(catalog.Hyperlink, "U"), "https://example.com/a?b=c#d", "https://example.com/a?b=c#d", false},
		{"link upper scheme", col(catalog.Hyperlink, "U"), "HTTP://example.com", nil, true},
		{"link no host", col(catalog.Hyperlink, "U"), "https://", nil, true},
		{"link no host path", col(catalog.Hyperlink, "U"), "https:///path", nil, true},
		{"link ftp", col(catalog.Hyperlink, "U"), "ftp://example.com", nil, true},
		{"link plain", col(catalog.Hyperlink, "U"), "example.com", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeValue(tt.c, tt.in)
			if tt.bad {
				if err == nil {
					t.Fatalf("want error, got %#v", got)
				}
				var fe *fault.Error
				if !errors.As(err, &fe) || fe.Code != fault.BadRequest {
					t.Fatalf("want BAD_REQUEST fault, got %T %v", err, err)
				}
				if !strings.Contains(fe.Message, `"`+tt.c.DisplayName+`"`) {
					t.Fatalf("message %q does not name the column", fe.Message)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %#v (%T), want %#v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestDecodeValueMessage(t *testing.T) {
	_, err := DecodeValue(col(catalog.Currency, "Price"), "1.23456")
	want := `"Price" must be a number with at most 4 decimal places.`
	var fe *fault.Error
	if !errors.As(err, &fe) || fe.Message != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestEncodeValue(t *testing.T) {
	calc := func(rt string) catalog.Column {
		return catalog.Column{Type: catalog.Calculated, DisplayName: "C", Options: catalog.TypeOptions{ResultType: rt}}
	}
	tests := []struct {
		name string
		c    catalog.Column
		in   any
		want any
	}{
		{"nil", col(catalog.Integer, "N"), nil, nil},
		{"int small", col(catalog.Integer, "N"), int64(5), int64(5)},
		{"int 2^53", col(catalog.Integer, "N"), int64(1 << 53), int64(1 << 53)},
		{"int 2^53+1", col(catalog.Integer, "N"), int64(1<<53 + 1), "9007199254740993"},
		{"int -2^53", col(catalog.Integer, "N"), int64(-(1 << 53)), int64(-(1 << 53))},
		{"int -2^53-1", col(catalog.Integer, "N"), int64(-(1 << 53) - 1), "-9007199254740993"},
		{"int min", col(catalog.Integer, "N"), int64(math.MinInt64), "-9223372036854775808"},
		{"int max", col(catalog.Integer, "N"), int64(math.MaxInt64), "9223372036854775807"},
		{"autonumber big", col(catalog.Autonumber, "id"), int64(1<<53 + 1), "9007199254740993"},
		{"lookup", col(catalog.Lookup, "P"), int64(3), int64(3)},
		{"int from float", col(catalog.Integer, "N"), float64(3), int64(3)},
		{"int unexpected string", col(catalog.Integer, "N"), "abc", "abc"},
		{"number", col(catalog.Number, "N"), 1.5, 1.5},
		{"number from int", col(catalog.Number, "N"), int64(2), 2.0},
		{"number from big int", col(catalog.Number, "N"), int64(1<<53 + 1), "9007199254740993"},
		{"currency", col(catalog.Currency, "P"), int64(123400), "12.3400"},
		{"currency neg", col(catalog.Currency, "P"), int64(-5000), "-0.5000"},
		{"currency tiny neg", col(catalog.Currency, "P"), int64(-1), "-0.0001"},
		{"currency zero", col(catalog.Currency, "P"), int64(0), "0.0000"},
		{"currency min", col(catalog.Currency, "P"), int64(math.MinInt64), "-922337203685477.5808"},
		{"currency max", col(catalog.Currency, "P"), int64(math.MaxInt64), "922337203685477.5807"},
		{"yes_no 0", col(catalog.YesNo, "Y"), int64(0), false},
		{"yes_no 1", col(catalog.YesNo, "Y"), int64(1), true},
		{"yes_no 7", col(catalog.YesNo, "Y"), int64(7), true},
		{"date", col(catalog.Date, "D"), "2026-03-01", "2026-03-01"},
		{"datetime", col(catalog.DateTime, "T"), "2026-03-01T00:00:00.000Z", "2026-03-01T00:00:00.000Z"},
		{"short", col(catalog.ShortText, "S"), "hi", "hi"},
		{"hyperlink", col(catalog.Hyperlink, "S"), "https://a.b", "https://a.b"},
		{"bytes", col(catalog.LongText, "S"), []byte{0, 1, 2, 255}, "AAEC/w=="},
		{"text with int storage", col(catalog.LongText, "S"), int64(5), int64(5)},
		{"text with float storage", col(catalog.LongText, "S"), 2.5, 2.5},
		{"calc default text", calc(""), "abc", "abc"},
		{"calc text number", calc("text"), 12.5, 12.5},
		{"calc integer", calc("integer"), int64(1<<53 + 1), "9007199254740993"},
		{"calc number", calc("number"), 1.5, 1.5},
		{"calc currency", calc("currency"), int64(10000), "1.0000"},
		{"calc currency float storage", calc("currency"), float64(25000), "2.5000"},
		{"calc yes_no", calc("yes_no"), int64(1), true},
		{"calc date", calc("date"), "2026-01-01", "2026-01-01"},
		{"calc datetime", calc("datetime"), "2026-01-01T00:00:00.000Z", "2026-01-01T00:00:00.000Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeValue(tt.c, tt.in)
			if !deepEq(got, tt.want) {
				t.Fatalf("got %#v (%T), want %#v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func deepEq(a, b any) bool {
	return a == b
}

func TestFormatCurrencyRoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, -1, 9999, 10000, -10000, 10001, math.MaxInt64, math.MinInt64, math.MinInt64 + 1} {
		s := FormatCurrency(v)
		got, err := DecodeValue(col(catalog.Currency, "P"), s)
		if err != nil || got != v {
			t.Fatalf("%d -> %q -> %v, %v", v, s, got, err)
		}
	}
}

// roundTrip checks decode -> encode -> JSON -> decode stability.
func roundTrip(t *testing.T, c catalog.Column, in any) {
	t.Helper()
	first, err := DecodeValue(c, in)
	if err != nil {
		return
	}
	enc := EncodeValue(c, first)
	raw, err := json.Marshal(enc)
	if err != nil {
		t.Fatalf("encode %#v of %#v does not marshal: %v", enc, first, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var back any
	if err := dec.Decode(&back); err != nil {
		t.Fatalf("re-decode %s: %v", raw, err)
	}
	second, err := DecodeValue(c, back)
	if err != nil {
		t.Fatalf("type %s in=%#v: first=%#v enc=%s second decode failed: %v", c.Type, in, first, raw, err)
	}
	if first != second {
		t.Fatalf("type %s in=%#v: first=%#v (%T) second=%#v (%T) via %s", c.Type, in, first, first, second, second, raw)
	}
}

func TestRoundTripSamples(t *testing.T) {
	types := []catalog.FieldType{catalog.ShortText, catalog.LongText, catalog.Integer, catalog.Number, catalog.Currency, catalog.YesNo, catalog.Date, catalog.DateTime, catalog.Lookup, catalog.Hyperlink}
	inputs := []any{nil, true, false, jn("0"), jn("1"), jn("-1"), jn("9007199254740993"), jn("9223372036854775807"), jn("-9223372036854775808"),
		jn("1.5"), jn("1e21"), jn("1e-7"), jn("12.3456"), jn("-0.0001"), jn("5e-324"), jn("1.7976931348623157e308"),
		"9007199254740993", "2026-03-01", "2026-03-01T12:00:00+02:00", "https://x.y/z", "héllo", ""}
	for _, ty := range types {
		for _, in := range inputs {
			roundTrip(t, col(ty, "F"), in)
		}
	}
}

func FuzzDecodeValue(f *testing.F) {
	seeds := []struct {
		typ  uint8
		kind uint8
		s    string
	}{
		{0, 1, "42"}, {1, 1, "1.5"}, {2, 1, "12.3400"}, {2, 0, "-0.0001"}, {3, 2, ""}, {4, 0, "2026-02-28"},
		{5, 0, "2026-03-01T12:30:45.123456+05:30"}, {6, 0, "héllo"}, {7, 0, "text"}, {8, 0, "https://example.com/x"},
		{0, 0, "9007199254740993"}, {0, 1, "9223372036854775808"}, {1, 1, "1e999"}, {1, 0, "NaN"}, {2, 1, "922337203685477.5807"},
		{9, 3, ""}, {10, 0, "x"}, {0, 1, "1e3"}, {1, 1, "-0"}, {5, 0, "0000-01-01T00:00:00+05:00"}, {4, 0, "2028-02-29"},
		{2, 1, "1e-40"}, {0, 1, "1e40"},
	}
	for _, s := range seeds {
		f.Add(s.typ, s.kind, s.s, uint8(5))
	}
	types := []catalog.FieldType{catalog.Integer, catalog.Number, catalog.Currency, catalog.YesNo, catalog.Date, catalog.DateTime,
		catalog.ShortText, catalog.LongText, catalog.Hyperlink, catalog.Lookup, catalog.Autonumber, catalog.Calculated}
	f.Fuzz(func(t *testing.T, typ, kind uint8, s string, maxLen uint8) {
		c := catalog.Column{Type: types[int(typ)%len(types)], DisplayName: "F", Options: catalog.TypeOptions{MaxLength: int(maxLen)}}
		var in any
		switch kind % 4 {
		case 0:
			in = s
		case 1:
			in = json.Number(s)
		case 2:
			in = s != ""
		case 3:
			in = nil
		}
		_, err := DecodeValue(c, in)
		if c.Type == catalog.Autonumber || c.Type == catalog.Calculated {
			if err == nil {
				t.Fatalf("%s accepted a value", c.Type)
			}
			return
		}
		roundTrip(t, c, in)
	})
}

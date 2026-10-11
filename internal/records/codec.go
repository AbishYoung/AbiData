package records

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"abidata/internal/catalog"
	"abidata/internal/fault"
)

const (
	maxSafeInt     = 1 << 53 // integers beyond ±2^53 are not exactly representable as JSON doubles
	maxNumberLen   = 400
	maxDecimalExp  = 40
	dateLayout     = "2006-01-02"
	datetimeLayout = "2006-01-02T15:04:05.000Z"
)

func badf(c catalog.Column, what string) error {
	return fault.Newf(fault.BadRequest, "%q %s", c.DisplayName, what)
}

// DecodeValue converts a JSON value (decoded with UseNumber) into the storage value
// for column c: nil, int64, float64 or string. nil is returned as nil for every type;
// NOT NULL enforcement is left to the database.
func DecodeValue(c catalog.Column, v any) (any, error) {
	switch c.Type {
	case catalog.Calculated:
		return nil, badf(c, "is calculated by the database and cannot be set.")
	case catalog.Autonumber:
		return nil, badf(c, "is assigned automatically and cannot be set.")
	}
	if v == nil {
		return nil, nil
	}
	switch c.Type {
	case catalog.Integer, catalog.Lookup:
		n, ok := intOf(v)
		if !ok {
			return nil, badf(c, "must be a whole number.")
		}
		return n, nil
	case catalog.Number:
		f, ok := floatOf(v)
		if !ok {
			return nil, badf(c, "must be a finite number.")
		}
		return f, nil
	case catalog.Currency:
		n, ok := currencyOf(v)
		if !ok {
			return nil, badf(c, "must be a number with at most 4 decimal places.")
		}
		return n, nil
	case catalog.YesNo:
		switch x := v.(type) {
		case bool:
			if x {
				return int64(1), nil
			}
			return int64(0), nil
		case json.Number:
			if n, ok := intOf(x); ok && (n == 0 || n == 1) {
				return n, nil
			}
		}
		return nil, badf(c, "must be true or false.")
	case catalog.Date:
		s, ok := v.(string)
		if ok && validText(s) {
			if t, err := time.Parse(dateLayout, s); err == nil && t.Format(dateLayout) == s {
				return s, nil
			}
		}
		return nil, badf(c, "must be a valid date in YYYY-MM-DD format.")
	case catalog.DateTime:
		s, ok := v.(string)
		if ok && validText(s) {
			if out, ok := NormalizeDateTime(s); ok {
				return out, nil
			}
		}
		return nil, badf(c, "must be a valid date and time in ISO 8601 format (for example 2026-03-01T12:30:00Z).")
	case catalog.ShortText:
		s, ok := v.(string)
		if !ok || !validText(s) {
			return nil, badf(c, "must be text.")
		}
		if utf8.RuneCountInString(s) > c.MaxLen() {
			return nil, fault.Newf(fault.BadRequest, "%q is too long: at most %d characters are allowed.", c.DisplayName, c.MaxLen())
		}
		return s, nil
	case catalog.LongText:
		s, ok := v.(string)
		if !ok || !validText(s) {
			return nil, badf(c, "must be text.")
		}
		return s, nil
	case catalog.Hyperlink:
		s, ok := v.(string)
		if !ok || !validText(s) || !isHTTPURL(s) {
			return nil, badf(c, "must be a web address starting with http:// or https://.")
		}
		return s, nil
	case catalog.GeoPoint, catalog.GeoArea:
		return decodeGeo(c, v)
	}
	return nil, fault.Newf(fault.BadRequest, "%q has an unknown field type %q.", c.DisplayName, string(c.Type))
}

// NormalizeDateTime parses an RFC 3339 timestamp and returns it as UTC with
// millisecond precision ("2006-01-02T15:04:05.000Z"), truncating finer digits.
func NormalizeDateTime(s string) (string, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", false
	}
	t = t.UTC().Truncate(time.Millisecond)
	if y := t.Year(); y < 0 || y > 9999 {
		return "", false
	}
	return t.Format(datetimeLayout), true
}

func validText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func isHTTPURL(s string) bool {
	var rest string
	switch {
	case strings.HasPrefix(s, "http://"):
		rest = s[len("http://"):]
	case strings.HasPrefix(s, "https://"):
		rest = s[len("https://"):]
	default:
		return false
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest != ""
}

// validNumberSyntax reports whether s follows the JSON number grammar.
func validNumberSyntax(s string) bool {
	if len(s) == 0 || len(s) > maxNumberLen {
		return false
	}
	i := 0
	if s[i] == '-' {
		i++
	}
	digits := func() int {
		n := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			n++
		}
		return n
	}
	if i < len(s) && s[i] == '0' {
		i++
	} else if digits() == 0 {
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		if digits() == 0 {
			return false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == len(s)
}

func numText(v any) (string, bool) {
	switch x := v.(type) {
	case json.Number:
		return string(x), true
	case string:
		return x, true
	}
	return "", false
}

func parseDecimal(s string) (decimal.Decimal, bool) {
	if !validNumberSyntax(s) {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, false
	}
	if e := d.Exponent(); e > maxDecimalExp || e < -maxDecimalExp {
		return decimal.Decimal{}, false
	}
	return d, true
}

func intOf(v any) (int64, bool) {
	s, ok := numText(v)
	if !ok {
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	d, ok := parseDecimal(s)
	if !ok || !d.IsInteger() {
		return 0, false
	}
	b := d.BigInt()
	if !b.IsInt64() {
		return 0, false
	}
	return b.Int64(), true
}

func floatOf(v any) (float64, bool) {
	s, ok := numText(v)
	if !ok || !validNumberSyntax(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if f == 0 {
		f = 0 // normalize -0
	}
	return f, true
}

func currencyOf(v any) (int64, bool) {
	s, ok := numText(v)
	if !ok {
		return 0, false
	}
	d, ok := parseDecimal(s)
	if !ok {
		return 0, false
	}
	scaled := d.Mul(decimal.NewFromInt(catalog.CurrencyScale))
	if !scaled.IsInteger() {
		return 0, false
	}
	b := scaled.BigInt()
	if !b.IsInt64() {
		return 0, false
	}
	return b.Int64(), true
}

// EncodeValue converts a storage value (nil, int64, float64, string, []byte) to the
// API JSON value for column c. Calculated columns use their result type. Unexpected
// storage types are passed through rather than rejected.
func EncodeValue(c catalog.Column, v any) any {
	if v == nil {
		return nil
	}
	if b, ok := v.([]byte); ok {
		return base64.StdEncoding.EncodeToString(b)
	}
	t := c.Type
	if t == catalog.Calculated {
		t = ResultFieldType(c.Options.ResultType)
	}
	switch t {
	case catalog.Currency:
		switch x := v.(type) {
		case int64:
			return FormatCurrency(x)
		case float64:
			if n, ok := integralInt64(x); ok {
				return FormatCurrency(n)
			}
		}
		return passthrough(v)
	case catalog.YesNo:
		switch x := v.(type) {
		case int64:
			return x != 0
		case float64:
			return x != 0
		}
		return v
	case catalog.Integer, catalog.Lookup, catalog.Autonumber:
		if f, ok := v.(float64); ok {
			if n, ok := integralInt64(f); ok {
				return passthrough(n)
			}
		}
		return passthrough(v)
	case catalog.Number:
		if n, ok := v.(int64); ok {
			if n > maxSafeInt || n < -maxSafeInt {
				return strconv.FormatInt(n, 10)
			}
			return float64(n)
		}
		return passthrough(v)
	case catalog.GeoPoint, catalog.GeoArea:
		return encodeGeo(v)
	}
	return passthrough(v)
}

// passthrough returns v, turning integers beyond ±2^53 into decimal strings.
func passthrough(v any) any {
	if n, ok := v.(int64); ok && (n > maxSafeInt || n < -maxSafeInt) {
		return strconv.FormatInt(n, 10)
	}
	return v
}

func integralInt64(f float64) (int64, bool) {
	if f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
		return 0, false
	}
	return int64(f), true
}

// ResultFieldType maps a calculated column's result_type to the field type whose
// representation it shares (default text).
func ResultFieldType(rt string) catalog.FieldType {
	switch rt {
	case "integer":
		return catalog.Integer
	case "number":
		return catalog.Number
	case "currency":
		return catalog.Currency
	case "yes_no":
		return catalog.YesNo
	case "date":
		return catalog.Date
	case "datetime":
		return catalog.DateTime
	case "geo_point":
		return catalog.GeoPoint
	case "geo_area":
		return catalog.GeoArea
	}
	return catalog.LongText
}

// FormatCurrency renders a ×10⁴ scaled integer as a decimal string with exactly 4 places.
func FormatCurrency(v int64) string {
	u := uint64(v) //nolint:gosec // deliberate two's-complement reinterpretation; negated below for v < 0
	sign := ""
	if v < 0 {
		u = -u // two's complement negation is exact for math.MinInt64
		sign = "-"
	}
	frac := strconv.FormatUint(u%catalog.CurrencyScale, 10)
	return sign + strconv.FormatUint(u/catalog.CurrencyScale, 10) + "." + strings.Repeat("0", 4-len(frac)) + frac
}

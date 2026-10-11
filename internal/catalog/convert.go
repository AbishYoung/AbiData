package catalog

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// ConvertValue converts a storage value of a `from` column into the storage value of a
// `to` column when a column's type is altered. v is nil, int64, float64, string or
// []byte. ok=false means the value cannot be converted without loss. Converting into
// autonumber or calculated is never possible, even for NULL.
func ConvertValue(from, to FieldType, v any) (out any, ok bool) {
	if from == to {
		return v, true
	}
	if to == Autonumber || to == Calculated {
		return nil, false
	}
	if v == nil {
		if to == YesNo {
			return int64(0), true
		}
		return nil, true
	}
	if b, isBytes := v.([]byte); isBytes {
		if !utf8.Valid(b) {
			return nil, false
		}
		v = string(b)
	}
	// Blank text holds no value, so it converts to "empty" in any non-text type without loss.
	if s, isText := v.(string); isText && strings.TrimSpace(s) == "" && isTextual(from) {
		switch to {
		case Integer, Lookup, Number, Currency, Date, DateTime, Hyperlink:
			return nil, true
		case YesNo:
			return int64(0), true
		}
	}
	switch to {
	case ShortText, LongText, Hyperlink:
		s, ok := textForm(from, v)
		if !ok || strings.ContainsRune(s, 0) {
			return nil, false
		}
		if to == Hyperlink && !isHTTPURL(s) {
			return nil, false
		}
		return s, true
	case Integer, Lookup:
		return toInteger(from, v)
	case Number:
		return toNumber(from, v)
	case Currency:
		return toCurrency(from, v)
	case YesNo:
		return toYesNo(from, v)
	case Date:
		return toDate(v)
	case DateTime:
		return toDateTime(v)
	}
	return nil, false
}

func isTextual(t FieldType) bool { return t == ShortText || t == LongText || t == Hyperlink }

func isTemporal(t FieldType) bool { return t == Date || t == DateTime }

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

func textForm(from FieldType, v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int64:
		switch from {
		case Currency:
			return decimal.New(x, -4).String(), true
		case YesNo:
			if x != 0 {
				return "Yes", true
			}
			return "No", true
		}
		return strconv.FormatInt(x, 10), true
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", false
		}
		return strconv.FormatFloat(x, 'g', -1, 64), true
	}
	return "", false
}

// numberSyntax reports whether s follows the JSON number grammar (bounded length).
func numberSyntax(s string) bool {
	if len(s) == 0 || len(s) > 400 {
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

func parseDec(s string) (decimal.Decimal, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	if !numberSyntax(s) {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, false
	}
	if e := d.Exponent(); e > 40 || e < -40 {
		return decimal.Decimal{}, false
	}
	return d, true
}

func floatIntegral(f float64) (int64, bool) {
	if f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
		return 0, false
	}
	return int64(f), true
}

func intOrNil(n int64, ok bool) (any, bool) {
	if !ok {
		return nil, false
	}
	return n, true
}

func toInteger(from FieldType, v any) (any, bool) {
	switch x := v.(type) {
	case int64:
		if from == Currency {
			if x%CurrencyScale != 0 {
				return nil, false
			}
			return x / CurrencyScale, true
		}
		return x, true
	case float64:
		return intOrNil(floatIntegral(x))
	case string:
		if isTemporal(from) {
			return nil, false
		}
		s := strings.TrimSpace(x)
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
		d, ok := parseDec(s)
		if !ok || !d.IsInteger() {
			return nil, false
		}
		b := d.BigInt()
		if !b.IsInt64() {
			return nil, false
		}
		return b.Int64(), true
	}
	return nil, false
}

func toNumber(from FieldType, v any) (any, bool) {
	switch x := v.(type) {
	case int64:
		if from == Currency {
			return decimal.New(x, -4).InexactFloat64(), true
		}
		return float64(x), true
	case float64:
		return x, true
	case string:
		if isTemporal(from) {
			return nil, false
		}
		s := strings.TrimPrefix(strings.TrimSpace(x), "+")
		if !numberSyntax(s) {
			return nil, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		if f == 0 {
			f = 0 // normalize -0
		}
		return f, true
	}
	return nil, false
}

func scaleDecimal(d decimal.Decimal) (any, bool) {
	scaled := d.Mul(decimal.NewFromInt(CurrencyScale))
	if !scaled.IsInteger() {
		return nil, false
	}
	b := scaled.BigInt()
	if !b.IsInt64() {
		return nil, false
	}
	return b.Int64(), true
}

func toCurrency(from FieldType, v any) (any, bool) {
	switch x := v.(type) {
	case int64:
		if from == YesNo || x > math.MaxInt64/CurrencyScale || x < math.MinInt64/CurrencyScale {
			return nil, false
		}
		return x * CurrencyScale, true
	case float64:
		if from == YesNo || math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, false
		}
		s := x * CurrencyScale
		r := math.Round(s) // half away from zero
		if math.Abs(s-r) >= 1e-9 {
			return nil, false
		}
		return intOrNil(floatIntegral(r))
	case string:
		if isTemporal(from) {
			return nil, false
		}
		s, ok := cleanCurrencyText(x)
		if !ok {
			return nil, false
		}
		d, ok := parseDec(s)
		if !ok {
			return nil, false
		}
		return scaleDecimal(d)
	}
	return nil, false
}

// cleanCurrencyText strips a leading currency symbol and valid thousands separators.
func cleanCurrencyText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	sign := ""
	if s != "" && (s[0] == '-' || s[0] == '+') {
		if s[0] == '-' {
			sign = "-"
		}
		s = strings.TrimSpace(s[1:])
	}
	for _, sym := range []string{"$", "€", "£"} {
		if strings.HasPrefix(s, sym) {
			s = strings.TrimSpace(s[len(sym):])
			break
		}
	}
	if strings.Contains(s, ",") {
		intPart, frac, hasFrac := strings.Cut(s, ".")
		if strings.Contains(frac, ",") {
			return "", false
		}
		groups := strings.Split(intPart, ",")
		for i, g := range groups {
			if i == 0 && (len(g) < 1 || len(g) > 3) || i > 0 && len(g) != 3 {
				return "", false
			}
		}
		s = strings.Join(groups, "")
		if hasFrac {
			s += "." + frac
		}
	}
	return sign + s, true
}

func toYesNo(from FieldType, v any) (any, bool) {
	switch x := v.(type) {
	case int64:
		if from == Currency {
			switch x {
			case 0:
				return int64(0), true
			case CurrencyScale:
				return int64(1), true
			}
			return nil, false
		}
		if x == 0 || x == 1 {
			return x, true
		}
	case float64:
		if x == 0 {
			return int64(0), true
		}
		if x == 1 {
			return int64(1), true
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "y", "1", "on":
			return int64(1), true
		case "false", "no", "n", "0", "off":
			return int64(0), true
		}
	}
	return nil, false
}

func parseDateStrict(s string) bool {
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}

func normalizeDateTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	t = t.UTC().Truncate(time.Millisecond)
	if y := t.Year(); y < 0 || y > 9999 {
		return time.Time{}, false
	}
	return t, true
}

func toDate(v any) (any, bool) {
	s, ok := v.(string)
	if !ok {
		return nil, false
	}
	s = strings.TrimSpace(s)
	if parseDateStrict(s) {
		return s, true
	}
	t, ok := normalizeDateTime(s)
	if !ok || t.Hour() != 0 || t.Minute() != 0 || t.Second() != 0 || t.Nanosecond() != 0 {
		return nil, false
	}
	return t.Format("2006-01-02"), true
}

func toDateTime(v any) (any, bool) {
	s, ok := v.(string)
	if !ok {
		return nil, false
	}
	s = strings.TrimSpace(s)
	if parseDateStrict(s) {
		return s + "T00:00:00.000Z", true
	}
	t, ok := normalizeDateTime(s)
	if !ok {
		return nil, false
	}
	return t.Format("2006-01-02T15:04:05.000Z"), true
}

package query

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"abidata/internal/fault"
)

var (
	wholeNumberRe = regexp.MustCompile(`^-?\d+$`)
	amountRe      = regexp.MustCompile(`^-?\d+(\.\d{1,4})?$`)
)

// CheckParams validates supplied parameter values against the types a query declares, so a
// malformed value is an error naming the parameter instead of a comparison that silently matches
// the wrong rows (text compared with a date, "abc" treated as 0). Empty (null) values are allowed:
// they bind as NULL. Parameters that are not declared are left alone.
func CheckParams(declared []Param, params Params) error {
	for _, d := range declared {
		v, ok := params[d.Name]
		if !ok || v == nil {
			continue
		}
		if msg := paramProblem(d.Type, v); msg != "" {
			return fault.Newf(fault.BadRequest, "Parameter %s must be %s.", d.Name, msg).
				WithDetails(map[string]any{"invalid_parameters": []string{d.Name}})
		}
	}
	return nil
}

// paramProblem describes what is wrong with v for a parameter of type typ ("" when it is fine).
func paramProblem(typ string, v any) string {
	text := func() (string, bool) {
		switch x := v.(type) {
		case string:
			return strings.TrimSpace(x), true
		case json.Number:
			return x.String(), true
		}
		return "", false
	}
	switch typ {
	case "integer":
		switch x := v.(type) {
		case int64:
			return ""
		case float64:
			if x == math.Trunc(x) && !math.IsInf(x, 0) {
				return ""
			}
		default:
			if s, ok := text(); ok {
				if wholeNumberRe.MatchString(s) {
					if _, err := strconv.ParseInt(s, 10, 64); err == nil {
						return ""
					}
				} else if f, err := strconv.ParseFloat(s, 64); err == nil && f == math.Trunc(f) && !math.IsInf(f, 0) && !strings.ContainsAny(s, "xXpP_") {
					return "" // 1e3
				}
			}
		}
		return "a whole number"
	case "number":
		switch v.(type) {
		case int64, float64:
			return ""
		default:
			if s, ok := text(); ok {
				if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && !strings.ContainsAny(s, "xXpP_") {
					return ""
				}
			}
		}
		return "a number"
	case "currency":
		switch v.(type) {
		case int64:
			return ""
		case float64:
			return "" // e.g. 12.5 sent as a JSON number
		default:
			if s, ok := text(); ok && amountRe.MatchString(s) {
				return ""
			}
		}
		return "an amount such as 12.50"
	case "yes_no":
		switch x := v.(type) {
		case bool:
			return ""
		case int64:
			if x == 0 || x == 1 {
				return ""
			}
		case float64:
			if x == 0 || x == 1 {
				return ""
			}
		default:
			if s, ok := text(); ok {
				switch strings.ToLower(s) {
				case "true", "false", "yes", "no", "1", "0":
					return ""
				}
			}
		}
		return "yes or no"
	case "date":
		if s, ok := v.(string); ok {
			if t, err := time.Parse("2006-01-02", s); err == nil && t.Format("2006-01-02") == s {
				return ""
			}
		}
		return "a date such as 2026-06-01"
	case "datetime":
		if s, ok := v.(string); ok {
			if _, err := time.Parse(time.RFC3339, s); err == nil {
				return ""
			}
		}
		return "a date and time such as 2026-06-01T09:30:00Z"
	}
	return "" // text and unknown types accept any scalar
}

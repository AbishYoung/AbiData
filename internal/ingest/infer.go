package ingest

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"abidata/internal/catalog"
)

// boolWords maps text to yes/no values (case-insensitive).
var boolWords = map[string]bool{
	"true": true, "false": false, "yes": true, "no": false, "y": true, "n": false, "t": true, "f": false,
	"on": true, "off": false, "1": true, "0": false,
}

// parseBool reads a yes/no word.
func parseBool(s string) (value, ok bool) {
	v, ok := boolWords[strings.ToLower(strings.TrimSpace(s))]
	return v, ok
}

func isIntText(s string) bool {
	t := strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	if t == "" || len(t) > 19 {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] < '0' || t[i] > '9' {
			return false
		}
	}
	// Leading zeros are identifiers (zip codes, SKUs), not numbers.
	if len(t) > 1 && t[0] == '0' {
		return false
	}
	_, err := strconv.ParseInt(strings.TrimPrefix(s, "+"), 10, 64)
	return err == nil
}

func isNumberText(s string) bool {
	if strings.ContainsAny(s, "xXpPnNiI_ ") { // hex floats, NaN, Inf, digit separators
		return false
	}
	t := strings.TrimPrefix(s, "+")
	if len(t) > 1 && t[0] == '0' && t[1] != '.' && t[1] != 'e' && t[1] != 'E' {
		return false
	}
	f, err := strconv.ParseFloat(t, 64)
	return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}

func isDateText(s string) bool {
	if len(s) != 10 {
		return false
	}
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}

func isDateTimeText(s string) bool {
	_, ok := normalizeDateTime(s)
	return ok
}

// normalizeDateTime turns the common timestamp spellings into RFC 3339. Values
// without a zone are taken as UTC; a bare date is midnight UTC.
func normalizeDateTime(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if isDateText(s) {
		return s + "T00:00:00Z", true
	}
	if len(s) > 11 && s[10] == ' ' {
		s = s[:10] + "T" + s[11:]
	}
	layouts := []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			if l == time.RFC3339Nano {
				return s, true
			}
			return t.UTC().Format(time.RFC3339), true
		}
	}
	return "", false
}

// inferType picks the AbiData type that fits every non-empty value. Currency is
// never inferred (a builder maps it explicitly). Empty input is short_text.
func inferType(vals []string) (catalog.FieldType, int) {
	maxLen := 0
	allInt, allNum, allBool, allDate, allDT := true, true, true, true, true
	sawBoolWord := false
	n := 0
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		n++
		if l := utf8.RuneCountInString(v); l > maxLen {
			maxLen = l
		}
		if allInt && !isIntText(v) {
			allInt = false
		}
		if allNum && !allInt && !isNumberText(v) {
			allNum = false
		}
		if allBool {
			b := strings.ToLower(v)
			if _, ok := boolWords[b]; !ok {
				allBool = false
			} else if b != "1" && b != "0" {
				sawBoolWord = true
			}
		}
		if allDate && !isDateText(v) {
			allDate = false
		}
		if allDT && !isDateTimeText(v) {
			allDT = false
		}
	}
	switch {
	case n == 0:
		return catalog.ShortText, 0
	case allInt:
		return catalog.Integer, maxLen
	case allNum:
		return catalog.Number, maxLen
	case allBool && sawBoolWord:
		return catalog.YesNo, maxLen
	case allDate:
		return catalog.Date, maxLen
	case allDT:
		return catalog.DateTime, maxLen
	case maxLen > catalog.DefaultMaxLength:
		return catalog.LongText, maxLen
	}
	return catalog.ShortText, maxLen
}

// inferColumns builds the SourceColumns from collected samples.
func inferColumns(names, originals []string, samples [][]string) []SourceColumn {
	out := make([]SourceColumn, len(names))
	for i := range names {
		t, ml := inferType(samples[i])
		sc := SourceColumn{Index: i, Name: names[i], Original: originals[i], Type: t, MaxLen: ml, Samples: []string{}}
		for _, v := range samples[i] {
			if strings.TrimSpace(v) == "" {
				continue
			}
			sc.NonEmpty++
			if len(sc.Samples) < 5 {
				s := v
				if utf8.RuneCountInString(s) > 60 {
					s = string([]rune(s)[:60]) + "…"
				}
				sc.Samples = append(sc.Samples, s)
			}
		}
		out[i] = sc
	}
	return out
}

// cleanNames sanitises header cells into unique, single-line field names.
func cleanNames(header []string) []string {
	out := make([]string, len(header))
	seen := map[string]bool{}
	for i, h := range header {
		name := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if r == 0 || r == utf8.RuneError {
				return -1
			}
			return r
		}, h)), " ")
		if r := []rune(name); len(r) > 100 {
			name = string(r[:100])
		}
		if name == "" {
			name = "Column " + strconv.Itoa(i+1)
		}
		base, n := name, 1
		for seen[strings.ToLower(name)] {
			n++
			name = base + " " + strconv.Itoa(n)
		}
		seen[strings.ToLower(name)] = true
		out[i] = name
	}
	return out
}

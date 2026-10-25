package reports

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"abidata/internal/catalog"
)

// Formatting limits (ADR 0018).
const (
	maxMask       = 100 // longest number or date pattern
	maxAffix      = 32  // longest prefix, suffix or currency symbol
	maxDecimals   = 6
	defaultSymbol = "$"
)

// CurrencyOptions customises how a currency value prints (ADR 0018 §1).
type CurrencyOptions struct {
	Symbol    string `json:"symbol,omitempty" maxLength:"32" doc:"Currency symbol such as $, €, £, ¥ or CHF (default $)."`
	Placement string `json:"placement,omitempty" enum:"prefix,suffix," doc:"Where the symbol goes (default prefix)."`
	Spaced    bool   `json:"spaced,omitempty" doc:"Put a space between the symbol and the number."`
	Negative  string `json:"negative,omitempty" enum:"minus,paren,sign_after," doc:"How negative amounts print: -$1.00, ($1.00) or $1.00- (default minus)."`
	Decimals  *int   `json:"decimals,omitempty" minimum:"0" maximum:"6" doc:"Decimal places (default 2)."`
}

func validateCurrency(where string, o *CurrencyOptions) error {
	if o == nil {
		return nil
	}
	if utf8.RuneCountInString(o.Symbol) > maxAffix || strings.ContainsAny(o.Symbol, "\r\n\x00") {
		return bad("%s: the currency symbol is too long.", where)
	}
	switch o.Placement {
	case "", "prefix", "suffix":
	default:
		return bad("%s: currency placement must be prefix or suffix.", where)
	}
	switch o.Negative {
	case "", "minus", "paren", "sign_after":
	default:
		return bad("%s: negative style must be minus, paren or sign_after.", where)
	}
	if o.Decimals != nil && (*o.Decimals < 0 || *o.Decimals > maxDecimals) {
		return bad("%s: currency decimals must be between 0 and %d.", where, maxDecimals)
	}
	return nil
}

// validateFormat checks the pattern kinds of a control's format spec. Other
// kinds are accepted as-is for compatibility with older reports.
func validateFormat(where, spec string) error {
	kind, arg, _ := strings.Cut(spec, ":")
	switch kind {
	case "pattern":
		if len(arg) > maxMask || arg == "" {
			return bad("%s: enter a number pattern of at most %d characters.", where, maxMask)
		}
		if _, err := parseMask(arg); err != nil {
			return bad("%s: %s", where, err.Error())
		}
	case "datepattern":
		if len(arg) > maxMask || arg == "" {
			return bad("%s: enter a date pattern of at most %d characters.", where, maxMask)
		}
	}
	return nil
}

func validateAffixes(where string, c *ReportControl) error {
	if utf8.RuneCountInString(c.Prefix) > maxAffix || utf8.RuneCountInString(c.Suffix) > maxAffix {
		return bad("%s: prefix and suffix can be at most %d characters.", where, maxAffix)
	}
	return nil
}

// formatControl renders a control's value: format spec, currency options, prefix and suffix.
func formatControl(c ReportControl, v any, f *field, tz *time.Location) any {
	out := format(v, c.Format, f, tz)
	if v != nil && c.Currency != nil && isCurrencyKind(c.Format, f) {
		if d, ok := asDecimal(v); ok {
			out = c.Currency.render(d)
		}
	}
	if s, ok := out.(string); ok && s != "" && (c.Prefix != "" || c.Suffix != "") {
		return c.Prefix + s + c.Suffix
	}
	return out
}

func isCurrencyKind(spec string, f *field) bool {
	kind, _, _ := strings.Cut(spec, ":")
	if kind == "currency" {
		return true
	}
	return kind == "" && f != nil && f.col != nil && f.col.Type == catalog.Currency
}

func (o *CurrencyOptions) render(d decimal.Decimal) string {
	places := int32(2)
	if o.Decimals != nil {
		places = int32(min(max(*o.Decimals, 0), maxDecimals))
	}
	sym := o.Symbol
	if sym == "" {
		sym = defaultSymbol
	}
	neg := d.IsNegative()
	num := thousands(d.Abs().StringFixed(places))
	if neg && strings.Trim(num, "0.,") == "" {
		neg = false // rounds to zero: no sign
	}
	sp := ""
	if o.Spaced {
		sp = " "
	}
	var body string
	if o.Placement == "suffix" {
		body = num + sp + sym
	} else {
		body = sym + sp + num
	}
	if !neg {
		return body
	}
	switch o.Negative {
	case "paren":
		return "(" + body + ")"
	case "sign_after":
		return body + "-"
	}
	return "-" + body
}

// ---- number pattern masks ("#,##0.00;(#,##0.00);\"-\"") ---------------------------------------

type maskSection struct {
	pre, post string
	minInt    int   // count of 0 before the point
	frac      int32 // digits after the point
	group     bool  // thousands separators
	percent   bool
	hasNum    bool
}

type mask struct {
	pos, neg, zero *maskSection
}

type errMask string

func (e errMask) Error() string { return string(e) }

// splitSections splits on ';' outside quotes.
func splitSections(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\\' && i+1 < len(s):
			cur.WriteByte(ch)
			i++
			cur.WriteByte(s[i])
			continue
		case ch == '"':
			inQ = !inQ
		case ch == ';' && !inQ:
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	return append(out, cur.String())
}

func parseSection(s string) (*maskSection, error) {
	sec := &maskSection{}
	var pre, post strings.Builder
	state := 0 // 0 before digits, 1 in digits, 2 after
	seenPoint := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		isNum := ch == '0' || ch == '#' || ch == ',' || (ch == '.' && !seenPoint)
		if state == 0 && isNum {
			state = 1
		} else if state == 1 && !isNum {
			state = 2
		}
		if state == 1 {
			sec.hasNum = true
			switch ch {
			case '0':
				if seenPoint {
					sec.frac++
				} else {
					sec.minInt++
				}
			case '#':
				if seenPoint {
					sec.frac++
				}
			case ',':
				if !seenPoint {
					sec.group = true
				}
			case '.':
				seenPoint = true
			}
			continue
		}
		b := &pre
		if state == 2 {
			b = &post
		}
		switch {
		case ch == '%':
			sec.percent = true
			b.WriteByte(ch)
		case ch == '"':
			j := strings.IndexByte(s[i+1:], '"')
			if j < 0 {
				b.WriteString(s[i+1:])
				i = len(s)
			} else {
				b.WriteString(s[i+1 : i+1+j])
				i += j + 1
			}
		case ch == '\\' && i+1 < len(s):
			i++
			b.WriteByte(s[i])
		default:
			b.WriteByte(ch)
		}
	}
	if sec.frac > 10 {
		return nil, errMask("A number pattern can show at most 10 decimal places.")
	}
	sec.pre, sec.post = pre.String(), post.String()
	return sec, nil
}

func parseMask(s string) (*mask, error) {
	parts := splitSections(s)
	if len(parts) > 3 {
		return nil, errMask("A number pattern has at most three sections: positive;negative;zero.")
	}
	m := &mask{}
	for i, p := range parts {
		sec, err := parseSection(p)
		if err != nil {
			return nil, err
		}
		if !sec.hasNum && i == 0 {
			return nil, errMask("A number pattern needs digit placeholders such as #,##0.00.")
		}
		switch i {
		case 0:
			m.pos = sec
		case 1:
			m.neg = sec
		case 2:
			m.zero = sec
		}
	}
	return m, nil
}

func (sec *maskSection) render(d decimal.Decimal) string {
	if !sec.hasNum {
		return sec.pre + sec.post
	}
	if sec.percent {
		d = d.Mul(decimal.NewFromInt(100))
	}
	s := d.Abs().StringFixed(sec.frac)
	ip, fp, _ := strings.Cut(s, ".")
	if sec.minInt == 0 && strings.Trim(ip, "0") == "" && sec.frac > 0 {
		ip = ""
	} else if len(ip) < sec.minInt {
		ip = strings.Repeat("0", sec.minInt-len(ip)) + ip
	}
	if sec.group {
		ip = thousands(ip)
	}
	if fp != "" {
		ip += "." + fp
	}
	return sec.pre + ip + sec.post
}

// applyMask formats d with a pattern mask. A negative value without a negative
// section keeps a leading minus sign.
func applyMask(mk *mask, d decimal.Decimal) string {
	switch {
	case d.IsZero() && mk.zero != nil:
		return mk.zero.render(d)
	case d.IsNegative() && mk.neg != nil:
		return mk.neg.render(d)
	}
	s := mk.pos.render(d)
	if d.IsNegative() && strings.Trim(mk.pos.render(d.Abs()), "0.,") != "" {
		return "-" + s
	}
	return s
}

// ---- date patterns ("YYYY-MM-DD", "MMM D, YYYY") --------------------------------------------

var dateTokens = []string{"YYYY", "MMMM", "dddd", "MMM", "ddd", "YY", "MM", "DD", "HH", "hh", "mm", "ss", "M", "D", "H", "h", "A", "a"}

var dateLayout = map[string]string{
	"YYYY": "2006", "YY": "06", "MMMM": "January", "MMM": "Jan", "MM": "01", "M": "1",
	"dddd": "Monday", "ddd": "Mon", "DD": "02", "D": "2", "HH": "15", "hh": "03", "h": "3",
	"mm": "04", "ss": "05", "A": "PM", "a": "pm",
}

// formatDatePattern renders t with Moment-style tokens; text in [brackets] is literal.
func formatDatePattern(t time.Time, pat string) string {
	var b strings.Builder
	for i := 0; i < len(pat); {
		if pat[i] == '[' {
			if j := strings.IndexByte(pat[i:], ']'); j > 0 {
				b.WriteString(pat[i+1 : i+j])
				i += j + 1
				continue
			}
		}
		matched := false
		for _, tok := range dateTokens {
			if !strings.HasPrefix(pat[i:], tok) {
				continue
			}
			matched = true
			i += len(tok)
			if tok == "H" {
				b.WriteString(strconv.Itoa(t.Hour()))
			} else {
				b.WriteString(t.Format(dateLayout[tok]))
			}
			break
		}
		if !matched {
			b.WriteByte(pat[i])
			i++
		}
	}
	return b.String()
}

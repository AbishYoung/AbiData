package reports

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// Styling limits (ADR 0018 §2).
const (
	MaxRules    = 8  // conditional formatting rules per control
	maxRuleText = 64 // longest rule comparison value or label
)

// FormatRule is one conditional formatting rule: when the control's value
// satisfies the comparison, the style overrides apply (first match wins).
type FormatRule struct {
	Operator   string `json:"operator" enum:"<,<=,>,>=,=,!=,is_empty,is_not_empty"`
	Value      string `json:"value,omitempty" maxLength:"64" doc:"Comparison value (numbers compare numerically, other text case-insensitively)."`
	Color      string `json:"color,omitempty" maxLength:"64" doc:"Text colour override."`
	Bold       bool   `json:"bold,omitempty"`
	Background string `json:"background,omitempty" maxLength:"64" doc:"Background colour override."`
	Label      string `json:"label,omitempty" maxLength:"64" doc:"Accessible description read after the value when the rule applies (default: highlighted)."`
}

// CellFx is the style override a matching rule applies to one control instance.
type CellFx struct {
	Color      string `json:"color,omitempty"`
	Bold       bool   `json:"bold,omitempty"`
	Background string `json:"background,omitempty"`
	Label      string `json:"label"`
}

// safeColor mirrors the viewer's SAFE_COLOR (viewerLogic.ts): hex, a name, rgb()/hsl(), or a paper token.
var safeColor = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|[a-zA-Z]+|(rgb|hsl)a?\([\d\s,.%/]+\)|var\(--paper-[a-z-]+\))$`)

func validColor(s string) bool { return s == "" || (len(s) <= 64 && safeColor.MatchString(s)) }

var ruleOps = map[string]bool{"<": true, "<=": true, ">": true, ">=": true, "=": true, "!=": true, "is_empty": true, "is_not_empty": true}

var styleEnums = map[string]map[string]bool{
	"border_top":      {"solid": true, "dashed": true, "double": true, "none": true},
	"border_bottom":   {"solid": true, "dashed": true, "double": true, "none": true},
	"font_family":     {"sans": true, "serif": true, "mono": true},
	"vertical_align":  {"top": true, "middle": true, "bottom": true},
	"text_decoration": {"none": true, "underline": true, "line-through": true},
}

// validateStyle checks the typography and border tokens a control may carry.
func validateStyle(where string, style map[string]string) error {
	if len(style) > 32 {
		return bad("%s: too many style settings.", where)
	}
	for k, v := range style {
		if len(k) > 32 || len(v) > 64 {
			return bad("%s: a style setting is too long.", where)
		}
		if enum, ok := styleEnums[k]; ok && v != "" && !enum[v] {
			return bad("%s: %q is not a valid value for %s.", where, v, strings.ReplaceAll(k, "_", " "))
		}
	}
	if w := style["border_width"]; w != "" {
		switch w {
		case "1", "2", "3", "4", "5":
		default:
			return bad("%s: border width must be 1 to 5 px.", where)
		}
	}
	for _, k := range []string{"border_color", "color", "background"} {
		if !validColor(style[k]) {
			return bad("%s: %s is not a valid colour.", where, strings.ReplaceAll(k, "_", " "))
		}
	}
	return nil
}

func validateRules(where string, rules []FormatRule) error {
	if len(rules) > MaxRules {
		return bad("%s: at most %d conditional formatting rules.", where, MaxRules)
	}
	for i, r := range rules {
		if !ruleOps[r.Operator] {
			return bad("%s: rule %d has an unknown comparison.", where, i+1)
		}
		if utf8.RuneCountInString(r.Value) > maxRuleText || utf8.RuneCountInString(r.Label) > maxRuleText {
			return bad("%s: rule %d is too long.", where, i+1)
		}
		if !validColor(r.Color) || !validColor(r.Background) {
			return bad("%s: rule %d has an invalid colour.", where, i+1)
		}
		if r.Operator != "is_empty" && r.Operator != "is_not_empty" && r.Value == "" {
			return bad("%s: rule %d needs a value to compare with.", where, i+1)
		}
	}
	return nil
}

func validateBandColors(label string, b *Band) error {
	if !validColor(b.Background) || !validColor(b.AlternateBackground) {
		return bad("Band %s has an invalid background colour.", label)
	}
	return nil
}

// ruleMatch returns the effect of the first rule raw satisfies.
func ruleMatch(rules []FormatRule, raw any) *CellFx {
	for _, r := range rules {
		if !r.matches(raw) {
			continue
		}
		fx := &CellFx{Color: r.Color, Bold: r.Bold, Background: r.Background, Label: r.Label}
		if fx.Label == "" {
			fx.Label = "highlighted"
		}
		return fx
	}
	return nil
}

func (r FormatRule) matches(raw any) bool {
	empty := raw == nil
	if s, ok := raw.(string); ok {
		empty = strings.TrimSpace(s) == ""
	}
	switch r.Operator {
	case "is_empty":
		return empty
	case "is_not_empty":
		return !empty
	}
	if empty {
		return r.Operator == "!=" // nothing equals a value
	}
	if a, ok := asDecimal(raw); ok {
		if b, err := decimal.NewFromString(r.Value); err == nil {
			c := a.Cmp(b)
			return cmpOp(r.Operator, c)
		}
	}
	as, ok := raw.(string)
	if !ok {
		as = fmt.Sprint(raw)
	}
	return cmpOp(r.Operator, foldCompare(as, r.Value))
}

func cmpOp(op string, c int) bool {
	switch op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	case "=":
		return c == 0
	case "!=":
		return c != 0
	}
	return false
}

// Package reports stores banded report definitions and resolves them into a
// stream of section instances for the client paginator (D11, brief <reports>).
package reports

import (
	"fmt"
	"regexp"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/forms"
)

// Layout is a banded report layout (px at 96 dpi).
type ReportLayout struct {
	Width  int          `json:"width"`
	Bands  Bands        `json:"bands"`
	Groups []Group      `json:"groups,omitempty"`
	Sorts  []ReportSort `json:"sorts,omitempty"`
}

// Bands are the fixed bands; group headers/footers live in Groups.
type Bands struct {
	ReportHeader Band `json:"report_header"`
	PageHeader   Band `json:"page_header"`
	Detail       Band `json:"detail"`
	PageFooter   Band `json:"page_footer"`
	ReportFooter Band `json:"report_footer"`
}

// Band is one horizontal section.
type Band struct {
	Height       int             `json:"height"`
	CanGrow      bool            `json:"can_grow,omitempty"`
	KeepTogether bool            `json:"keep_together,omitempty"`
	ForceNewPage bool            `json:"force_new_page,omitempty"`
	Visible      *bool           `json:"visible,omitempty"`
	Controls     []ReportControl `json:"controls"`
}

// Control is a report control.
type ReportControl struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type" enum:"label,textbox,line,rect,page_number,page_count,now,map,image"`
	X         int               `json:"x"`
	Y         int               `json:"y"`
	W         int               `json:"w"`
	H         int               `json:"h"`
	Style     map[string]string `json:"style,omitempty"`
	Caption   string            `json:"caption,omitempty"`
	Format    string            `json:"format,omitempty"`
	Bind      *forms.Bind       `json:"bind,omitempty"`
	Calc      *forms.Calc       `json:"calc,omitempty"`
	Aggregate *Aggregate        `json:"aggregate,omitempty"`
	// MaxImages is an image control's gallery size: how many of the field's images print (default 1, at most MaxImagesLimit).
	MaxImages int               `json:"max_images,omitempty"`
	Map       *forms.MapOptions `json:"map,omitempty"`
}

// Aggregate is a structured aggregate (never parsed from text, D12).
type Aggregate struct {
	Func     string `json:"aggregate" enum:"sum,avg,count,min,max"`
	ColumnID string `json:"column_id,omitempty"` // empty with count = row count
	Scope    string `json:"scope" enum:"group,report"`
	Running  bool   `json:"running,omitempty"`
}

// Group defines a grouping level.
type Group struct {
	ColumnID   string `json:"column_id,omitempty"`
	LuaExpr    string `json:"lua_expr,omitempty"`
	ReportSort string `json:"sort,omitempty" enum:"asc,desc,"`
	Interval   int    `json:"interval,omitempty" doc:"Text: group by first N characters; number: by multiples of N."`
	Header     Band   `json:"header"`
	Footer     Band   `json:"footer"`
}

// Sort orders detail rows within groups.
type ReportSort struct {
	ColumnID string `json:"column_id"`
	Desc     bool   `json:"desc,omitempty"`
}

// PageSetup is the paper configuration.
type PageSetup struct {
	Paper       string `json:"paper" enum:"letter,a4,legal"`
	Orientation string `json:"orientation" enum:"portrait,landscape"`
	MarginTop   int    `json:"margin_top"` // px
	MarginRight int    `json:"margin_right"`
	MarginBot   int    `json:"margin_bottom"`
	MarginLeft  int    `json:"margin_left"`
}

var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

const (
	MaxGroups = 8
	maxBand   = 2000
	// MaxImagesLimit is the most images one image control can show.
	MaxImagesLimit = 24
)

func bad(format string, args ...any) error { return fault.Newf(fault.BadRequest, format, args...) }

// AllBands returns every band with a stable label.
func (l *ReportLayout) AllBands() map[string]*Band {
	m := map[string]*Band{
		"report_header": &l.Bands.ReportHeader, "page_header": &l.Bands.PageHeader, "detail": &l.Bands.Detail,
		"page_footer": &l.Bands.PageFooter, "report_footer": &l.Bands.ReportFooter,
	}
	for i := range l.Groups {
		m[fmt.Sprintf("group_header_%d", i)] = &l.Groups[i].Header
		m[fmt.Sprintf("group_footer_%d", i)] = &l.Groups[i].Footer
	}
	return m
}

// Validate checks a layout and page setup against the source table.
func Validate(l *ReportLayout, ps *PageSetup, t *catalog.Table, cols []string) error {
	has := func(id string) bool {
		if id == "id" {
			return true
		}
		if t != nil {
			_, ok := t.Column(id)
			return ok
		}
		for _, c := range cols {
			if c == id {
				return true
			}
		}
		return false
	}
	switch ps.Paper {
	case "letter", "a4", "legal":
	default:
		return bad("Paper must be Letter, A4 or Legal.")
	}
	if ps.Orientation != "portrait" && ps.Orientation != "landscape" {
		return bad("Orientation must be portrait or landscape.")
	}
	for _, m := range []int{ps.MarginTop, ps.MarginRight, ps.MarginBot, ps.MarginLeft} {
		if m < 0 || m > 300 {
			return bad("Margins must be between 0 and 300 px.")
		}
	}
	if l.Width <= 0 || l.Width > 4000 {
		return bad("Report width must be between 1 and 4000.")
	}
	if len(l.Groups) > MaxGroups {
		return bad("A report can have at most %d grouping levels.", MaxGroups)
	}
	for i, g := range l.Groups {
		if (g.ColumnID == "") == (g.LuaExpr == "") {
			return bad("Group %d needs exactly one field or expression.", i+1)
		}
		if g.ColumnID != "" && !has(g.ColumnID) {
			return bad("Group %d uses a field that is not in the record source.", i+1)
		}
		if g.LuaExpr != "" {
			if err := forms.CheckCalc(g.LuaExpr); err != nil {
				return bad("Group %d: %s", i+1, fault.Classify(err).DisplayMessage())
			}
		}
		if g.Interval < 0 || g.Interval > 1_000_000 {
			return bad("Group %d interval is out of range.", i+1)
		}
	}
	for _, s := range l.Sorts {
		if !has(s.ColumnID) {
			return bad("A sort uses a field that is not in the record source.")
		}
	}
	ids := map[string]bool{}
	names := map[string]bool{}
	for label, b := range l.AllBands() {
		if b.Height < 0 || b.Height > maxBand {
			return bad("Band %s height is out of range.", label)
		}
		for _, c := range b.Controls {
			where := fmt.Sprintf("Control %q", c.Name)
			if c.ID == "" || ids[c.ID] {
				return bad("%s needs a unique id.", where)
			}
			ids[c.ID] = true
			if !nameRe.MatchString(c.Name) || names[c.Name] {
				return bad("%s: control names must be unique identifiers.", where)
			}
			names[c.Name] = true
			switch c.Type {
			case "label", "textbox", "line", "rect", "page_number", "page_count", "now", "image":
				if c.Map != nil {
					return bad("%s: only map controls have map settings.", where)
				}
			case "map":
				if c.Bind == nil || c.Calc != nil || c.Aggregate != nil {
					return bad("%s: a map must be bound to a location or area field.", where)
				}
				if t != nil {
					if col, ok := t.Column(c.Bind.ColumnID); ok && !forms.IsGeoColumn(col) {
						return bad("%s: a map must be bound to a location or area field.", where)
					}
				}
				if err := forms.ValidateMapOptions(where, c.Map); err != nil {
					return err
				}
			default:
				return bad("%s has unknown type %q.", where, c.Type)
			}
			if c.X < 0 || c.Y < 0 || c.W < 0 || c.H < 0 || c.X+c.W > 4000 || c.Y+c.H > maxBand {
				return bad("%s is outside its band.", where)
			}
			n := 0
			if c.Bind != nil {
				n++
				if !has(c.Bind.ColumnID) {
					return bad("%s is bound to a field that is not in the record source.", where)
				}
			}
			if c.Calc != nil {
				n++
				if err := forms.CheckCalc(c.Calc.LuaExpr); err != nil {
					return bad("%s: %s", where, fault.Classify(err).DisplayMessage())
				}
			}
			if c.Aggregate != nil {
				n++
				a := c.Aggregate
				switch a.Func {
				case "sum", "avg", "min", "max":
					if a.ColumnID == "" || !has(a.ColumnID) {
						return bad("%s: the aggregate needs a field of the record source.", where)
					}
				case "count":
					if a.ColumnID != "" && !has(a.ColumnID) {
						return bad("%s: the aggregate field is not in the record source.", where)
					}
				default:
					return bad("%s: unknown aggregate %q.", where, a.Func)
				}
				if a.Scope != "group" && a.Scope != "report" {
					return bad("%s: aggregate scope must be group or report.", where)
				}
			}
			if n > 1 {
				return bad("%s can only have one of bind, calc or aggregate.", where)
			}
			if c.MaxImages < 0 || c.MaxImages > MaxImagesLimit {
				return bad("%s: show between 1 and %d images.", where, MaxImagesLimit)
			}
			if c.Type == "image" {
				// ADR 0011: shows the images of an attachments field.
				if c.Bind == nil {
					return bad("%s: an image control must be bound to an attachments field.", where)
				}
				if t != nil {
					if col, ok := t.Column(c.Bind.ColumnID); !ok || col.Type != catalog.Attachments {
						return bad("%s: an image control must be bound to an attachments field.", where)
					}
				}
			}
		}
	}
	return nil
}

// Bind and Calc are shared with forms.
type (
	Bind = forms.Bind
	Calc = forms.Calc
)

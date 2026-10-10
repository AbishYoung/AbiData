// Package forms stores form definitions and dispatches form events to Lua
// handlers using the request/patch protocol (D10, brief <forms>).
package forms

import (
	"fmt"
	"regexp"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/script"
)

// Layout is the absolute-positioned form layout (px at 96 dpi).
type Layout struct {
	Width    int               `json:"width"`
	Sections Sections          `json:"sections"`
	Events   map[string]string `json:"events,omitempty"` // form event → handler function name
}

// Sections are the header, detail and footer bands.
type Sections struct {
	Header Section `json:"header"`
	Detail Section `json:"detail"`
	Footer Section `json:"footer"`
}

// Section is one band.
type Section struct {
	Height   int       `json:"height"`
	Controls []Control `json:"controls"`
}

// Control is one control on the form.
type Control struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type" enum:"label,textbox,combobox,checkbox,button,line,rect,subform"`
	X        int               `json:"x"`
	Y        int               `json:"y"`
	W        int               `json:"w"`
	H        int               `json:"h"`
	TabIndex int               `json:"tab_index"`
	Style    map[string]string `json:"style,omitempty"`
	Visible  *bool             `json:"visible,omitempty"`
	Enabled  *bool             `json:"enabled,omitempty"`
	Locked   bool              `json:"locked,omitempty"`
	Bind     *Bind             `json:"bind,omitempty"`
	Calc     *Calc             `json:"calc,omitempty"`
	Format   string            `json:"format,omitempty"`
	Caption  string            `json:"caption,omitempty"`
	Lookup   *Lookup           `json:"lookup,omitempty"`
	Subform  *Subform          `json:"subform,omitempty"`
	Events   map[string]string `json:"events,omitempty"` // click|before_update|after_update|change → handler
}

// Bind binds a control to a column of the form's source.
type Bind struct {
	ColumnID string `json:"column_id"`
}

// Calc is a calculated control: a Lua expression over the record's fields.
type Calc struct {
	LuaExpr string `json:"lua_expr"`
}

// Lookup configures a combo box's row source.
type Lookup struct {
	SourceQueryID   string `json:"source_query_id,omitempty"`
	TableID         string `json:"table_id,omitempty"`
	ValueColumnID   string `json:"value_column_id"`
	DisplayColumnID string `json:"display_column_id"`
}

// Subform embeds another form linked master→child.
type Subform struct {
	FormID             string `json:"form_id"`
	LinkMasterColumnID string `json:"link_master_column_id"`
	LinkChildColumnID  string `json:"link_child_column_id"`
}

// Form events and control events (brief <forms>).
var (
	FormEvents    = map[string]bool{"load": true, "current": true, "before_insert": true, "before_update": true, "after_update": true, "before_delete": true, "after_delete": true, "close": true}
	ControlEvents = map[string]bool{"click": true, "before_update": true, "after_update": true, "change": true}
	controlTypes  = map[string]bool{"label": true, "textbox": true, "combobox": true, "checkbox": true, "button": true, "line": true, "rect": true, "subform": true}
	identRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,99}$`)
	controlNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)
)

// Limits on layouts.
const (
	MaxControls = 500
	MaxWidth    = 4000
	MaxHeight   = 4000
	maxCalcLen  = 2000
)

func bad(format string, args ...any) error { return fault.Newf(fault.BadRequest, format, args...) }

// AllControls returns every control across sections.
func (l *Layout) AllControls() []*Control {
	var out []*Control
	for _, s := range []*Section{&l.Sections.Header, &l.Sections.Detail, &l.Sections.Footer} {
		for i := range s.Controls {
			out = append(out, &s.Controls[i])
		}
	}
	return out
}

// Source describes what a form's controls may bind to.
type Source struct {
	Table   *catalog.Table // table source
	Columns []string       // query source output names (bind.column_id is then an output name)
}

func (s Source) has(colID string) bool {
	if s.Table != nil {
		if colID == "id" {
			return true
		}
		_, ok := s.Table.Column(colID)
		return ok
	}
	for _, c := range s.Columns {
		if c == colID {
			return true
		}
	}
	return false
}

// Validate checks a layout against its source and schema. formExists reports
// whether a subform's form ID exists.
func Validate(l *Layout, src Source, sch *catalog.Schema, formExists func(id string) bool) error {
	if l.Width <= 0 || l.Width > MaxWidth {
		return bad("Form width must be between 1 and %d.", MaxWidth)
	}
	for ev, fn := range l.Events {
		if !FormEvents[ev] {
			return bad("Unknown form event %q.", ev)
		}
		if !identRe.MatchString(fn) {
			return bad("Handler %q for %s must be a function name.", fn, ev)
		}
	}
	controls := l.AllControls()
	if len(controls) > MaxControls {
		return bad("A form can have at most %d controls.", MaxControls)
	}
	ids := map[string]bool{}
	names := map[string]bool{}
	for _, s := range []Section{l.Sections.Header, l.Sections.Detail, l.Sections.Footer} {
		if s.Height < 0 || s.Height > MaxHeight {
			return bad("Section heights must be between 0 and %d.", MaxHeight)
		}
	}
	for _, c := range controls {
		where := fmt.Sprintf("Control %q", c.Name)
		if c.ID == "" || ids[c.ID] {
			return bad("%s needs a unique id.", where)
		}
		ids[c.ID] = true
		if !controlNameRe.MatchString(c.Name) || names[strings.ToLower(c.Name)] {
			return bad("%s: control names must be unique identifiers.", where)
		}
		names[strings.ToLower(c.Name)] = true
		if !controlTypes[c.Type] {
			return bad("%s has unknown type %q.", where, c.Type)
		}
		if c.W < 0 || c.H < 0 || c.X < 0 || c.Y < 0 || c.X+c.W > MaxWidth || c.Y+c.H > MaxHeight {
			return bad("%s is outside the form.", where)
		}
		if len(c.Caption) > 2000 || len(c.Format) > 100 {
			return bad("%s: caption or format is too long.", where)
		}
		for ev, fn := range c.Events {
			if !ControlEvents[ev] {
				return bad("%s: unknown event %q.", where, ev)
			}
			if !identRe.MatchString(fn) {
				return bad("%s: handler %q must be a function name.", where, fn)
			}
		}
		if c.Bind != nil && c.Calc != nil {
			return bad("%s cannot be both bound and calculated.", where)
		}
		if c.Bind != nil && !src.has(c.Bind.ColumnID) {
			return bad("%s is bound to a field that is not in the form's record source.", where)
		}
		if c.Calc != nil {
			if err := CheckCalc(c.Calc.LuaExpr); err != nil {
				return bad("%s: %s", where, fault.Classify(err).DisplayMessage())
			}
		}
		if c.Type == "combobox" {
			if c.Lookup == nil {
				return bad("%s: a combo box needs a row source.", where)
			}
			if c.Lookup.TableID != "" {
				t, ok := sch.Table(c.Lookup.TableID)
				if !ok {
					return bad("%s: lookup table no longer exists.", where)
				}
				for _, id := range []string{c.Lookup.ValueColumnID, c.Lookup.DisplayColumnID} {
					if _, ok := t.Column(id); !ok && id != "id" {
						return bad("%s: lookup field is not in table %q.", where, t.DisplayName)
					}
				}
			} else if c.Lookup.SourceQueryID == "" {
				return bad("%s: a combo box needs a table or query row source.", where)
			}
		}
		if c.Type == "subform" {
			if c.Subform == nil || !formExists(c.Subform.FormID) {
				return bad("%s: the subform's form does not exist.", where)
			}
			if c.Subform.LinkMasterColumnID != "" && !src.has(c.Subform.LinkMasterColumnID) {
				return bad("%s: the master link field is not in the form's record source.", where)
			}
		}
	}
	return nil
}

// CheckCalc verifies a calculated-control expression parses as one Lua expression.
func CheckCalc(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" || len(expr) > maxCalcLen {
		return bad("A calculated control needs an expression of at most %d characters.", maxCalcLen)
	}
	if _, err := script.Compile("return ("+expr+")", "calc"); err != nil {
		return bad("The expression is not valid Lua: %s", fault.Classify(err).DisplayMessage())
	}
	return nil
}

// BoundColumns returns column IDs referenced by bindings, subform links and lookups.
func (l *Layout) BoundColumns() []string {
	var out []string
	for _, c := range l.AllControls() {
		if c.Bind != nil {
			out = append(out, c.Bind.ColumnID)
		}
		if c.Subform != nil && c.Subform.LinkMasterColumnID != "" {
			out = append(out, c.Subform.LinkMasterColumnID)
		}
		if c.Lookup != nil {
			out = append(out, c.Lookup.ValueColumnID, c.Lookup.DisplayColumnID)
		}
	}
	return out
}

// Package apps holds the rules of public apps (ADR 0014): forms and reports a
// workspace owner publishes behind a secret link. It decides what may be
// published, pins what an owner approved, and trims definitions down to what
// a visitor needs. The HTTP routes live in httpapi.
package apps

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/forms"
	"abidata/internal/reports"
)

// TokenLen is the length of a link token: base62, about 256 bits.
const TokenLen = 43

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// ActorPrefix marks the synthetic user id visitors of an app act as.
const ActorPrefix = "app:"

// ActorID is the user id recorded for writes made through an app.
func ActorID(appID string) string { return ActorPrefix + appID }

// IsActor reports whether a recorded user id belongs to an app.
func IsActor(userID string) bool { return strings.HasPrefix(userID, ActorPrefix) }

// DisplayName is how an app is named in history and audit views.
func DisplayName(name string) string { return `App "` + name + `"` }

// NewToken returns a fresh link token and its lookup hash.
func NewToken() (string, []byte) {
	out := make([]byte, 0, TokenLen)
	buf := make([]byte, 64)
	for len(out) < TokenLen {
		if _, err := rand.Read(buf); err != nil {
			panic(err) // crypto/rand never fails on supported platforms
		}
		for _, b := range buf {
			// 248 = 4*62: reject the tail so every character is uniform.
			if b < 248 && len(out) < TokenLen {
				out = append(out, base62[b%62])
			}
		}
	}
	return string(out), HashToken(string(out))
}

// HashToken is the lookup key of a token.
func HashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

// ValidToken reports whether tok has the shape NewToken produces, so malformed
// links are refused before any database access.
func ValidToken(tok string) bool {
	if len(tok) != TokenLen {
		return false
	}
	for i := 0; i < len(tok); i++ {
		if strings.IndexByte(base62, tok[i]) < 0 {
			return false
		}
	}
	return true
}

func refuse(format string, args ...any) error { return fault.Newf(fault.BadRequest, format, args...) }

// CheckForm reports why a form cannot be published as a submit-only page.
func CheckForm(f forms.Form) error {
	if f.SourceKind != "table" {
		return refuse("The form %q cannot be published: its record source must be a table.", f.Name)
	}
	if !f.AllowAdditions {
		return refuse("The form %q cannot be published: it does not allow new records. Turn on Allow additions first.", f.Name)
	}
	for _, c := range f.Layout.AllControls() {
		switch c.Type {
		case "subform", "attachments", "button", "map":
			return refuse("The form %q cannot be published: %s controls (%s) are not available on public pages yet.", f.Name, c.Type, c.Name)
		}
		if c.Lookup != nil && c.Lookup.TableID == "" && c.Lookup.SourceQueryID == "" {
			return refuse("The form %q cannot be published: the combo box %s has no row source.", f.Name, c.Name)
		}
	}
	return nil
}

// CheckReport reports why a report cannot be published.
func CheckReport(r reports.Report) error {
	for _, c := range reportControls(r) {
		switch c.Type {
		case "image", "map":
			return refuse("The report %q cannot be published: %s controls (%s) are not available on public pages yet.", r.Name, c.Type, c.Name)
		}
	}
	return nil
}

func reportControls(r reports.Report) []*reports.ReportControl {
	var out []*reports.ReportControl
	add := func(b *reports.Band) {
		for i := range b.Controls {
			out = append(out, &b.Controls[i])
		}
	}
	l := &r.ReportLayout
	for _, b := range []*reports.Band{&l.Bands.ReportHeader, &l.Bands.PageHeader, &l.Bands.Detail, &l.Bands.PageFooter, &l.Bands.ReportFooter} {
		add(b)
	}
	for i := range l.Groups {
		add(&l.Groups[i].Header)
		add(&l.Groups[i].Footer)
	}
	return out
}

// FormQueries are the saved queries a form reads (its combo boxes' row sources).
func FormQueries(f forms.Form) []string {
	var out []string
	for _, c := range f.Layout.AllControls() {
		if c.Lookup != nil && c.Lookup.SourceQueryID != "" {
			out = append(out, c.Lookup.SourceQueryID)
		}
	}
	return out
}

// ReportQueries are the saved queries a report reads (its record source).
func ReportQueries(r reports.Report) []string {
	if r.SourceKind == "query" && r.SourceID != "" {
		return []string{r.SourceID}
	}
	return nil
}

// Fingerprint names the exact versions behind a published item: the object,
// its module, every saved query it reads (also through other queries) and,
// when its module can require them, the shared modules. Any edit changes it.
// Items without shared modules keep the fingerprint they had before shared
// modules were part of it.
func Fingerprint(kind, id string, version, moduleVersion int64, queries, modules map[string]int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%s@%d/m%d", kind, id, version, moduleVersion)
	for _, q := range slices.Sorted(maps.Keys(queries)) {
		fmt.Fprintf(&b, ";q:%s@%d", q, queries[q])
	}
	for _, m := range slices.Sorted(maps.Keys(modules)) {
		fmt.Fprintf(&b, ";s:%s@%d", m, modules[m])
	}
	return b.String()
}

// requireWord finds the require function in Lua source.
var requireWord = regexp.MustCompile(`\brequire\b`)

// RequiresModules reports whether a form module can load shared modules. A
// sandboxed script reaches require only by its global name (there is no _G,
// getfenv, load or debug library), so a module that never names it cannot
// run shared module code. A mention in a comment or string only makes the
// check stricter.
func RequiresModules(source string) bool { return requireWord.MatchString(source) }

// PublicColumn is what a visitor learns about a field bound in a public form.
type PublicColumn struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	FieldType string `json:"field_type"`
	Required  bool   `json:"required"`
	MaxLength int    `json:"max_length,omitempty"`
	Format    string `json:"format,omitempty"`
}

// PublicForm is a form as a visitor sees it: its layout without handler or
// expression source, and only the fields it binds.
type PublicForm struct {
	Name    string         `json:"name"`
	Layout  forms.Layout   `json:"layout"`
	Columns []PublicColumn `json:"columns"`
}

// ViewForm trims f for visitors. t is the form's source table.
func ViewForm(f forms.Form, t *catalog.Table) PublicForm {
	l := f.Layout
	l.Events = nil
	l.Sections = cloneSections(l.Sections)
	for _, c := range l.AllControls() {
		c.Events = nil
		if c.Calc != nil {
			c.Calc = &forms.Calc{} // the server computes calculated controls; the expression is design detail
		}
	}
	out := PublicForm{Name: f.Name, Layout: l, Columns: []PublicColumn{}}
	seen := map[string]bool{}
	for _, id := range l.BoundColumns() {
		c, ok := t.Column(id)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		col := PublicColumn{ID: c.ID, Name: c.DisplayName, FieldType: string(c.Type), Required: c.Required, Format: c.Options.Format}
		if c.Type == catalog.ShortText {
			col.MaxLength = c.MaxLen()
		}
		out.Columns = append(out.Columns, col)
	}
	return out
}

// cloneSections copies the controls so trimming never touches the cached form.
func cloneSections(s forms.Sections) forms.Sections {
	cp := func(sec forms.Section) forms.Section {
		sec.Controls = slices.Clone(sec.Controls)
		return sec
	}
	return forms.Sections{Header: cp(s.Header), Detail: cp(s.Detail), Footer: cp(s.Footer)}
}

func cloneReportLayout(l reports.ReportLayout) reports.ReportLayout {
	cp := func(b reports.Band) reports.Band {
		b.Controls = slices.Clone(b.Controls)
		return b
	}
	b := &l.Bands
	b.ReportHeader, b.PageHeader, b.Detail, b.PageFooter, b.ReportFooter = cp(b.ReportHeader), cp(b.PageHeader), cp(b.Detail), cp(b.PageFooter), cp(b.ReportFooter)
	l.Groups = slices.Clone(l.Groups)
	for i := range l.Groups {
		l.Groups[i].Header, l.Groups[i].Footer = cp(l.Groups[i].Header), cp(l.Groups[i].Footer)
	}
	return l
}

// PublicReport is a report as a visitor's viewer needs it: the layout and
// paper, without module, group or calculated-control source.
type PublicReport struct {
	Name      string               `json:"name"`
	Layout    reports.ReportLayout `json:"layout"`
	PageSetup reports.PageSetup    `json:"page_setup"`
	Params    []PublicParam        `json:"parameters"`
}

// PublicParam is a parameter the visitor is asked for.
type PublicParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ViewReport trims r for visitors; params are its source query's parameters.
func ViewReport(r reports.Report, params []PublicParam) PublicReport {
	l := cloneReportLayout(r.ReportLayout)
	cp := reports.Report{ReportLayout: l}
	for _, c := range reportControls(cp) {
		if c.Calc != nil {
			c.Calc = &forms.Calc{}
		}
	}
	for i := range l.Groups {
		if l.Groups[i].LuaExpr != "" {
			l.Groups[i].LuaExpr = "expr"
		}
	}
	if params == nil {
		params = []PublicParam{}
	}
	return PublicReport{Name: r.Name, Layout: cp.ReportLayout, PageSetup: r.PageSetup, Params: params}
}

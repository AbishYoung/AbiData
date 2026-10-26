package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/query"
	"abidata/internal/workspace"
)

// Generated formats (ADR 0018 §4: background report generation).
const (
	FormatCSV  = "csv"
	FormatJSON = "json"
	FormatHTML = "html"
)

// MaxGenerated bounds one generated file (the artifact limit).
const MaxGenerated = 16 << 20

// MaxGenerateBuffer bounds the report instances one generation holds in memory
// before it renders them: a little over what a MaxGenerated file needs, so an
// oversize report fails while it runs instead of after building every
// instance (a variable so tests can lower it).
var MaxGenerateBuffer = 4 * MaxGenerated

// instanceOverhead approximates the cost of one buffered instance (struct and
// map) on top of its values.
const instanceOverhead = 256

// GenerateInput describes one background report run.
type GenerateInput struct {
	Lease  *workspace.Lease
	Schema *catalog.Schema
	Ref    string // report id or name
	Params query.Params
	Format string
	Caller Caller
}

// Generated is a rendered report file.
type Generated struct {
	Report  Report
	Content []byte
	Rows    int
	MIME    string
	Ext     string
}

// ByRef finds a report by id, or else by name (case-insensitive).
func (s *Service) ByRef(ctx context.Context, l *workspace.Lease, ref string) (Report, error) {
	if ref == "" {
		return Report{}, fault.New(fault.BadRequest, "Name a report.")
	}
	if r, err := s.Get(ctx, l, ref); err == nil {
		return r, nil
	} else if fault.Classify(err).Code != fault.NotFound {
		return Report{}, err
	}
	all, err := s.List(ctx, l)
	if err != nil {
		return Report{}, err
	}
	for _, r := range all {
		if strings.EqualFold(r.Name, ref) {
			return s.Get(ctx, l, r.ID)
		}
	}
	return Report{}, fault.Newf(fault.NotFound, "No report named %q.", ref)
}

// Generate runs a report in memory and renders it as csv, json or html.
func (s *Service) Generate(ctx context.Context, in GenerateInput) (Generated, error) {
	switch in.Format {
	case FormatCSV, FormatJSON, FormatHTML:
	default:
		return Generated{}, fault.New(fault.BadRequest, "Report format must be csv, json or html.")
	}
	rep, err := s.ByRef(ctx, in.Lease, in.Ref)
	if err != nil {
		return Generated{}, err
	}
	var insts []Instance
	buffered := 0
	tooLarge := fault.Newf(fault.BadRequest, "The generated report is larger than %d MiB; add criteria to the report's source.", MaxGenerated>>20)
	if err := s.Run(ctx, RunInput{Lease: in.Lease, Schema: in.Schema, Report: rep, Params: in.Params, Caller: in.Caller,
		Emit: func(i Instance) error {
			buffered += instanceOverhead
			for k, v := range i.Values {
				buffered += fieldOverhead + len(k)
				if text, ok := v.(string); ok {
					buffered += len(text)
				}
			}
			if buffered > MaxGenerateBuffer {
				return tooLarge
			}
			insts = append(insts, i)
			return nil
		}}); err != nil {
		return Generated{}, err
	}
	g := Generated{Report: rep}
	t := flatten(&rep.ReportLayout, insts)
	g.Rows = len(t.rows)
	switch in.Format {
	case FormatCSV:
		g.Content, g.MIME, g.Ext = csvBytes(t), "text/csv; charset=utf-8", "csv"
	case FormatJSON:
		b, err := json.MarshalIndent(buildTree(&rep.ReportLayout, insts), "", "  ")
		if err != nil {
			return Generated{}, err
		}
		g.Content, g.MIME, g.Ext = append(b, '\n'), "application/json", "json"
	case FormatHTML:
		g.Content, g.MIME, g.Ext = htmlBytes(&rep, insts, t), "text/html; charset=utf-8", "html"
	}
	if len(g.Content) > MaxGenerated {
		return Generated{}, tooLarge
	}
	return g, nil
}

// ---- flattening ------------------------------------------------------------------------------

type flatTable struct {
	headers []string
	rows    [][]string
}

func isDataControl(c ReportControl) bool {
	return c.Type == "textbox" && (c.Bind != nil || c.Calc != nil || c.Aggregate != nil)
}

func sortedControls(b *Band, keep func(ReportControl) bool) []ReportControl {
	var out []ReportControl
	for _, c := range b.Controls {
		if keep(c) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].X < out[j].X
	})
	return out
}

func valueText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	return fmt.Sprint(v)
}

var bandRe = regexp.MustCompile(`^group_(header|footer)_(\d+)$`)

func bandOf(l *ReportLayout, name string) *Band {
	switch name {
	case "report_header":
		return &l.Bands.ReportHeader
	case "detail":
		return &l.Bands.Detail
	case "report_footer":
		return &l.Bands.ReportFooter
	}
	if m := bandRe.FindStringSubmatch(name); m != nil {
		var i int
		_, _ = fmt.Sscanf(m[2], "%d", &i)
		if i >= 0 && i < len(l.Groups) {
			if m[1] == "header" {
				return &l.Groups[i].Header
			}
			return &l.Groups[i].Footer
		}
	}
	return nil
}

func groupLevel(name, kind string) (int, bool) {
	m := bandRe.FindStringSubmatch(name)
	if m == nil || m[1] != kind {
		return 0, false
	}
	var i int
	_, _ = fmt.Sscanf(m[2], "%d", &i)
	return i, true
}

// groupLabel is a group header's first non-empty text.
func groupLabel(l *ReportLayout, in Instance) string {
	b := bandOf(l, in.Band)
	if b == nil {
		return ""
	}
	for _, c := range sortedControls(b, func(c ReportControl) bool { return c.Type == "textbox" || c.Type == "label" }) {
		v, ok := in.Values[c.ID]
		if !ok && c.Type == "label" {
			v = c.Caption
		}
		if t := valueText(v); t != "" {
			return t
		}
	}
	return ""
}

func flatten(l *ReportLayout, insts []Instance) flatTable {
	levels := len(l.Groups)
	detail := sortedControls(&l.Bands.Detail, isDataControl)
	t := flatTable{}
	for i := 0; i < levels; i++ {
		t.headers = append(t.headers, fmt.Sprintf("Group %d", i+1))
	}
	for _, c := range detail {
		t.headers = append(t.headers, c.Name)
	}
	keys := make([]string, levels)
	for _, in := range insts {
		if in.Template {
			continue
		}
		if lv, ok := groupLevel(in.Band, "header"); ok && lv < levels {
			keys[lv] = groupLabel(l, in)
			for i := lv + 1; i < levels; i++ {
				keys[i] = ""
			}
		} else if in.Band == "detail" {
			row := append([]string{}, keys...)
			for _, c := range detail {
				row = append(row, valueText(in.Values[c.ID]))
			}
			t.rows = append(t.rows, row)
		}
	}
	return t
}

var plainNumber = regexp.MustCompile(`^[-+]?[\d,]*\.?\d+(?:[eE][-+]?\d+)?%?$|^\(?[\d,]+\.?\d*\)?$`)

// safeCell neutralises spreadsheet formulas in a text cell (leading = + - @ or control characters).
func safeCell(s string) string {
	if len(s) < 2 || plainNumber.MatchString(s) {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func csvBytes(t flatTable) []byte {
	var b bytes.Buffer
	b.WriteString("\xEF\xBB\xBF")
	write := func(row []string) {
		for i, f := range row {
			if i > 0 {
				b.WriteByte(',')
			}
			f = safeCell(f)
			if strings.ContainsAny(f, "\",\r\n") {
				f = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
			}
			b.WriteString(f)
		}
		b.WriteString("\r\n")
	}
	write(t.headers)
	for _, r := range t.rows {
		write(r)
	}
	return b.Bytes()
}

// ---- tree (json) ---------------------------------------------------------------------------------

type treeGroup struct {
	Key    string              `json:"key"`
	Level  int                 `json:"level"`
	Header map[string]string   `json:"header"`
	Rows   []map[string]string `json:"rows"`
	Groups []*treeGroup        `json:"groups"`
	Footer map[string]string   `json:"footer"`
}

type treeReport struct {
	ReportHeader map[string]string   `json:"report_header"`
	Rows         []map[string]string `json:"rows"`
	Groups       []*treeGroup        `json:"groups"`
	ReportFooter map[string]string   `json:"report_footer"`
}

func named(l *ReportLayout, in Instance) map[string]string {
	out := map[string]string{}
	if b := bandOf(l, in.Band); b != nil {
		for _, c := range sortedControls(b, isDataControl) {
			out[c.Name] = valueText(in.Values[c.ID])
		}
	}
	return out
}

func buildTree(l *ReportLayout, insts []Instance) *treeReport {
	root := &treeReport{ReportHeader: map[string]string{}, Rows: []map[string]string{}, Groups: []*treeGroup{}, ReportFooter: map[string]string{}}
	var stack []*treeGroup
	for _, in := range insts {
		if in.Template {
			continue
		}
		switch in.Band {
		case "report_header":
			root.ReportHeader = named(l, in)
		case "report_footer":
			root.ReportFooter = named(l, in)
		case "detail":
			if len(stack) > 0 {
				g := stack[len(stack)-1]
				g.Rows = append(g.Rows, named(l, in))
			} else {
				root.Rows = append(root.Rows, named(l, in))
			}
		default:
			if lv, ok := groupLevel(in.Band, "header"); ok {
				if lv > len(stack) {
					lv = len(stack)
				}
				stack = stack[:lv]
				g := &treeGroup{Key: groupLabel(l, in), Level: lv, Header: named(l, in), Rows: []map[string]string{}, Groups: []*treeGroup{}, Footer: map[string]string{}}
				if lv > 0 {
					stack[lv-1].Groups = append(stack[lv-1].Groups, g)
				} else {
					root.Groups = append(root.Groups, g)
				}
				stack = append(stack, g)
			} else if lv, ok := groupLevel(in.Band, "footer"); ok && lv < len(stack) {
				stack[lv].Footer = named(l, in)
			}
		}
	}
	return root
}

// ---- html ------------------------------------------------------------------------------------------

func htmlBytes(rep *Report, insts []Instance, t flatTable) []byte {
	var b bytes.Buffer
	esc := html.EscapeString
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">")
	b.WriteString("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	fmt.Fprintf(&b, "<title>%s</title>", esc(rep.Name))
	b.WriteString("<style>body{font:14px/1.4 system-ui,sans-serif;margin:2rem}table{border-collapse:collapse;width:100%}th,td{border-bottom:1px solid #ccc;padding:.25rem .5rem;text-align:left}th{border-bottom:2px solid #000}</style></head><body>")
	fmt.Fprintf(&b, "<h1>%s</h1>\n<table><thead><tr>", esc(rep.Name))
	for _, h := range t.headers {
		fmt.Fprintf(&b, "<th scope=\"col\">%s</th>", esc(h))
	}
	b.WriteString("</tr></thead><tbody>\n")
	for _, r := range t.rows {
		b.WriteString("<tr>")
		for _, c := range r {
			fmt.Fprintf(&b, "<td>%s</td>", esc(c))
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody>")
	var foot []string
	for _, in := range insts {
		if in.Band == "report_footer" {
			for _, c := range sortedControls(&rep.ReportLayout.Bands.ReportFooter, isDataControl) {
				if v := valueText(in.Values[c.ID]); v != "" {
					foot = append(foot, c.Name+": "+v)
				}
			}
		}
	}
	if len(foot) > 0 {
		fmt.Fprintf(&b, "<tfoot><tr><td colspan=\"%d\">%s</td></tr></tfoot>", max(1, len(t.headers)), esc(strings.Join(foot, "   ")))
	}
	b.WriteString("</table></body></html>\n")
	return b.Bytes()
}

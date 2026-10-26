package apps

import (
	"strings"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/forms"
	"abidata/internal/reports"
)

func TestTokens(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		tok, h := NewToken()
		if !ValidToken(tok) || seen[tok] || string(HashToken(tok)) != string(h) {
			t.Fatalf("token %q", tok)
		}
		seen[tok] = true
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", TokenLen-1), strings.Repeat("a", TokenLen-1) + "-", strings.Repeat("a", TokenLen+1)} {
		if ValidToken(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
	if !IsActor(ActorID("X")) || IsActor("u1") || DisplayName("Shop") != `App "Shop"` {
		t.Fatal("actor helpers")
	}
}

func form(controls ...forms.Control) forms.Form {
	f := forms.Form{ID: "F", Name: "Sign up", SourceKind: "table", SourceID: "T", AllowAdditions: true}
	f.Layout.Sections.Detail.Controls = controls
	return f
}

func TestCheckForm(t *testing.T) {
	ok := form(forms.Control{Name: "Name", Type: "textbox", Bind: &forms.Bind{ColumnID: "c1"}},
		forms.Control{Name: "Kind", Type: "combobox", Bind: &forms.Bind{ColumnID: "c2"}, Lookup: &forms.Lookup{SourceQueryID: "Q1", ValueColumnID: "id", DisplayColumnID: "name"}})
	if err := CheckForm(ok); err != nil {
		t.Fatal(err)
	}
	if q := FormQueries(ok); len(q) != 1 || q[0] != "Q1" {
		t.Fatalf("queries %v", q)
	}
	cases := map[string]forms.Form{
		"record source":   func() forms.Form { f := form(); f.SourceKind = "query"; return f }(),
		"Allow additions": func() forms.Form { f := form(); f.AllowAdditions = false; return f }(),
		"subform":         form(forms.Control{Name: "Lines", Type: "subform"}),
		"attachments":     form(forms.Control{Name: "Photos", Type: "attachments"}),
		"button":          form(forms.Control{Name: "Go", Type: "button"}),
		"map":             form(forms.Control{Name: "Where", Type: "map"}),
		"row source":      form(forms.Control{Name: "Kind", Type: "combobox", Lookup: &forms.Lookup{}}),
	}
	for want, f := range cases {
		err := CheckForm(f)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestCheckReport(t *testing.T) {
	r := reports.Report{Name: "List", SourceKind: "query", SourceID: "Q"}
	r.ReportLayout.Bands.Detail.Controls = []reports.ReportControl{{Name: "n", Type: "textbox"}}
	if err := CheckReport(r); err != nil {
		t.Fatal(err)
	}
	if q := ReportQueries(r); len(q) != 1 || q[0] != "Q" {
		t.Fatalf("queries %v", q)
	}
	r.ReportLayout.Groups = []reports.Group{{Footer: reports.Band{Controls: []reports.ReportControl{{Name: "pic", Type: "image"}}}}}
	if err := CheckReport(r); err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("image in a group footer: %v", err)
	}
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint("form", "F", 3, 2, map[string]int64{"Q2": 1, "Q1": 5}, nil)
	if a != "form:F@3/m2;q:Q1@5;q:Q2@1" {
		t.Fatalf("got %s", a)
	}
	withMods := Fingerprint("form", "F", 3, 2, map[string]int64{"Q2": 1, "Q1": 5}, map[string]int64{"S2": 1, "S1": 4})
	if withMods != a+";s:S1@4;s:S2@1" {
		t.Fatalf("with modules: %s", withMods)
	}
	for _, b := range []string{
		Fingerprint("form", "F", 4, 2, map[string]int64{"Q2": 1, "Q1": 5}, nil),
		Fingerprint("form", "F", 3, 3, map[string]int64{"Q2": 1, "Q1": 5}, nil),
		Fingerprint("form", "F", 3, 2, map[string]int64{"Q2": 2, "Q1": 5}, nil),
		Fingerprint("form", "F", 3, 2, map[string]int64{"Q1": 5}, nil),
		Fingerprint("form", "F", 3, 2, map[string]int64{"Q2": 1, "Q1": 5}, map[string]int64{"S1": 1}),
	} {
		if b == a {
			t.Fatalf("an edit kept the fingerprint %s", b)
		}
	}
}

func TestRequiresModules(t *testing.T) {
	for src, want := range map[string]bool{
		"":                                       false,
		"function on_load() ui.notify('x') end":  false,
		"local requirement = 1":                  false,
		"local u = require('util')":              true,
		"local r = require\nlocal u = r('util')": true,
		"-- require nothing":                     true, // stricter, never weaker
	} {
		if got := RequiresModules(src); got != want {
			t.Errorf("%q: %v, want %v", src, got, want)
		}
	}
}

func TestViewFormTrims(t *testing.T) {
	f := form(
		forms.Control{Name: "Name", Type: "textbox", Bind: &forms.Bind{ColumnID: "c1"}, Events: map[string]string{"change": "on_name"}},
		forms.Control{Name: "Total", Type: "textbox", Calc: &forms.Calc{LuaExpr: "secret_formula()"}},
	)
	f.Layout.Events = map[string]string{"before_insert": "check"}
	f.ModuleSource = "function check() end"
	tbl := &catalog.Table{ID: "T", Columns: []catalog.Column{
		{ID: "c1", DisplayName: "Name", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: 40}},
		{ID: "c9", DisplayName: "Salary", Type: catalog.Currency},
	}}
	v := ViewForm(f, tbl)
	if v.Layout.Events != nil || v.Layout.Sections.Detail.Controls[0].Events != nil || v.Layout.Sections.Detail.Controls[1].Calc.LuaExpr != "" {
		t.Fatalf("design detail leaked: %+v", v.Layout)
	}
	if len(v.Columns) != 1 || v.Columns[0] != (PublicColumn{ID: "c1", Name: "Name", FieldType: "short_text", Required: true, MaxLength: 40}) {
		t.Fatalf("columns %+v", v.Columns)
	}
	// The cached form is untouched.
	if f.Layout.Sections.Detail.Controls[1].Calc.LuaExpr == "" || f.Layout.Sections.Detail.Controls[0].Events == nil {
		t.Fatal("trimming changed the original")
	}
}

func TestViewReportTrims(t *testing.T) {
	r := reports.Report{Name: "List", ModuleSource: "x = 1"}
	r.ReportLayout.Bands.Detail.Controls = []reports.ReportControl{{ID: "a", Type: "textbox", Calc: &forms.Calc{LuaExpr: "secret()"}}}
	r.ReportLayout.Groups = []reports.Group{{LuaExpr: "group_secret()"}}
	v := ViewReport(r, nil)
	if v.Layout.Bands.Detail.Controls[0].Calc.LuaExpr != "" || v.Layout.Groups[0].LuaExpr == "group_secret()" || v.Params == nil {
		t.Fatalf("design detail leaked: %+v", v)
	}
	if r.ReportLayout.Bands.Detail.Controls[0].Calc.LuaExpr != "secret()" || r.ReportLayout.Groups[0].LuaExpr != "group_secret()" {
		t.Fatal("trimming changed the original")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLimiter()
	l.now = func() time.Time { return now }
	for i := range 10 {
		if ok, _ := l.Allow("a", Budget{PerMinute: 60, Burst: 10}); !ok {
			t.Fatalf("request %d refused", i)
		}
	}
	ok, wait := l.Allow("a", Budget{PerMinute: 60, Burst: 10})
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("burst not limited: %v %v", ok, wait)
	}
	if ok, _ := l.Allow("b", Budget{PerMinute: 60, Burst: 10}); !ok {
		t.Fatal("buckets are not independent")
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow("a", Budget{PerMinute: 60, Burst: 10}); !ok {
		t.Fatal("bucket did not refill")
	}
}

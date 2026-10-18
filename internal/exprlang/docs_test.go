package exprlang

import (
	"os"
	"testing"
)

func TestEveryFunctionIsDocumented(t *testing.T) {
	for _, f := range Docs() {
		if f.Signature == "" || f.Summary == "" || f.Group == "" {
			t.Errorf("%s has no documentation in docs.go", f.Name)
		}
	}
	for name := range docs {
		found := false
		for _, f := range Functions(Query) {
			found = found || f.Name == name
		}
		if !found {
			t.Errorf("docs.go documents %s, which is not in the catalog", name)
		}
	}
}

// Every example in the reference parses (in the scope it would be used in).
func TestReferenceExamplesParse(t *testing.T) {
	cols := []Column{}
	for _, n := range []string{"First", "Last", "City", "Country", "Phone", "Code", "Price", "Qty", "Ordered", "Shipped",
		"Discount", "Nickname", "Name", "Total", "Count", "Data", "Spot", "Depot", "Zone", "Fence"} {
		cols = append(cols, Column{Name: n})
	}
	sc := Scope{Mode: Query, Sources: []Source{{Columns: cols}}}
	for _, f := range Docs() {
		if f.Example == "" || f.Name == "char" {
			continue
		}
		if _, err := Parse(f.Example, sc); err != nil {
			t.Errorf("%s example %q: %v", f.Name, f.Example, err)
		}
	}
}

func TestGeneratedReferenceIsCurrent(t *testing.T) {
	b, err := os.ReadFile("../../docs/content/reference/expressions.md")
	if err != nil || string(b) != Markdown() {
		t.Fatal("docs/content/reference/expressions.md is stale; run make gen")
	}
}

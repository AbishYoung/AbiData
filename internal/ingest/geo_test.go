package ingest

import (
	"strings"
	"testing"

	"abidata/internal/catalog"
)

// Importing text into geo fields uses the same parser as the records API.
func TestConvertRowGeoText(t *testing.T) {
	p := &plan{Cols: []mapCol{
		{Col: catalog.Column{ID: "P", DisplayName: "Spot", Type: catalog.GeoPoint}},
		{Col: catalog.Column{ID: "A", DisplayName: "Fence", Type: catalog.GeoArea}},
	}}
	out, rerr := convertRow(p, []any{"51.50135, -0.14189", `{"type":"Polygon","coordinates":[[[0,0],[0,1],[1,1],[1,0]]]}`})
	if rerr != nil {
		t.Fatal(rerr)
	}
	if out[0] != `{"type":"Point","coordinates":[-0.14189,51.50135]}` || out[1] != `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}` {
		t.Fatalf("%v", out)
	}
	if out, rerr = convertRow(p, []any{"POINT(10 20)", "  "}); rerr != nil || out[0] != `{"type":"Point","coordinates":[10,20]}` || out[1] != nil {
		t.Fatalf("wkt/blank: %v %v", out, rerr)
	}
	_, rerr = convertRow(p, []any{"Paris", nil})
	if rerr == nil || rerr.Column != "Spot" || !strings.Contains(rerr.Message, "Spot") {
		t.Fatalf("bad point: %+v", rerr)
	}
	_, rerr = convertRow(p, []any{nil, int64(5)})
	if rerr == nil || rerr.Column != "Fence" {
		t.Fatalf("number into area: %+v", rerr)
	}
	_, rerr = convertRow(p, []any{"95, 0", nil})
	if rerr == nil || !strings.Contains(rerr.Message, "latitude") {
		t.Fatalf("range: %+v", rerr)
	}
}

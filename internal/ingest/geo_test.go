package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
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

// A point can be built from a latitude and a longitude column (ADR 0010).
func TestImportPointFromTwoColumns(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Sightings", catalog.ColumnSpec{DisplayName: "Species", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Spot", Type: catalog.GeoPoint})
	m := e.upload("s.csv", "Species,Lat,Lon\nKudzu,51.5,-0.14\nReed,,\nOak,91,0\nAsh,12,\nElm,abc,1\n", UploadOptions{})
	lon := 2
	mp := ImportMapping{Mode: ModeAppend, TableID: tb.ID, Columns: []ColumnMap{
		{Source: 0, ColumnID: col2(nil, tb, "Species").ID},
		{Source: 1, ColumnID: col2(nil, tb, "Spot").ID, LonSource: &lon},
	}}
	if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mp); err != nil {
		t.Fatal(err)
	}
	val, err := e.svc.Validate(e.ctx, e.l, m.ID)
	if err != nil || val.ErrorCount != 3 {
		t.Fatalf("validate %+v %v", val, err)
	}
	for _, re := range val.Errors {
		if re.Column != "Spot" {
			t.Fatalf("error should name the point field: %+v", re)
		}
	}
	res := result(t, e.commit(m.ID))
	if res.Inserted != 2 {
		t.Fatalf("%+v", res)
	}
	var got string
	if err := e.l.Read(e.ctx, func(q dbx.Querier) error {
		return q.QueryRow(e.ctx, `SELECT "Spot" FROM "Sightings" WHERE "Species" = 'Kudzu'`).Scan(&got)
	}); err != nil {
		t.Fatal(err)
	}
	// Compared as JSON: PostgreSQL's jsonb stores it with its own spacing.
	var pt struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(got), &pt); err != nil || pt.Type != "Point" || len(pt.Coordinates) != 2 ||
		pt.Coordinates[0] != -0.14 || pt.Coordinates[1] != 51.5 {
		t.Fatalf("%q %v", got, err)
	}
}

func TestPointFromTwoColumnsMappingErrors(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Sightings", catalog.ColumnSpec{DisplayName: "Species", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Spot", Type: catalog.GeoPoint})
	m := e.upload("s.csv", "Species,Lat,Lon\nKudzu,1,2\n", UploadOptions{})
	two, bad := 2, 9
	sp, spot := col2(nil, tb, "Species").ID, col2(nil, tb, "Spot").ID
	for name, cols := range map[string][]ColumnMap{
		"not a point":   {{Source: 1, ColumnID: sp, LonSource: &two}},
		"no field":      {{Source: 0, ColumnID: sp}, {Source: 1, LonSource: &two}},
		"bad lon":       {{Source: 1, ColumnID: spot, LonSource: &bad}},
		"lon reused":    {{Source: 1, ColumnID: spot, LonSource: &two}, {Source: 2, ColumnID: sp}},
		"lon is source": {{Source: 2, ColumnID: spot, LonSource: &two}},
	} {
		if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, ImportMapping{Mode: ModeAppend, TableID: tb.ID, Columns: cols}); fcode(err) != "BAD_REQUEST" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

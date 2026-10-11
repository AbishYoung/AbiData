package catalog

import (
	"fmt"
	"testing"

	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

func TestConvertGeo(t *testing.T) {
	pt := `{"type":"Point","coordinates":[-0.14189,51.50135]}`
	area := `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`
	tests := []struct {
		name     string
		from, to FieldType
		in       any
		want     any
		ok       bool
	}{
		{"short to point", ShortText, GeoPoint, "51.50135, -0.14189", pt, true},
		{"long to point from json", LongText, GeoPoint, `{"lat":51.50135,"lon":-0.14189}`, pt, true},
		{"wkt to point", ShortText, GeoPoint, "POINT(-0.14189 51.50135)", pt, true},
		{"blank to point", ShortText, GeoPoint, "  ", nil, true},
		{"nil to point", ShortText, GeoPoint, nil, nil, true},
		{"bad text to point", ShortText, GeoPoint, "London", nil, false},
		{"out of range to point", ShortText, GeoPoint, "99, 0", nil, false},
		{"long to area", LongText, GeoArea, `{"type":"Polygon","coordinates":[[[0,0],[0,1],[1,1],[1,0]]]}`, area, true},
		{"point text to area", ShortText, GeoArea, "1, 2", nil, false},
		{"point to short", GeoPoint, ShortText, pt, "51.50135, -0.14189", true},
		{"point to long", GeoPoint, LongText, pt, "51.50135, -0.14189", true},
		{"area to long", GeoArea, LongText, area, area, true},
		{"nil point to text", GeoPoint, LongText, nil, nil, true},
		{"point to area", GeoPoint, GeoArea, pt, nil, false},
		{"area to point", GeoArea, GeoPoint, area, nil, false},
		{"point to integer", GeoPoint, Integer, pt, nil, false},
		{"integer to point", Integer, GeoPoint, int64(1), nil, false},
		{"point to hyperlink", GeoPoint, Hyperlink, pt, nil, false},
		{"hyperlink to point", Hyperlink, GeoPoint, "https://example.com", nil, false},
		{"point to date", GeoPoint, Date, pt, nil, false},
		{"yes_no to point", YesNo, GeoPoint, int64(1), nil, false},
		{"number to area", Number, GeoArea, 1.5, nil, false},
	}
	for _, c := range tests {
		got, ok := ConvertValue(c.from, c.to, c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: ConvertValue(%s→%s, %v) = %v, %v; want %v, %v", c.name, c.from, c.to, c.in, got, ok, c.want, c.ok)
		}
	}
}

func (f *fixture) mustFail(sql string) {
	f.t.Helper()
	err := f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(f.ctx, sql)
		return err
	})
	if err == nil {
		f.t.Fatalf("expected %q to fail", sql)
	}
}

func TestGeoColumnLifecycle(t *testing.T) {
	f := newFixture(t)
	f.apply(Op{Kind: OpCreateTable, Table: &TableSpec{DisplayName: "Sightings", Columns: []ColumnSpec{
		{DisplayName: "Spot", Type: GeoPoint},
		{DisplayName: "Zone", Type: GeoArea},
		{DisplayName: "Centre", Type: Calculated, Options: TypeOptions{Expression: "geo_centroid(Zone)", ResultType: "geo_point"}},
		{DisplayName: "Inside", Type: Calculated, Options: TypeOptions{Expression: "geo_within(Spot, Zone)", ResultType: "yes_no"}},
	}}})
	tb, _ := f.schema().TableByPhysical("Sightings")
	if got := f.query(`SELECT name, type FROM pragma_table_info('Sightings') WHERE name IN ('Spot','Zone') ORDER BY cid`); fmt.Sprint(got) != "[[Spot TEXT] [Zone TEXT]]" {
		t.Fatalf("geo columns are TEXT: %v", got)
	}
	f.exec(`INSERT INTO Sightings(Spot, Zone) VALUES (geo_point(1, 1), '{"type":"Polygon","coordinates":[[[0,0],[2,0],[2,2],[0,2],[0,0]]]}')`)
	f.exec(`INSERT INTO Sightings(Spot) VALUES (geo_point(5, 5))`)
	if got := fmt.Sprint(f.query(`SELECT Inside, Centre FROM Sightings ORDER BY id`)); got != `[[1 {"type":"Point","coordinates":[1,1]}] [<nil> <nil>]]` {
		t.Fatalf("calculated: %s", got)
	}
	// The type CHECK keeps the wrong geometry out, even for writers that bypass the records API.
	f.mustFail(`INSERT INTO Sightings(Zone) VALUES (geo_point(1, 1))`)
	f.mustFail(`INSERT INTO Sightings(Spot) VALUES ('not json')`)

	// geo_point -> short_text writes "lat, lon"; back again restores the GeoJSON.
	spot, _ := tb.ColumnByPhysical("Spot")
	f.apply(Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: spot.ID, Column: &ColumnSpec{DisplayName: "Spot", Type: ShortText}})
	if got := fmt.Sprint(f.query(`SELECT Spot FROM Sightings ORDER BY id`)); got != "[[1, 1] [5, 5]]" {
		t.Fatalf("to text: %s", got)
	}
	f.apply(Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: spot.ID, Column: &ColumnSpec{DisplayName: "Spot", Type: GeoPoint}})
	if got := fmt.Sprint(f.query(`SELECT Spot FROM Sightings ORDER BY id`)); got != `[[{"type":"Point","coordinates":[1,1]}] [{"type":"Point","coordinates":[5,5]}]]` {
		t.Fatalf("back to point: %s", got)
	}

	// A point cannot become an area or a number: the impact report names the rows.
	for _, to := range []FieldType{GeoArea, Integer} {
		r, err := f.run(Preview, ApplyOptions{}, Op{Kind: OpAlterColumn, TableID: tb.ID, ColumnID: spot.ID, Column: &ColumnSpec{DisplayName: "Spot", Type: to}})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.ConversionFailures) != 1 || r.ConversionFailures[0].Count != 2 || r.ConversionFailures[0].To != string(to) {
			t.Fatalf("to %s expected a conversion failure: %+v", to, r)
		}
	}
}

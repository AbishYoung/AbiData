package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
)

const sightingsGeoJSON = `{
  "type": "FeatureCollection",
  "name": "ignored",
  "features": [
    {"type":"Feature","id":1,"properties":{"Species":"Kudzu","Count":3,"geometry":"prop"},"geometry":{"type":"Point","coordinates":[-0.14,51.5]}},
    {"type":"Feature","properties":{"Species":"Reed","Notes":"wet","Nested":{"a":1}},"geometry":{"type":"Point","coordinates":[1,2]}},
    {"type":"Feature","properties":null,"geometry":null}
  ]
}`

func TestParseGeoJSONFeatureCollection(t *testing.T) {
	e := newEnv(t)
	m := e.upload("sightings.geojson", sightingsGeoJSON, UploadOptions{})
	if m.Format != "geojson" || m.RowCount != 3 {
		t.Fatalf("%+v", m)
	}
	var names []string
	for _, c := range m.Columns {
		names = append(names, c.Name)
	}
	want := "Species,Count,geometry (property),Notes,Nested,geometry"
	if strings.Join(names, ",") != want {
		t.Fatalf("columns %v, want %s", names, want)
	}
	last := m.Columns[len(m.Columns)-1]
	if last.Type != catalog.GeoPoint {
		t.Fatalf("geometry column type %q", last.Type)
	}
	if m.Columns[1].Type != catalog.Integer {
		t.Fatalf("count type %q", m.Columns[1].Type)
	}
}

func TestImportGeoJSONNewTable(t *testing.T) {
	e := newEnv(t)
	m := e.upload("s.geojson", sightingsGeoJSON, UploadOptions{})
	mp := ImportMapping{Mode: ModeAppend, NewTable: &NewTable{Name: "Sightings", Columns: []NewColumn{
		{Source: 0, Name: "Species", Type: catalog.ShortText},
		{Source: 1, Name: "Count", Type: catalog.Integer},
		{Source: 5, Name: "Spot", Type: catalog.GeoPoint},
	}}}
	if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mp); err != nil {
		t.Fatal(err)
	}
	res := result(t, e.commit(m.ID))
	if res.Inserted != 3 || !res.TableCreated {
		t.Fatalf("%+v", res)
	}
	var spot string
	var withGeo int
	if err := e.l.Read(e.ctx, func(q dbx.Querier) error {
		if err := q.QueryRow(e.ctx, `SELECT "Spot" FROM "Sightings" WHERE "Species" = 'Kudzu'`).Scan(&spot); err != nil {
			return err
		}
		return q.QueryRow(e.ctx, `SELECT count(*) FROM "Sightings" WHERE "Spot" IS NOT NULL`).Scan(&withGeo)
	}); err != nil || withGeo != 2 {
		t.Fatalf("%q %d %v", spot, withGeo, err)
	}
	// Compared as JSON: PostgreSQL's jsonb stores it with its own spacing.
	var pt struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(spot), &pt); err != nil || pt.Type != "Point" || len(pt.Coordinates) != 2 ||
		pt.Coordinates[0] != -0.14 || pt.Coordinates[1] != 51.5 {
		t.Fatalf("%q %v", spot, err)
	}
}

func TestImportGeoJSONAreasIntoExistingTable(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Zones", catalog.ColumnSpec{DisplayName: "Name", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Fence", Type: catalog.GeoArea})
	m := e.upload("zones.geojson", `{"type":"FeatureCollection","features":[
		{"type":"Feature","properties":{"Name":"A"},"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}},
		{"type":"Feature","properties":{"Name":"B"},"geometry":{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,1],[0,0]]],[[[2,2],[3,2],[3,3],[2,3],[2,2]]]]}},
		{"type":"Feature","properties":{"Name":"Line"},"geometry":{"type":"LineString","coordinates":[[0,0],[1,1]]}}
	]}`, UploadOptions{})
	if m.Columns[1].Type != catalog.ShortText && m.Columns[1].Type != catalog.LongText {
		t.Fatalf("a mixed geometry column is not inferred as a geo type: %q", m.Columns[1].Type)
	}
	if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeAppend, "", "Name", "Fence")); err != nil {
		t.Fatal(err)
	}
	val, err := e.svc.Validate(e.ctx, e.l, m.ID)
	if err != nil || val.ErrorCount != 1 || val.Errors[0].Column != "Fence" || val.Errors[0].Row != 3 {
		t.Fatalf("a line is not an area: %+v %v", val, err)
	}
	if res := result(t, e.commit(m.ID)); res.Inserted != 2 || e.count(tb) != 2 {
		t.Fatalf("%+v", res)
	}
}

func TestParseGeoJSONSingleFeatureAndKeyOrder(t *testing.T) {
	e := newEnv(t)
	one := e.upload("one.geojson", `{"type":"Feature","properties":{"Name":"x"},"geometry":{"type":"Point","coordinates":[1,2]}}`, UploadOptions{})
	if one.RowCount != 1 || one.Columns[1].Name != "geometry" {
		t.Fatalf("%+v", one)
	}
	// "features" before "type", a later feature with an extra property: the
	// geometry column still comes last.
	m := e.upload("k.geojson", `{"features":[
		{"type":"Feature","properties":{"a":"1"},"geometry":{"type":"Point","coordinates":[1,2]}},
		{"type":"Feature","properties":{"b":"2"},"geometry":{"type":"Point","coordinates":[3,4]}}],"type":"FeatureCollection"}`, UploadOptions{})
	if len(m.Columns) != 3 || m.Columns[2].Name != "geometry" || m.Columns[2].Type != catalog.GeoPoint {
		t.Fatalf("%+v", m.Columns)
	}
}

func TestParseGeoJSONRejectsOtherShapes(t *testing.T) {
	e := newEnv(t)
	for name, body := range map[string]string{
		"array":         `[{"type":"Feature"}]`,
		"geometry only": `{"type":"Point","coordinates":[1,2]}`,
		"no features":   `{"type":"FeatureCollection"}`,
		"not features":  `{"type":"FeatureCollection","features":{"a":1}}`,
		"bad item":      `{"type":"FeatureCollection","features":[{"type":"Point","coordinates":[1,2]}]}`,
		"empty":         `{"type":"FeatureCollection","features":[]}`,
		"truncated":     `{"type":"FeatureCollection","features":[{"type":"Feature"`,
	} {
		_, err := e.svc.Upload(e.ctx, e.ws, "u1", UploadOptions{Filename: "x.geojson"}, strings.NewReader(body))
		if fcode(err) != "BAD_REQUEST" && fcode(err) != "VALIDATION_FAILED" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestInferGeoText(t *testing.T) {
	pt, area := `{"type":"Point","coordinates":[1,2]}`, `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}`
	for name, tc := range map[string]struct {
		in   []string
		want catalog.FieldType
	}{
		"points":     {[]string{pt, "", pt}, catalog.GeoPoint},
		"areas":      {[]string{area}, catalog.GeoArea},
		"mixed":      {[]string{pt, area}, catalog.ShortText},
		"other json": {[]string{`{"a":1}`}, catalog.ShortText},
		"line":       {[]string{`{"type":"LineString","coordinates":[]}`}, catalog.ShortText},
		"bad json":   {[]string{`{"type":"Point"`}, catalog.ShortText},
	} {
		got, _ := inferType(tc.in)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

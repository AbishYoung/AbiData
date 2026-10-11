package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Geo fields over HTTP: schema, shorthand inputs, GeoJSON responses, filters,
// validation errors, a saved SQL query with an area parameter and a visual
// query with a spatial join, all through the JSON layer.
func TestGeoFieldsOverHTTP(t *testing.T) {
	e, root, ws := setupWorkspace(t)
	base := "/api/v1/w/" + ws
	ops := map[string]any{"ops": []map[string]any{
		{"op": "create_table", "table": map[string]any{"display_name": "Sightings", "columns": []map[string]any{
			{"display_name": "Species", "field_type": "short_text"},
			{"display_name": "Spot", "field_type": "geo_point"},
		}}},
		{"op": "create_table", "table": map[string]any{"display_name": "Zones", "columns": []map[string]any{
			{"display_name": "Zone", "field_type": "short_text"},
			{"display_name": "Fence", "field_type": "geo_area"},
			{"display_name": "Size", "field_type": "calculated", "type_options": map[string]any{"expression": "geo_area_m2(Fence)", "result_type": "number"}},
		}}},
	}}
	st, res, _ := root.do("POST", base+"/schema/apply", ops)
	if st != 200 {
		t.Fatalf("apply %d %v", st, res)
	}
	tables := res["schema"].(map[string]any)["tables"].([]any)
	var sid, zid string
	col := map[string]string{}
	for _, tb := range tables {
		m := tb.(map[string]any)
		switch m["display_name"] {
		case "Sightings":
			sid = m["id"].(string)
		case "Zones":
			zid = m["id"].(string)
		}
		for _, c := range m["columns"].([]any) {
			cm := c.(map[string]any)
			col[cm["display_name"].(string)] = cm["id"].(string)
		}
	}
	ed := e.login("ed@example.com")
	srows, zrows := base+"/tables/"+sid+"/rows", base+"/tables/"+zid+"/rows"

	// Shorthands in, GeoJSON out.
	for _, spot := range []any{
		"51.50135, -0.14189",
		map[string]any{"lat": 51.50135, "lon": -0.14189},
		map[string]any{"type": "Feature", "properties": map[string]any{}, "geometry": map[string]any{"type": "Point", "coordinates": []any{-0.14189, 51.50135}}},
	} {
		st, row, _ := ed.do("POST", srows, map[string]any{"fields": map[string]any{col["Species"]: "x", col["Spot"]: spot}})
		if st != 201 {
			t.Fatalf("insert %v: %d %v", spot, st, row)
		}
		b, _ := json.Marshal(row["fields"].(map[string]any)[col["Spot"]])
		if string(b) != `{"coordinates":[-0.14189,51.50135],"type":"Point"}` {
			t.Fatalf("response value %s", b)
		}
	}
	ed.do("POST", srows, map[string]any{"fields": map[string]any{col["Species"]: "far", col["Spot"]: "10, 10"}})
	ed.do("POST", srows, map[string]any{"fields": map[string]any{col["Species"]: "none"}})

	// Area with a calculated size.
	fence := map[string]any{"type": "Polygon", "coordinates": []any{[]any{[]any{-1, 51}, []any{-1, 52}, []any{1, 52}, []any{1, 51}}}}
	st, zr, _ := ed.do("POST", zrows, map[string]any{"fields": map[string]any{col["Zone"]: "London", col["Fence"]: fence}})
	if st != 201 {
		t.Fatalf("zone %d %v", st, zr)
	}
	f := zr["fields"].(map[string]any)
	if size, _ := f[col["Size"]].(float64); size < 1.5e10 || size > 1.6e10 {
		t.Fatalf("calculated size %v", f[col["Size"]])
	}
	ring := f[col["Fence"]].(map[string]any)["coordinates"].([]any)[0].([]any)
	if len(ring) != 5 {
		t.Fatalf("ring not closed: %v", ring)
	}

	// Errors name the field.
	for name, body := range map[string]any{"latitude": "95, 0", "Spot": "Paris", "Fence": nil} {
		fields := map[string]any{col["Spot"]: body}
		table := srows
		if name == "Fence" {
			fields = map[string]any{col["Fence"]: map[string]any{"type": "Point", "coordinates": []any{1, 2}}}
			table = zrows
		}
		st, b, _ := ed.do("POST", table, map[string]any{"fields": fields})
		msg, _ := b["error"].(map[string]any)["message"].(string)
		if st != 422 || errCode(b) != "VALIDATION_FAILED" || !strings.Contains(msg, name) {
			t.Fatalf("%s: %d %v", name, st, b)
		}
	}

	// Filters.
	count := func(filter string) float64 {
		t.Helper()
		st, cnt, _ := ed.do("GET", srows+"/count?filter="+urlEscape(filter), nil)
		if st != 200 {
			t.Fatalf("%s: %d %v", filter, st, cnt)
		}
		return cnt["count"].(float64)
	}
	within := fmt.Sprintf(`{"col":%q,"op":"within","value":{"type":"Polygon","coordinates":[[[-1,51],[1,51],[1,52],[-1,52],[-1,51]]]}}`, col["Spot"])
	near := fmt.Sprintf(`{"col":%q,"op":"near","value":{"lat":51.5,"lon":-0.14,"metres":1000}}`, col["Spot"])
	if n := count(within); n != 3 {
		t.Fatalf("within = %v", n)
	}
	if n := count(near); n != 3 {
		t.Fatalf("near = %v", n)
	}
	if n := count(fmt.Sprintf(`{"col":%q,"op":"near","value":[10,10,10]}`, col["Spot"])); n != 1 {
		t.Fatalf("near list = %v", n)
	}
	if n := count(fmt.Sprintf(`{"col":%q,"op":"is_null"}`, col["Spot"])); n != 1 {
		t.Fatalf("is_null = %v", n)
	}
	st, b, _ := ed.do("GET", srows+"/count?filter="+urlEscape(fmt.Sprintf(`{"col":%q,"op":"contains","value":"1"}`, col["Spot"])), nil)
	if st != 400 {
		t.Fatalf("contains on a location: %d %v", st, b)
	}

	// A saved SQL query with an area parameter, run with a JSON object.
	st, q, _ := root.do("POST", base+"/queries", map[string]any{"name": "In fence", "kind": "sql",
		"sql_text":   `SELECT Species, geo_distance_m(Spot, geo_point(51.5, -0.14)) < 1000 AS close FROM Sightings WHERE geo_within(Spot, :fence) ORDER BY id`,
		"parameters": []map[string]string{{"name": "fence", "type": "area"}}})
	if st != 201 && st != 200 {
		t.Fatalf("save query %d %v", st, q)
	}
	st, out, _ := ed.do("POST", base+"/queries/"+q["id"].(string)+"/run", map[string]any{"params": map[string]any{"fence": fence}})
	if st != 200 || len(out["rows"].([]any)) != 3 {
		t.Fatalf("run %d %v", st, out)
	}
	if st, b, _ := ed.do("POST", base+"/queries/"+q["id"].(string)+"/run", map[string]any{"params": map[string]any{"fence": "somewhere"}}); st != 400 {
		t.Fatalf("bad area param: %d %v", st, b)
	}

	// Visual spatial join; column_types carry the geo types.
	model := map[string]any{
		"sources": []map[string]any{{"alias": "s", "table_id": sid, "x": 0, "y": 0}, {"alias": "z", "table_id": zid, "x": 0, "y": 0}},
		"joins": []map[string]any{{"left_alias": "s", "left_column_id": col["Spot"], "right_alias": "z", "right_column_id": col["Fence"],
			"kind": "inner", "op": "within"}},
		"fields": []map[string]any{{"alias": "s", "column_id": col["Species"]}, {"alias": "z", "column_id": col["Zone"]},
			{"alias": "s", "column_id": col["Spot"]}, {"alias": "z", "column_id": col["Fence"]}},
	}
	st, out, _ = root.do("POST", base+"/queries/run", map[string]any{"model": model})
	if st != 200 || len(out["rows"].([]any)) != 3 {
		t.Fatalf("spatial join %d %v", st, out)
	}
	types := fmt.Sprint(out["column_types"])
	if types != "[text text geo_point geo_area]" {
		t.Fatalf("column_types %s", types)
	}
}

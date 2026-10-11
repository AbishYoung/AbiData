package records

import (
	"encoding/json"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/sqlitex"
)

const zoneJSON = `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]],[[4,4],[6,4],[6,6],[4,6],[4,4]]]}`

type geoEnv struct {
	*env
	tbl                  *catalog.Table
	name, loc, zone, ctr string
}

func newGeoEnv(t *testing.T) *geoEnv {
	t.Helper()
	e := newEnv(t)
	eng := &catalog.Engine{}
	if _, err := eng.Run(e.ctx, e.l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sightings", TrackHistory: true,
		Columns: []catalog.ColumnSpec{
			{DisplayName: "Name", Type: catalog.ShortText},
			{DisplayName: "Location", Type: catalog.GeoPoint},
			{DisplayName: "Zone", Type: catalog.GeoArea},
			{DisplayName: "Zone centre", Type: catalog.Calculated, Options: catalog.TypeOptions{Expression: "geo_centroid(Zone)", ResultType: "geo_point"}},
		}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, err := e.svc.Schemas.Get(e.ctx, e.l)
	if err != nil {
		t.Fatal(err)
	}
	tbl, _ := sch.TableByPhysical("Sightings")
	return &geoEnv{env: e, tbl: tbl, name: e.col(tbl, "Name"), loc: e.col(tbl, "Location"), zone: e.col(tbl, "Zone"), ctr: e.col(tbl, "Zone_centre")}
}

func (g *geoEnv) add(name string, loc any) Row {
	g.t.Helper()
	r, err := g.svc.Insert(g.ctx, g.l, g.tbl.ID, map[string]any{g.name: name, g.loc: loc}, Actor{UserID: "u1"}, nil)
	if err != nil {
		g.t.Fatal(err)
	}
	return r
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGeoFieldRoundTripAndShorthands(t *testing.T) {
	g := newGeoEnv(t)
	want := `{"type":"Point","coordinates":[-0.14189,51.50135]}`
	for i, in := range []any{
		"51.50135, -0.14189",
		map[string]any{"lat": num("51.50135"), "lon": num("-0.14189")},
		map[string]any{"lat": 51.50135, "lon": -0.14189},
		map[string]any{"type": "Point", "coordinates": []any{num("-0.14189"), num("51.501350000001")}},
		map[string]any{"type": "Feature", "properties": map[string]any{}, "geometry": map[string]any{"type": "Point", "coordinates": []any{-0.14189, 51.50135}}},
	} {
		r := g.add("s", in)
		got, err := g.svc.Get(g.ctx, g.l, g.tbl.ID, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s := marshal(t, got.Fields[g.loc]); s != want {
			t.Errorf("case %d: stored %s, want %s", i, s, want)
		}
	}
}

func TestGeoFieldAreaAndCalculated(t *testing.T) {
	g := newGeoEnv(t)
	var area map[string]any
	if err := json.Unmarshal([]byte(`{"type":"Polygon","coordinates":[[[0,0],[0,2],[2,2],[2,0]]]}`), &area); err != nil {
		t.Fatal(err)
	}
	r, err := g.svc.Insert(g.ctx, g.l, g.tbl.ID, map[string]any{g.zone: area}, Actor{UserID: "u"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := g.svc.Get(g.ctx, g.l, g.tbl.ID, r.ID)
	if s := marshal(t, got.Fields[g.zone]); s != `{"type":"Polygon","coordinates":[[[0,0],[2,0],[2,2],[0,2],[0,0]]]}` {
		t.Errorf("zone = %s", s)
	}
	if s := marshal(t, got.Fields[g.ctr]); s != `{"type":"Point","coordinates":[1,1]}` {
		t.Errorf("centre = %s", s)
	}
	// History stores the geometry as an object, not an escaped string.
	var after string
	if err := g.l.Read(g.ctx, func(q sqlitex.Querier) error {
		return q.QueryRow(g.ctx, `SELECT after FROM _sys_row_history WHERE row_id = ?`, r.ID).Scan(&after)
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, `"type":"Polygon"`) || strings.Contains(after, `\"type\"`) {
		t.Errorf("history after = %s", after)
	}
	// Clearing a geo value stores NULL.
	if _, err := g.svc.Update(g.ctx, g.l, g.tbl.ID, r.ID, got.Version, map[string]any{g.zone: nil}, Actor{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGeoValidationErrors(t *testing.T) {
	g := newGeoEnv(t)
	ring := func(n int) any {
		pts := make([]any, 0, n+1)
		for i := 0; i < n; i++ {
			pts = append(pts, []any{float64(i) * 0.001, float64(i%2) * 0.001})
		}
		return map[string]any{"type": "Polygon", "coordinates": []any{pts}}
	}
	for _, tc := range []struct {
		col  string
		val  any
		want string
	}{
		{g.loc, "91, 0", "latitude"},
		{g.loc, "0, 181", "longitude"},
		{g.loc, "somewhere", "Location"},
		{g.loc, num("5"), "Location"},
		{g.loc, map[string]any{"type": "Polygon", "coordinates": []any{}}, "Point"},
		{g.zone, "1, 2", "Zone"},
		{g.zone, map[string]any{"type": "Point", "coordinates": []any{1, 2}}, "Polygon or MultiPolygon"},
		{g.zone, map[string]any{"type": "Polygon", "coordinates": []any{[]any{[]any{0, 0}, []any{1, 1}}}}, "4 positions"},
		{g.zone, map[string]any{"type": "Polygon", "coordinates": []any{[]any{[]any{170, 0}, []any{-170, 0}, []any{-170, 5}, []any{170, 5}}}}, "180"},
		{g.zone, ring(10001), "10000"},
	} {
		_, err := g.svc.Insert(g.ctx, g.l, g.tbl.ID, map[string]any{tc.col: tc.val}, Actor{}, nil)
		fe := fault.Classify(err)
		if err == nil || fe.Code != fault.ValidationFailed || !strings.Contains(fe.Message, tc.want) {
			t.Errorf("%v: err = %v (%s), want VALIDATION_FAILED containing %q", tc.val, err, fe.Message, tc.want)
		}
	}
	// Calculated geo fields cannot be set.
	if _, err := g.svc.Insert(g.ctx, g.l, g.tbl.ID, map[string]any{g.ctr: "1, 2"}, Actor{}, nil); err == nil {
		t.Error("calculated field accepted a value")
	}
}

func TestGeoFilters(t *testing.T) {
	g := newGeoEnv(t)
	g.add("in", "2, 2")
	g.add("hole", "5, 5")
	g.add("edge", "0, 5")
	g.add("out", "20, 20")
	if _, err := g.svc.Insert(g.ctx, g.l, g.tbl.ID, map[string]any{g.name: "none"}, Actor{}, nil); err != nil {
		t.Fatal(err)
	}
	names := func(f Filter) string {
		t.Helper()
		rows, _, err := g.svc.List(g.ctx, g.l, g.tbl.ID, ListQuery{Filter: &f, Sort: []Sort{{Col: g.name}}, Limit: 50})
		if err != nil {
			t.Fatalf("%+v: %v", f, err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Fields[g.name].(string))
		}
		return strings.Join(out, ",")
	}
	var zone map[string]any
	_ = json.Unmarshal([]byte(zoneJSON), &zone)
	if got := names(Filter{Col: g.loc, Op: OpWithin, Value: zone}); got != "edge,in" {
		t.Errorf("within = %q", got)
	}
	if got := names(Filter{Col: g.loc, Op: OpWithin, Value: zoneJSON}); got != "edge,in" {
		t.Errorf("within text = %q", got)
	}
	// 1 degree of latitude is about 111 km.
	near := func(v any) string { return names(Filter{Col: g.loc, Op: OpNear, Value: v}) }
	if got := near(map[string]any{"lat": 2.0, "lon": 2.0, "metres": 500.0}); got != "in" {
		t.Errorf("near 500 m = %q", got)
	}
	if got := near([]any{2.5, 2.0, 300000.0}); got != "in" {
		t.Errorf("near 300 km = %q", got)
	}
	if got := near("2,2,450000"); got != "edge,in" {
		t.Errorf("near 450 km text = %q", got)
	}
	if got := names(Filter{Col: g.loc, Op: OpIsNull}); got != "none" {
		t.Errorf("is_null = %q", got)
	}
	if got := names(Filter{And: []Filter{{Col: g.loc, Op: OpNotNull}, {Col: g.loc, Op: OpNear, Value: "0,5,1"}}}); got != "edge" {
		t.Errorf("combined = %q", got)
	}
	// Errors.
	for _, f := range []Filter{
		{Col: g.loc, Op: OpWithin, Value: "nope"},
		{Col: g.loc, Op: OpWithin},
		{Col: g.loc, Op: OpNear, Value: "1,2"},
		{Col: g.loc, Op: OpNear, Value: []any{91.0, 0.0, 5.0}},
		{Col: g.loc, Op: OpNear, Value: []any{1.0, 2.0, -5.0}},
		{Col: g.loc, Op: OpNear, Value: map[string]any{"lat": 1.0, "lon": 2.0}},
		{Col: g.loc, Op: OpEq, Value: "1, 2"},
		{Col: g.loc, Op: OpContains, Value: "1"},
		{Col: g.zone, Op: OpWithin, Value: zoneJSON},
		{Col: g.name, Op: OpNear, Value: "1,2,3"},
	} {
		if _, _, err := g.svc.List(g.ctx, g.l, g.tbl.ID, ListQuery{Filter: &f, Limit: 5}); !isBadRequest(err) {
			t.Errorf("%+v: err = %v, want BAD_REQUEST", f, err)
		}
	}
}

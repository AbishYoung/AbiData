package geo_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	_ "abidata/internal/script/modules/db"
	_ "abidata/internal/script/modules/geo"
	_ "abidata/internal/script/modules/json"
	_ "abidata/internal/script/modules/records"
	"abidata/internal/workspace/wstest"
)

type fx struct {
	ctx context.Context
	rn  *script.Runner
	hd  *host.Data
	id  ids.WorkspaceID
}

func setup(t *testing.T) *fx {
	t.Helper()
	ctx := context.Background()
	w := wstest.New(t)
	m := w.Manager
	id := w.Create(t)
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Release)
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sightings",
		Columns: []catalog.ColumnSpec{
			{DisplayName: "Name", Type: catalog.ShortText},
			{DisplayName: "Spot", Type: catalog.GeoPoint},
			{DisplayName: "Zone", Type: catalog.GeoArea},
		}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	cache := catalog.NewCache()
	hd := &host.Data{Lease: l, Schemas: cache, Records: &records.Service{Schemas: cache}, CanWrite: true, UserID: "u1"}
	return &fx{ctx: ctx, rn: script.NewRunner(4, 2), hd: hd, id: l.ID()}
}

func (f *fx) run(t *testing.T, src string) []any {
	t.Helper()
	p, err := script.Compile(src, "t")
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.rn.Exec(f.ctx, script.ExecRequest{Kind: script.UIEvent, Workspace: f.id, Proto: p, Host: f.hd})
	if err != nil {
		t.Fatalf("%s\n=> %v", src, err)
	}
	return res.Values
}

func (f *fx) fails(t *testing.T, src, contains string) {
	t.Helper()
	p, err := script.Compile(src, "t")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.rn.Exec(f.ctx, script.ExecRequest{Kind: script.UIEvent, Workspace: f.id, Proto: p, Host: f.hd})
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("%s\n=> %v, want error containing %q", src, err, contains)
	}
}

func TestGeoModule(t *testing.T) {
	f := setup(t)
	v := f.run(t, `
		local p = geo.point(51.50135, -0.14189)
		local sq = {type = "Polygon", coordinates = {{{0,0},{10,0},{10,10},{0,10},{0,0}}, {{4,4},{6,4},{6,6},{4,6},{4,4}}}}
		return p.type, p.coordinates[1], p.coordinates[2], geo.lat(p), geo.lon("51.5, -0.1"),
			geo.within(geo.point(2, 2), sq), geo.within(geo.point(5, 5), sq), geo.within({lat = 0, lon = 5}, sq), geo.within("20, 20", sq),
			geo.distance(geo.point(51.5074, -0.1278), geo.point(48.8566, 2.3522)) > 343000,
			geo.distance(p, geo.point(0, 0)) == geo.distance(geo.point(0, 0), p),
			geo.area(sq) > 1.17e12, geo.centroid(sq).type, geo.centroid(sq).coordinates[1] > 4.9,
			geo.bbox(sq)[1], geo.bbox(sq)[2], geo.bbox(sq)[3], geo.bbox(sq)[4],
			#geo.point(1, 2, 300).coordinates`)
	want := []any{"Point", -0.14189, 51.50135, 51.50135, -0.1, true, false, true, false, true, true, true, "Point", true, int64(0), int64(0), int64(10), int64(10), int64(3)}
	if len(v) != len(want) {
		t.Fatalf("%#v", v)
	}
	for i := range want {
		if f, ok := want[i].(float64); ok {
			if g, ok := v[i].(float64); !ok || math.Abs(g-f) > 1e-9 {
				t.Errorf("[%d] = %#v, want %v", i, v[i], f)
			}
			continue
		}
		if v[i] != want[i] {
			t.Errorf("[%d] = %#v, want %#v", i, v[i], want[i])
		}
	}
	// Geometry round-trips through json.
	if got := f.run(t, `return json.encode(geo.point(1.5, 2.25))`); got[0] != `{"coordinates":[2.25,1.5],"type":"Point"}` {
		t.Errorf("json: %v", got)
	}
}

func TestGeoModuleErrors(t *testing.T) {
	f := setup(t)
	for src, msg := range map[string]string{
		`geo.point(91, 0)`:                           "latitude",
		`geo.point(0, 181)`:                          "longitude",
		`geo.point("a", 0)`:                          "number expected",
		`geo.point(0/0, 0)`:                          "finite",
		`geo.distance("nowhere", geo.point(0,0))`:    "invalid point",
		`geo.distance(nil, geo.point(0,0))`:          "point is required",
		`geo.within(geo.point(0,0), {})`:             "invalid area",
		`geo.within(geo.point(0,0), geo.point(1,1))`: "Polygon or MultiPolygon",
		`geo.area("x")`:                              "invalid area",
		`geo.bbox()`:                                 "area is required",
		`geo.centroid({type = "Polygon", coordinates = {{{0,0},{1,1},{0,0}}}})`:                                       "4 positions",
		`geo.within(geo.point(0,0), {type = "Polygon", coordinates = {{{170,0},{-170,0},{-170,5},{170,5},{170,0}}}})`: "180",
	} {
		f.fails(t, src, msg)
	}
}

func TestGeoRecordsRoundTrip(t *testing.T) {
	f := setup(t)
	v := f.run(t, `
		local id = records.insert("Sightings", {Name = "a", Spot = geo.point(51.5, -0.1),
			Zone = {type = "Polygon", coordinates = {{{0,0},{0,2},{2,2},{2,0}}}}})
		local r = records.get("Sightings", id)
		local z = records.get("Sightings", id).Zone
		records.update("Sightings", id, {Spot = "10, 20"})
		local r2 = records.get("Sightings", id)
		local id2 = records.insert("Sightings", {Name = "b"})
		return r.Spot.type, r.Spot.coordinates[1], r.Spot.coordinates[2], z.type, #z.coordinates[1], z.coordinates[1][2][1],
			geo.within(r.Spot, z), geo.area(r.Zone) > 0, r2.Spot.coordinates[1], r2.Spot.coordinates[2],
			records.get("Sightings", id2).Spot == db.NULL`)
	want := []any{"Point", -0.1, 51.5, "Polygon", int64(5), int64(2), false, true, int64(20), int64(10), true}
	for i := range want {
		if v[i] != want[i] {
			t.Errorf("[%d] = %#v, want %#v", i, v[i], want[i])
		}
	}
	// An invalid location is rejected by the field's own validation.
	f.fails(t, `records.insert("Sightings", {Spot = {lat = 100, lon = 0}})`, "latitude")
	f.fails(t, `records.insert("Sightings", {Zone = geo.point(1, 1)})`, "Zone")
	// db.query returns the stored GeoJSON text, which geo functions accept.
	v = f.run(t, `
		records.insert("Sightings", {Name = "c", Spot = geo.point(1, 2)})
		local row = db.query_one([[SELECT "Spot" FROM "Sightings" WHERE "Name" = 'c']])
		return type(row.Spot), geo.lat(row.Spot), geo.lon(row.Spot)`)
	if v[0] != "string" || v[1] != int64(1) || v[2] != int64(2) {
		t.Errorf("%#v", v)
	}
}

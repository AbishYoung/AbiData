package reports

import (
	"encoding/json"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/forms"
	"abidata/internal/workspace"
)

func TestAreaText(t *testing.T) {
	for m2, want := range map[float64]string{
		0:         "0 m²",
		850.4:     "850 m²",
		12345.6:   "12,346 m²",
		99_999:    "99,999 m²",
		3_400_000: "3.4 km²",
		160_000:   "0.2 km²",
		12.34e7:   "123 km²",
		1.5e10:    "15,000 km²",
	} {
		if got := areaText(m2); got != want {
			t.Errorf("areaText(%v) = %q, want %q", m2, got, want)
		}
	}
	if got := pointText(-0.000001, 51.501349); got != "0.00000, 51.50135" {
		t.Errorf("pointText: %q", got)
	}
}

func TestGeoInReports(t *testing.T) {
	f := setup(t)
	if _, err := (&catalog.Engine{}).Run(f.ctx, f.l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sightings", Columns: []catalog.ColumnSpec{
		{DisplayName: "Species", Type: catalog.ShortText}, {DisplayName: "Location", Type: catalog.GeoPoint}, {DisplayName: "Zone", Type: catalog.GeoArea},
	}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, _ := f.svc.Schemas.Get(f.ctx, f.l)
	tbl, _ := sch.TableByPhysical("Sightings")
	// A 0.01° square at the equator: about 1.236 km² on the sphere.
	if err := f.l.Write(f.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(f.ctx, `INSERT INTO Sightings(Species, Location, Zone) VALUES ('Knotweed', '{"type":"Point","coordinates":[-0.1418923,51.5013512]}',
			'{"type":"Polygon","coordinates":[[[0,0],[0.01,0],[0.01,0.01],[0,0.01],[0,0]]]}')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lay := ReportLayout{Width: 600, Bands: Bands{Detail: Band{Height: 220, Controls: []ReportControl{
		{ID: "pt", Name: "Where", Type: "textbox", W: 200, H: 20, Bind: &Bind{ColumnID: col(tbl, "Location")}},
		{ID: "ar", Name: "ZoneText", Type: "textbox", Y: 20, W: 300, H: 20, Bind: &Bind{ColumnID: col(tbl, "Zone")}},
		{ID: "mp", Name: "ZoneMap", Type: "map", Y: 40, W: 300, H: 160, Bind: &Bind{ColumnID: col(tbl, "Zone")},
			Map: &forms.MapOptions{OverlayQueryID: "q", OverlayGeoColumn: "Zone"}},
	}}}}
	ps := PageSetup{Paper: "a4", Orientation: "portrait"}
	rep, err := f.svc.Put(f.ctx, f.l, "", 0, ReportInput{Name: "Sightings", SourceKind: "table", SourceID: tbl.ID, ReportLayout: lay, PageSetup: ps}, "u")
	if err != nil {
		t.Fatal(err)
	}
	sch, _ = f.svc.Schemas.Get(f.ctx, f.l)
	var detail map[string]any
	if err := f.svc.Run(f.ctx, RunInput{Lease: f.l, Schema: sch, Report: rep, Caller: Caller{TZ: time.UTC},
		Emit: func(i Instance) error {
			if i.Band == "detail" {
				detail = i.Values
			}
			return nil
		}}); err != nil {
		t.Fatal(err)
	}
	if detail["pt"] != "51.50135, -0.14189" {
		t.Errorf("point text: %v", detail["pt"])
	}
	if detail["ar"] != "Area · 4 points · 1.2 km²" {
		t.Errorf("area text: %v", detail["ar"])
	}
	b, _ := json.Marshal(detail["mp"])
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m["type"] != "Polygon" {
		t.Errorf("map value is not the geometry: %s", b)
	}

	// A map must be bound to a geo field, and only maps carry map settings.
	for name, c := range map[string]ReportControl{
		"text-bound map": {ID: "x", Name: "X", Type: "map", W: 10, H: 10, Bind: &Bind{ColumnID: col(tbl, "Species")}},
		"unbound map":    {ID: "x", Name: "X", Type: "map", W: 10, H: 10},
		"textbox map":    {ID: "x", Name: "X", Type: "textbox", W: 10, H: 10, Map: &forms.MapOptions{}},
	} {
		bad := ReportLayout{Width: 100, Bands: Bands{Detail: Band{Height: 20, Controls: []ReportControl{c}}}}
		if _, err := f.svc.Put(f.ctx, f.l, "", 0, ReportInput{Name: "Bad " + name, SourceKind: "table", SourceID: tbl.ID, ReportLayout: bad, PageSetup: ps}, "u"); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestGeoTextFromQueryText(t *testing.T) {
	if s, ok := geoText(`{"type":"Point","coordinates":[2.5,-1.25]}`, ""); !ok || s != "-1.25000, 2.50000" {
		t.Errorf("query point: %q %v", s, ok)
	}
	if _, ok := geoText(`{"type":"Feature"}`, ""); ok {
		t.Error("non-geometry JSON treated as geo")
	}
	if _, ok := geoText("plain text", ""); ok {
		t.Error("plain text treated as geo")
	}
	if s, ok := geoText("not json", catalog.GeoPoint); !ok || s != "not json" {
		t.Errorf("invalid geo column text: %q %v", s, ok)
	}
}

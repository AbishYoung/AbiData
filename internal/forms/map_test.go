package forms

import (
	"fmt"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
)

func TestMapControlValidation(t *testing.T) {
	f := setup(t)
	e := &catalog.Engine{}
	if _, err := e.Run(f.ctx, f.l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sightings", Columns: []catalog.ColumnSpec{
		{DisplayName: "Species", Type: catalog.ShortText},
		{DisplayName: "Location", Type: catalog.GeoPoint},
		{DisplayName: "Zone", Type: catalog.GeoArea}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, err := f.svc.Schemas.Get(f.ctx, f.l)
	if err != nil {
		t.Fatal(err)
	}
	tbl, _ := sch.TableByPhysical("Sightings")
	n := 0
	put := func(c Control) error {
		n++
		c.ID, c.Name, c.W, c.H = "m1", "Map1", 300, 200
		_, err := f.svc.Put(f.ctx, f.l, "", 0, FormInput{Name: fmt.Sprintf("Sighting %d", n), SourceKind: "table", SourceID: tbl.ID,
			Layout: Layout{Width: 400, Sections: Sections{Detail: Section{Height: 240, Controls: []Control{c}}}}}, "bu")
		return err
	}
	for name, c := range map[string]Control{
		"point":   {Type: "map", Bind: &Bind{ColumnID: col(tbl, "Location")}},
		"area":    {Type: "map", Bind: &Bind{ColumnID: col(tbl, "Zone")}},
		"overlay": {Type: "map", Bind: &Bind{ColumnID: col(tbl, "Location")}, Map: &MapOptions{OverlayQueryID: "q1", OverlayGeoColumn: "Zone", OverlayLabelColumn: "Name"}},
	} {
		if err := put(c); err != nil {
			t.Errorf("%s map rejected: %v", name, err)
		}
	}
	for name, c := range map[string]Control{
		"unbound":       {Type: "map"},
		"text field":    {Type: "map", Bind: &Bind{ColumnID: col(tbl, "Species")}},
		"no geo column": {Type: "map", Bind: &Bind{ColumnID: col(tbl, "Location")}, Map: &MapOptions{OverlayQueryID: "q1"}},
		"columns only":  {Type: "map", Bind: &Bind{ColumnID: col(tbl, "Location")}, Map: &MapOptions{OverlayGeoColumn: "Zone"}},
		"textbox map":   {Type: "textbox", Bind: &Bind{ColumnID: col(tbl, "Species")}, Map: &MapOptions{}},
	} {
		if err := put(c); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

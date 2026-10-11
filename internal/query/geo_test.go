package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

const (
	geoSightings = "01J00000000000000000000G01"
	geoSName     = "01J00000000000000000000G02"
	geoSWhere    = "01J00000000000000000000G03"
	geoZones     = "01J00000000000000000000H01"
	geoZName     = "01J00000000000000000000H02"
	geoZArea     = "01J00000000000000000000H03"
)

const quarantine = `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]]]}`

func geoFixture(t *testing.T) (*fx, *catalog.Schema) {
	t.Helper()
	f := setup(t)
	ctx := context.Background()
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, f.l, []catalog.Op{
		{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{ID: geoSightings, DisplayName: "Sightings", Columns: []catalog.ColumnSpec{
			{ID: geoSName, DisplayName: "Species", Type: catalog.ShortText},
			{ID: geoSWhere, DisplayName: "Spot", Type: catalog.GeoPoint}}}},
		{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{ID: geoZones, DisplayName: "Zones", Columns: []catalog.ColumnSpec{
			{ID: geoZName, DisplayName: "Zone", Type: catalog.ShortText},
			{ID: geoZArea, DisplayName: "Fence", Type: catalog.GeoArea}}}},
	}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO Sightings(Species, Spot) VALUES ('inside', geo_point(5,5)), ('outside', geo_point(20,20)), ('in east', geo_point(5,25)), ('nowhere', NULL)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO Zones(Zone, Fence) VALUES ('Quarantine', ?), ('East', ?)`, quarantine,
			`{"type":"Polygon","coordinates":[[[20,0],[30,0],[30,10],[20,10],[20,0]]]}`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sch, err := catalog.NewCache().Get(ctx, f.l)
	if err != nil {
		t.Fatal(err)
	}
	return f, sch
}

func TestSpatialJoin(t *testing.T) {
	f, sch := geoFixture(t)
	m := Model{
		Sources: []Source{{Alias: "s", TableID: geoSightings}, {Alias: "z", TableID: geoZones}},
		Joins:   []Join{{LeftAlias: "s", LeftColumnID: geoSWhere, RightAlias: "z", RightColumnID: geoZArea, Kind: "inner", Op: "within"}},
		Fields: []Field{
			{Alias: "s", ColumnID: geoSName, Sort: "asc"},
			{Alias: "z", ColumnID: geoZName},
			{Alias: "s", ColumnID: geoSWhere},
			{Alias: "z", ColumnID: geoZArea},
		},
	}
	c, err := Compile(sch, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.SQL, `ON geo_within("s"."Spot", "z"."Fence")`) {
		t.Fatalf("sql: %s", c.SQL)
	}
	if got := strings.Join(c.Types, ","); got != "text,text,geo_point,geo_area" {
		t.Fatalf("types: %s", got)
	}
	r, err := RunTyped(f.ctx, f.l, Policy{}, sch, c.Types, c.SQL, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, row := range r.Rows {
		names = append(names, fmt.Sprint(row[0], "/", row[1]))
	}
	if strings.Join(names, ",") != "in east/East,inside/Quarantine" {
		t.Fatalf("rows: %v", names)
	}
	if strings.Join(r.Types, ",") != "text,text,geo_point,geo_area" {
		t.Fatalf("column_types: %v", r.Types)
	}

	// Left spatial join keeps sightings that fall in no zone.
	m.Joins[0].Kind = "left"
	c, err = Compile(sch, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err = Run(f.ctx, f.l, Policy{}, c.SQL, nil, 0)
	if err != nil || len(r.Rows) != 4 {
		t.Fatalf("left join: %+v %v", r, err)
	}

	// Columns may be given in either order.
	m.Joins[0] = Join{LeftAlias: "z", LeftColumnID: geoZArea, RightAlias: "s", RightColumnID: geoSWhere, Kind: "inner", Op: "within"}
	c, err = Compile(sch, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.SQL, `geo_within("s"."Spot", "z"."Fence")`) {
		t.Fatalf("swapped: %s", c.SQL)
	}

	// Wrong column types and unknown ops are refused.
	for _, j := range []Join{
		{LeftAlias: "s", LeftColumnID: geoSName, RightAlias: "z", RightColumnID: geoZArea, Op: "within"},
		{LeftAlias: "s", LeftColumnID: geoSWhere, RightAlias: "z", RightColumnID: geoZName, Op: "within"},
		{LeftAlias: "s", LeftColumnID: geoSWhere, RightAlias: "z", RightColumnID: "id", Op: "within"},
		{LeftAlias: "s", LeftColumnID: geoSWhere, RightAlias: "z", RightColumnID: geoZArea, Op: "near"},
	} {
		m.Joins[0] = j
		if _, err := Compile(sch, m, nil); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%+v compiled: %v", j, err)
		}
	}
}

func TestWithinCriterion(t *testing.T) {
	f, sch := geoFixture(t)
	run := func(m Model, params Params) (Result, string) {
		t.Helper()
		c, err := Compile(sch, m, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckParams(m.Parameters, params); err != nil {
			t.Fatal(err)
		}
		r, err := RunTyped(f.ctx, f.l, Policy{}, sch, c.Types, c.SQL, params, 0)
		if err != nil {
			t.Fatalf("%s: %v", c.SQL, err)
		}
		return r, c.SQL
	}
	base := func(crit Criterion) Model {
		return Model{Sources: []Source{{Alias: "s", TableID: geoSightings}}, Fields: []Field{
			{Alias: "s", ColumnID: geoSName, Sort: "asc"},
			{Alias: "s", ColumnID: geoSWhere, Criteria: []Criterion{crit}},
		}}
	}
	// Area parameter, as a JSON object (the API form) and as text.
	m := base(Criterion{Op: "within", Param: "fence"})
	m.Parameters = []Param{{Name: "fence", Type: "area"}}
	var obj map[string]any
	_ = json.Unmarshal([]byte(quarantine), &obj)
	for _, v := range []any{obj, quarantine} {
		r, sql := run(m, Params{"fence": v})
		if len(r.Rows) != 1 || r.Rows[0][0] != "inside" || !strings.Contains(sql, `geo_within("s"."Spot", :fence)`) {
			t.Fatalf("param %T: %v %s", v, r.Rows, sql)
		}
	}
	// GeoJSON literal.
	r, _ := run(base(Criterion{Op: "within", Value: obj}), nil)
	if len(r.Rows) != 1 || r.Rows[0][0] != "inside" {
		t.Fatalf("literal: %v", r.Rows)
	}
	// An area field of a joined source (a cross join filtered by the criterion).
	m2 := Model{
		Sources: []Source{{Alias: "s", TableID: geoSightings}, {Alias: "z", TableID: geoZones}},
		Fields: []Field{
			{Alias: "s", ColumnID: geoSName, Sort: "asc"},
			{Alias: "z", ColumnID: geoZName, Criteria: []Criterion{{Op: "eq", Value: "East"}}},
			{Alias: "s", ColumnID: geoSWhere, Criteria: []Criterion{{Op: "within", RefAlias: "z", RefColumnID: geoZArea}}},
		},
	}
	r, _ = run(m2, nil)
	if len(r.Rows) != 1 || r.Rows[0][0] != "in east" {
		t.Fatalf("ref: %v", r.Rows)
	}
	c, _ := Compile(sch, m2, nil)
	if len(c.Deps.Columns) != 4 {
		t.Fatalf("deps: %+v", c.Deps)
	}

	// Errors.
	for name, crit := range map[string]Criterion{
		"no operand":      {Op: "within"},
		"two operands":    {Op: "within", Param: "fence", Value: obj},
		"undeclared":      {Op: "within", Param: "nope"},
		"bad literal":     {Op: "within", Value: "not an area"},
		"point as area":   {Op: "within", Value: map[string]any{"type": "Point", "coordinates": []any{1, 2}}},
		"half a ref":      {Op: "within", RefAlias: "s"},
		"unknown ref src": {Op: "within", RefAlias: "q", RefColumnID: geoZArea},
		"ref not area":    {Op: "within", RefAlias: "s", RefColumnID: geoSName},
	} {
		mm := base(crit)
		mm.Parameters = []Param{{Name: "fence", Type: "area"}}
		if _, err := Compile(sch, mm, nil); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%s compiled: %v", name, err)
		}
	}
	// within on a non-location field, and an area parameter used by a text parameter.
	mm := Model{Sources: []Source{{Alias: "s", TableID: geoSightings}}, Fields: []Field{
		{Alias: "s", ColumnID: geoSName, Criteria: []Criterion{{Op: "within", Value: obj}}}}}
	if _, err := Compile(sch, mm, nil); fault.Classify(err).Code != fault.BadRequest {
		t.Errorf("within on text compiled: %v", err)
	}
	mm = base(Criterion{Op: "within", Param: "fence"})
	mm.Parameters = []Param{{Name: "fence", Type: "text"}}
	if _, err := Compile(sch, mm, nil); fault.Classify(err).Code != fault.BadRequest {
		t.Errorf("text parameter accepted: %v", err)
	}
}

func TestCheckAreaParams(t *testing.T) {
	declared := []Param{{Name: "fence", Type: "area"}}
	var obj map[string]any
	_ = json.Unmarshal([]byte(quarantine), &obj)
	for _, ok := range []any{quarantine, obj, nil} {
		if err := CheckParams(declared, Params{"fence": ok}); err != nil {
			t.Errorf("%v rejected: %v", ok, err)
		}
	}
	for _, bad := range []any{"somewhere", json.Number("5"), true, map[string]any{"type": "Point", "coordinates": []any{1, 2}}, `{"type":"Polygon","coordinates":[[[0,0],[1,1]]]}`} {
		err := CheckParams(declared, Params{"fence": bad})
		if fault.Classify(err).Code != fault.BadRequest || !strings.Contains(err.Error(), "fence") {
			t.Errorf("%v accepted: %v", bad, err)
		}
	}
}

func TestSavedSQLQueryUsesGeoFunctions(t *testing.T) {
	f, sch := geoFixture(t)
	svc := &Service{Schemas: catalog.NewCache()}
	saved, err := svc.Put(f.ctx, f.l, "", 0, SavedInput{Name: "Sightings in zone", Kind: "sql",
		SQL: `SELECT Species, geo_lat(Spot) AS lat, round(geo_distance_m(Spot, geo_point(5, 6)) / 1000.0) AS km,
			geo_area_m2(:fence) > 1e12 AS big, geo_bbox(:fence) AS box, geo_centroid(:fence) AS centre
			FROM Sightings WHERE geo_within(Spot, :fence) ORDER BY Species`,
		Parameters: []Param{{Name: "fence", Type: "area"}}}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	_ = json.Unmarshal([]byte(quarantine), &obj)
	r, err := svc.RunSaved(f.ctx, f.l, Policy{}, saved.ID, Params{"fence": obj}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 || r.Rows[0][0] != "inside" || r.Rows[0][1] != 5.0 || r.Rows[0][2] != 111.0 || r.Rows[0][3] != int64(1) || r.Rows[0][4] != "[0,0,10,10]" {
		t.Fatalf("rows: %v", r.Rows)
	}
	if got := strings.Join(r.Types, ","); got != "text,number,number,integer,text,geo_point" {
		// lat is a function result (unknown type, sampled); centre is sampled GeoJSON.
		t.Fatalf("types: %s", got)
	}
	if _, err := svc.RunSaved(f.ctx, f.l, Policy{}, saved.ID, Params{"fence": "nope"}, 0); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("bad area: %v", err)
	}
	_ = sch
	// A query selecting geo columns directly is typed from the catalog.
	r, err = Run(f.ctx, f.l, Policy{}, `SELECT Spot, Fence FROM Sightings, Zones WHERE Zone = 'East' AND Spot IS NOT NULL LIMIT 1`, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := (&Service{Schemas: catalog.NewCache()}).Run(f.ctx, f.l, Policy{}, `SELECT Spot, Fence FROM Sightings, Zones LIMIT 1`, nil, 0)
	if err != nil || strings.Join(r2.Types, ",") != "geo_point,geo_area" {
		t.Fatalf("types from catalog: %+v %v", r2.Types, err)
	}
	_ = r
}

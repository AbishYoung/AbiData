package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/forms"
	"abidata/internal/geo"
	"abidata/internal/query"
	"abidata/internal/reports"
	"abidata/internal/workspace"
)

// The biosecurity part of the sample workspace: Northwind's supplier farms run
// an invasive species programme. It shows geographic fields (ADR 0010: points,
// areas with a hole, the spatial join, geo_* SQL functions, map controls and
// overlays) and file attachments (ADR 0011: photos stored in the workspace
// bucket, the attachments control and the report image control).

// Sighting and zone status values, shared by the data, the status pick lists
// and the queries.
var (
	sightingStatuses = []string{"Reported", "Confirmed", "Treated", "Cleared"}
	zoneStatuses     = []string{"Active", "Lifted"}
)

// bioKit carries what the build step of build() set up, so the biosecurity code
// can use the same schema, record and service helpers.
type bioKit struct {
	b      *Builder
	ctx    context.Context
	l      *workspace.Lease
	owner  string
	now    time.Time
	apply  func(ops ...catalog.Op) (*catalog.Schema, error)
	table  func(sch *catalog.Schema, display string) tbl
	insert func(t tbl, rows []map[string]any) ([]int64, error)
}

type bioTables struct{ sup, spc, sig, zon tbl }

type bioQueries struct{ zones, sightings, sightingStatus, zoneStatus string }

func (k *bioKit) run() error {
	t, err := k.tables()
	if err != nil {
		return err
	}
	rnd := rand.New(rand.NewPCG(11, 2026)) //nolint:gosec // deterministic sample data, not security
	if err := k.rows(t, rnd); err != nil {
		return err
	}
	qs, err := k.queries(t)
	if err != nil {
		return err
	}
	if _, err := k.b.Forms.Put(k.ctx, k.l, "", 0, sightingForm(t, qs), k.owner); err != nil {
		return fmt.Errorf("create form %q: %w", "Record a sighting", err)
	}
	if _, err := k.b.Forms.Put(k.ctx, k.l, "", 0, zoneForm(t, qs), k.owner); err != nil {
		return fmt.Errorf("create form %q: %w", "Quarantine zone", err)
	}
	if _, err := k.b.Reports.Put(k.ctx, k.l, "", 0, sightingsReport(t, qs), k.owner); err != nil {
		return fmt.Errorf("create report %q: %w", "Biosecurity sightings report", err)
	}
	return nil
}

func (k *bioKit) tables() (bioTables, error) {
	sch, err := k.apply(
		create("Suppliers", "The farms and growers that supply Northwind. Their Location is a map point used by the biosecurity forms and reports.",
			required(text("Name")), text("Contact"), text("City"), text("Country"), typed("Location", catalog.GeoPoint)),
		create("Species", "Invasive species that Northwind's supplier farms watch for.",
			required(text("Name")), text("Scientific name"), text("Category"), text("Risk level"), typed("Notes", catalog.LongText)),
	)
	if err != nil {
		return bioTables{}, err
	}
	sup, spc := k.table(sch, "Suppliers"), k.table(sch, "Species")
	observed := catalog.ColumnSpec{DisplayName: "Observed at", Type: catalog.DateTime, Required: true,
		DefaultSQL: `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`}
	photos := catalog.ColumnSpec{DisplayName: "Photos", Type: catalog.Attachments, Options: catalog.TypeOptions{MaxFiles: 10}}
	sch, err = k.apply(
		create("Pest sightings", "Field reports of invasive species on supplier farms: where and when, how many, and photos. Record new ones with the Record a sighting form.",
			required(lookup("Species", spc, "Name")), observed, typed("Location", catalog.GeoPoint), typed("GPS accuracy (m)", catalog.Number),
			typed("Count / abundance", catalog.Integer), text("Status"), lookup("Supplier", sup, "Name"), photos, typed("Notes", catalog.LongText)),
		create("Quarantine zones", "Areas where movement is restricted because of an infestation. Draw the boundary on the map in the Quarantine zone form.",
			required(text("Name")), typed("Boundary", catalog.GeoArea), lookup("Species", spc, "Name"), typed("Declared on", catalog.Date),
			text("Status"), typed("Notes", catalog.LongText)),
	)
	if err != nil {
		return bioTables{}, err
	}
	return bioTables{sup: k.table(sch, "Suppliers"), spc: k.table(sch, "Species"), sig: k.table(sch, "Pest sightings"), zon: k.table(sch, "Quarantine zones")}, nil
}

// ---- geometry -----------------------------------------------------------

// offsetLatLon moves (lat, lon) by dist metres on a bearing (degrees clockwise from north).
func offsetLatLon(lat, lon, bearing, dist float64) (float64, float64) {
	sin, cos := math.Sincos(bearing * math.Pi / 180)
	return offsetEN(lat, lon, dist*sin, dist*cos)
}

// offsetEN moves (lat, lon) by east and north offsets in metres.
func offsetEN(lat, lon, east, north float64) (float64, float64) {
	return lat + north/111320, lon + east/(111320*math.Cos(lat*math.Pi/180))
}

func pointJSON(lat, lon float64) string {
	p, err := geo.NewPoint(lat, lon)
	if err != nil {
		panic("demo: " + err.Error()) // the seed coordinates are fixed and valid
	}
	return p.JSON()
}

// zonePolygon renders a zone as a GeoJSON Polygon: its ring around the farm and
// an optional circular hole (written clockwise by the codec's normalisation).
func zonePolygon(z zoneSeed, lat, lon float64) string {
	ring := func(pts [][2]float64) []any {
		out := make([]any, 0, len(pts)+1)
		for _, p := range pts {
			out = append(out, []float64{p[1], p[0]})
		}
		return append(out, out[0])
	}
	var outer [][2]float64
	for _, v := range z.Ring {
		la, lo := offsetLatLon(lat, lon, v[0], v[1])
		outer = append(outer, [2]float64{la, lo})
	}
	rings := []any{ring(outer)}
	if z.Hole != nil {
		var hole [][2]float64
		for i := 0; i < 16; i++ {
			sin, cos := math.Sincos(float64(i) * 2 * math.Pi / 16)
			la, lo := offsetEN(lat, lon, z.Hole[0]+z.Hole[2]*sin, z.Hole[1]+z.Hole[2]*cos)
			hole = append(hole, [2]float64{la, lo})
		}
		rings = append(rings, ring(hole))
	}
	b, err := json.Marshal(map[string]any{"type": "Polygon", "coordinates": rings})
	if err != nil {
		panic("demo: " + err.Error())
	}
	return string(b)
}

// ---- rows ---------------------------------------------------------------

type sightingSeed struct {
	species  int
	supplier int
	at       time.Time
	lat, lon float64
	accuracy float64 // 0: not recorded
	count    int64
	status   string
	photos   []int // indexes in photoSpecs
	notes    string
}

func (k *bioKit) rows(t bioTables, rnd *rand.Rand) error {
	var rows []map[string]any
	for _, s := range suppliers {
		rows = append(rows, map[string]any{t.sup.col("Name"): s.Name, t.sup.col("Contact"): s.Contact, t.sup.col("City"): s.City,
			t.sup.col("Country"): s.Country, t.sup.col("Location"): pointJSON(s.Lat, s.Lon)})
	}
	supIDs, err := k.insert(t.sup, rows)
	if err != nil {
		return err
	}

	rows = nil
	for _, s := range speciesList {
		rows = append(rows, map[string]any{t.spc.col("Name"): s.Name, t.spc.col("Scientific name"): s.Scientific, t.spc.col("Category"): s.Category,
			t.spc.col("Risk level"): s.Risk, t.spc.col("Notes"): s.Notes})
	}
	spcIDs, err := k.insert(t.spc, rows)
	if err != nil {
		return err
	}

	today := time.Date(k.now.Year(), k.now.Month(), k.now.Day(), 0, 0, 0, 0, time.UTC)
	day := func(d time.Time) string { return d.Format("2006-01-02") }

	rows = nil
	for _, z := range zones {
		s := suppliers[z.Supplier]
		rows = append(rows, map[string]any{t.zon.col("Name"): z.Name, t.zon.col("Boundary"): zonePolygon(z, s.Lat, s.Lon),
			t.zon.col("Species"): num(spcIDs[z.Species]), t.zon.col("Declared on"): day(today.AddDate(0, 0, -z.Declared)),
			t.zon.col("Status"): z.Status, t.zon.col("Notes"): z.Notes})
	}
	if _, err := k.insert(t.zon, rows); err != nil {
		return err
	}

	photoIDs, err := k.uploadPhotos()
	if err != nil {
		return err
	}

	// Sightings: scattered around each farm, so some fall inside a zone, some
	// outside it and two inside the hole of the Loire zone. Oldest first.
	var seeds []sightingSeed
	accuracies := []float64{3.5, 4.8, 6, 8, 12, 15, 25}
	for ci, c := range clusters {
		farm := suppliers[c.Supplier]
		for i := 0; i < c.N; i++ {
			lat, lon := offsetLatLon(farm.Lat, farm.Lon, rnd.Float64()*360, c.RadiusM*math.Sqrt(rnd.Float64()))
			if ci == 1 && i < 2 {
				// Inside the hole (the treated orchard block) of the Loire zone.
				h := zones[1].Hole
				lat, lon = offsetEN(farm.Lat, farm.Lon, h[0]+float64(i)*140-70, h[1]+float64(i)*90-45)
			}
			age := c.MinAge + rnd.IntN(c.MaxAge-c.MinAge+1)
			s := sightingSeed{species: c.Species[rnd.IntN(len(c.Species))], supplier: c.Supplier, lat: lat, lon: lon,
				at:    today.AddDate(0, 0, -age).Add(time.Duration(6*60+rnd.IntN(12*60)) * time.Minute),
				count: int64(1 + rnd.IntN(c.Count)), notes: sightingNotes[rnd.IntN(len(sightingNotes))]}
			if rnd.IntN(8) != 0 {
				s.accuracy = accuracies[rnd.IntN(len(accuracies))]
			}
			switch {
			case c.Cleaned || age > 100:
				s.status = []string{"Treated", "Cleared"}[rnd.IntN(2)]
			case age < 10:
				s.status = "Reported"
			case age < 35:
				s.status = []string{"Reported", "Confirmed"}[rnd.IntN(2)]
			default:
				s.status = []string{"Confirmed", "Treated", "Confirmed"}[rnd.IntN(3)]
			}
			if len(photoIDs) > 0 {
				switch r := rnd.IntN(10); {
				case r < 4:
				case r < 7:
					s.photos = []int{s.species}
				case r < 9:
					s.photos = []int{s.species, 6 + rnd.IntN(2)}
				default:
					s.photos = []int{s.species, 6, 7}
				}
			}
			seeds = append(seeds, s)
		}
	}
	slices.SortStableFunc(seeds, func(a, b sightingSeed) int { return a.at.Compare(b.at) })

	rows = nil
	for _, s := range seeds {
		r := map[string]any{
			t.sig.col("Species"):           num(spcIDs[s.species]),
			t.sig.col("Observed at"):       s.at.Format(time.RFC3339),
			t.sig.col("Location"):          pointJSON(s.lat, s.lon),
			t.sig.col("Count / abundance"): num(s.count),
			t.sig.col("Status"):            s.status,
			t.sig.col("Supplier"):          num(supIDs[s.supplier]),
			t.sig.col("Notes"):             s.notes,
		}
		if s.accuracy > 0 {
			r[t.sig.col("GPS accuracy (m)")] = json.Number(strconv.FormatFloat(s.accuracy, 'f', -1, 64))
		}
		if len(s.photos) > 0 {
			ids := make([]string, len(s.photos))
			for i, p := range s.photos {
				ids[i] = photoIDs[p]
			}
			r[t.sig.col("Photos")] = ids
		}
		rows = append(rows, r)
	}
	_, err = k.insert(t.sig, rows)
	return err
}

// uploadPhotos stores the generated pictures in the workspace bucket and returns
// their file ids (same order as photoSpecs); nil when no file service is set.
func (k *bioKit) uploadPhotos() ([]string, error) {
	if k.b.Files == nil {
		return nil, nil
	}
	ids := make([]string, len(photoSpecs))
	for i, p := range photoSpecs {
		m, err := k.b.Files.Upload(k.ctx, k.l, k.owner, p.File, bytes.NewReader(renderPhoto(p, uint64(100+i))))
		if err != nil {
			return nil, fmt.Errorf("store sample photo %s: %w", p.File, err)
		}
		ids[i] = m.ID
	}
	return ids, nil
}

// ---- queries ------------------------------------------------------------

func (k *bioKit) queries(t bioTables) (bioQueries, error) {
	Q := func(tb tbl) string { return q(tb.t.PhysicalName) }
	S, Z, P, U := Q(t.sig), Q(t.zon), Q(t.spc), Q(t.sup)
	loc, bnd := q(t.sig.phys("Location")), q(t.zon.phys("Boundary"))
	statusList := func(vals []string) string {
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = "SELECT '" + v + "'"
			if i == 0 {
				parts[i] += " AS Status"
			}
		}
		return strings.Join(parts, " UNION ALL ")
	}

	var out bioQueries
	put := func(in query.SavedInput) (string, error) {
		s, err := k.b.Queries.Put(k.ctx, k.l, "", 0, in, k.owner)
		if err != nil {
			return "", fmt.Errorf("save query %q: %w", in.Name, err)
		}
		return s.ID, nil
	}
	var err error
	// The two overlays and the pick lists feed the forms.
	if out.zones, err = put(query.SavedInput{Name: "Zone boundaries", Kind: "sql",
		Description: "Every quarantine zone with its boundary. Drawn as the overlay on the sighting form and the report maps.",
		SQL: fmt.Sprintf(`SELECT z.%s || CASE WHEN z.%s = 'Lifted' THEN ' (lifted)' ELSE '' END AS Zone, z.%s AS Boundary FROM %s z WHERE z.%s IS NOT NULL ORDER BY z.%s`,
			q(t.zon.phys("Name")), q(t.zon.phys("Status")), bnd, Z, bnd, q(t.zon.phys("Name")))}); err != nil {
		return out, err
	}
	if out.sightings, err = put(query.SavedInput{Name: "Sighting locations", Kind: "sql",
		Description: "Where each species was seen. Drawn as the overlay on the Quarantine zone form.",
		SQL: fmt.Sprintf(`SELECT sp.%s AS Species, s.%s AS Location FROM %s s JOIN %s sp ON sp.id = s.%s WHERE s.%s IS NOT NULL`,
			q(t.spc.phys("Name")), loc, S, P, q(t.sig.phys("Species")), loc)}); err != nil {
		return out, err
	}
	if out.sightingStatus, err = put(query.SavedInput{Name: "Sighting statuses", Kind: "sql",
		Description: "The pick list for the Status of a sighting.", SQL: statusList(sightingStatuses)}); err != nil {
		return out, err
	}
	if out.zoneStatus, err = put(query.SavedInput{Name: "Zone statuses", Kind: "sql",
		Description: "The pick list for the Status of a quarantine zone.", SQL: statusList(zoneStatuses)}); err != nil {
		return out, err
	}

	// The three analysis queries of the walkthrough.
	if _, err = put(query.SavedInput{Name: "Sightings inside quarantine zones", Kind: "visual",
		Description: "A spatial join: each sighting whose location lies inside a quarantine zone's boundary (zones with a hole exclude it).",
		Visual: &query.Model{
			Sources: []query.Source{{Alias: "s", TableID: t.sig.id(), X: 40, Y: 40}, {Alias: "z", TableID: t.zon.id(), X: 380, Y: 40}, {Alias: "sp", TableID: t.spc.id(), X: 40, Y: 280}},
			Joins: []query.Join{
				{LeftAlias: "s", LeftColumnID: t.sig.col("Location"), RightAlias: "z", RightColumnID: t.zon.col("Boundary"), Kind: "inner", Op: "within"},
				{LeftAlias: "s", LeftColumnID: t.sig.col("Species"), RightAlias: "sp", RightColumnID: "id", Kind: "inner"},
			},
			Fields: []query.Field{
				{Alias: "z", ColumnID: t.zon.col("Name"), OutputName: "Zone"},
				{Alias: "sp", ColumnID: t.spc.col("Name"), OutputName: "Species"},
				{Alias: "s", ColumnID: t.sig.col("Observed at"), Sort: "desc"},
				{Alias: "s", ColumnID: t.sig.col("Status")},
				{Alias: "z", ColumnID: t.zon.col("Status"), OutputName: "Zone status"},
			},
		}}); err != nil {
		return out, err
	}
	if _, err = put(query.SavedInput{Name: "Sightings near a point", Kind: "sql",
		Description: "Sightings within a distance of a point, nearest first. Try lat 51.27, lon 0.52 and 5000 metres.",
		Parameters:  []query.Param{{Name: "lat", Type: "number"}, {Name: "lon", Type: "number"}, {Name: "metres", Type: "number"}},
		SQL: fmt.Sprintf(`SELECT sp.%s AS Species, s.%s AS "Observed at", s.%s AS Status, su.%s AS Supplier, round(geo_distance_m(s.%s, geo_point(:lat, :lon))) AS "Distance (m)" `+
			`FROM %s s JOIN %s sp ON sp.id = s.%s LEFT JOIN %s su ON su.id = s.%s `+
			`WHERE geo_distance_m(s.%s, geo_point(:lat, :lon)) <= :metres ORDER BY "Distance (m)"`,
			q(t.spc.phys("Name")), q(t.sig.phys("Observed at")), q(t.sig.phys("Status")), q(t.sup.phys("Name")), loc,
			S, P, q(t.sig.phys("Species")), U, q(t.sig.phys("Supplier")), loc)}); err != nil {
		return out, err
	}
	if _, err = put(query.SavedInput{Name: "Zone sizes", Kind: "sql",
		Description: "Each quarantine zone's area in hectares (geo_area_m2 / 10000) and how many sightings lie inside it.",
		SQL: fmt.Sprintf(`SELECT z.%s AS Zone, sp.%s AS Species, z.%s AS Status, round(geo_area_m2(z.%s) / 10000.0, 1) AS "Area (ha)", `+
			`(SELECT count(*) FROM %s s WHERE geo_within(s.%s, z.%s)) AS Sightings FROM %s z LEFT JOIN %s sp ON sp.id = z.%s ORDER BY "Area (ha)" DESC`,
			q(t.zon.phys("Name")), q(t.spc.phys("Name")), q(t.zon.phys("Status")), bnd, S, loc, bnd, Z, P, q(t.zon.phys("Species")))}); err != nil {
		return out, err
	}
	return out, nil
}

// ---- forms --------------------------------------------------------------

// stack lays controls out in one column of caption-above-field pairs, which is
// also the order the form uses on a phone.
type stack struct {
	controls []forms.Control
	y, n     int
}

const stackWidth = 388

func (s *stack) add(caption, name, kind string, h int, mod func(*forms.Control)) {
	s.n++
	s.controls = append(s.controls,
		forms.Control{ID: fmt.Sprintf("l%d", s.n), Name: fmt.Sprintf("Label%d", s.n), Type: "label", X: 16, Y: s.y, W: stackWidth, H: 20, Caption: caption, Style: map[string]string{"weight": "bold"}})
	c := forms.Control{ID: fmt.Sprintf("c%d", s.n), Name: name, Type: kind, X: 16, Y: s.y + 22, W: stackWidth, H: h, TabIndex: s.n}
	mod(&c)
	s.controls = append(s.controls, c)
	s.y += 22 + h + 12
}

func bound(col string) func(*forms.Control) {
	return func(c *forms.Control) { c.Bind = &forms.Bind{ColumnID: col} }
}

func pick(col string, l forms.Lookup) func(*forms.Control) {
	return func(c *forms.Control) { c.Bind = &forms.Bind{ColumnID: col}; c.Lookup = &l }
}

func sightingForm(t bioTables, qs bioQueries) forms.FormInput {
	s := &stack{y: 12}
	s.add("Species", "Species", "combobox", 32, pick(t.sig.col("Species"), forms.Lookup{TableID: t.spc.id(), ValueColumnID: "id", DisplayColumnID: t.spc.col("Name")}))
	s.add("Observed at", "ObservedAt", "textbox", 32, bound(t.sig.col("Observed at")))
	s.add("Location (tap the map or use your location)", "Location", "map", 320, func(c *forms.Control) {
		c.Bind = &forms.Bind{ColumnID: t.sig.col("Location")}
		c.Map = &forms.MapOptions{OverlayQueryID: qs.zones, OverlayGeoColumn: "Boundary", OverlayLabelColumn: "Zone"}
	})
	s.add("GPS accuracy (m)", "GPSAccuracy", "textbox", 32, bound(t.sig.col("GPS accuracy (m)")))
	s.add("Count / abundance", "Count", "textbox", 32, bound(t.sig.col("Count / abundance")))
	s.add("Status", "Status", "combobox", 32, pick(t.sig.col("Status"), forms.Lookup{SourceQueryID: qs.sightingStatus, ValueColumnID: "Status", DisplayColumnID: "Status"}))
	s.add("Supplier (farm)", "Supplier", "combobox", 32, pick(t.sig.col("Supplier"), forms.Lookup{TableID: t.sup.id(), ValueColumnID: "id", DisplayColumnID: t.sup.col("Name")}))
	s.add("Photos", "Photos", "attachments", 210, bound(t.sig.col("Photos")))
	s.add("Notes", "Notes", "textbox", 90, bound(t.sig.col("Notes")))
	return forms.FormInput{Name: "Record a sighting", SourceKind: "table", SourceID: t.sig.id(), DefaultView: "single",
		Layout: forms.Layout{Width: 420, Sections: forms.Sections{Detail: forms.Section{Height: s.y + 4, Controls: s.controls}}}}
}

func zoneForm(t bioTables, qs bioQueries) forms.FormInput {
	s := &stack{y: 12}
	s.add("Zone name", "ZoneName", "textbox", 32, bound(t.zon.col("Name")))
	s.add("Species", "Species", "combobox", 32, pick(t.zon.col("Species"), forms.Lookup{TableID: t.spc.id(), ValueColumnID: "id", DisplayColumnID: t.spc.col("Name")}))
	s.add("Status", "Status", "combobox", 32, pick(t.zon.col("Status"), forms.Lookup{SourceQueryID: qs.zoneStatus, ValueColumnID: "Status", DisplayColumnID: "Status"}))
	s.add("Declared on", "DeclaredOn", "textbox", 32, bound(t.zon.col("Declared on")))
	s.add("Boundary (choose Edit area to draw the fence)", "Boundary", "map", 360, func(c *forms.Control) {
		c.Bind = &forms.Bind{ColumnID: t.zon.col("Boundary")}
		c.Map = &forms.MapOptions{OverlayQueryID: qs.sightings, OverlayGeoColumn: "Location"}
	})
	s.add("Notes", "Notes", "textbox", 90, bound(t.zon.col("Notes")))
	return forms.FormInput{Name: "Quarantine zone", SourceKind: "table", SourceID: t.zon.id(), DefaultView: "single",
		Layout: forms.Layout{Width: 420, Sections: forms.Sections{Detail: forms.Section{Height: s.y + 4, Controls: s.controls}}}}
}

// ---- report -------------------------------------------------------------

func sightingsReport(t bioTables, qs bioQueries) reports.ReportInput {
	col := t.sig.col
	bold := map[string]string{"bold": "true"}
	value := func(id, name string, x, y, w, h int, column string) reports.ReportControl {
		return reports.ReportControl{ID: id, Name: name, Type: "textbox", X: x, Y: y, W: w, H: h, Bind: &forms.Bind{ColumnID: col(column)}}
	}
	caption := func(id, name string, y int, text string) reports.ReportControl {
		return reports.ReportControl{ID: id, Name: name, Type: "label", Y: y, W: 62, H: 20, Caption: text, Style: bold}
	}
	notes := value("dn", "Notes", 0, 124, 226, 56, "Notes")
	notes.Style = map[string]string{"align": "left"}
	lay := reports.ReportLayout{Width: 680,
		Bands: reports.Bands{
			ReportHeader: reports.Band{Height: 56, Controls: []reports.ReportControl{
				{ID: "title", Name: "Title", Type: "label", W: 480, H: 30, Caption: "Biosecurity sightings", Style: map[string]string{"bold": "true", "font_size": "20"}},
				{ID: "sub", Name: "Subtitle", Type: "label", Y: 32, W: 560, H: 20, Caption: "Invasive species recorded on supplier farms, with their location, the quarantine zones and the first photo."}}},
			Detail: reports.Band{Height: 190, KeepTogether: true, Controls: []reports.ReportControl{
				caption("c1", "CObserved", 4, "Observed"), value("d1", "ObservedAt", 64, 4, 162, 20, "Observed at"),
				caption("c2", "CSupplier", 28, "Supplier"), value("d2", "SupplierName", 64, 28, 162, 20, "Supplier"),
				caption("c3", "CCount", 52, "Count"), value("d3", "CountValue", 64, 52, 162, 20, "Count / abundance"),
				caption("c4", "CStatus", 76, "Status"), value("d4", "StatusValue", 64, 76, 162, 20, "Status"),
				caption("c5", "CAccuracy", 100, "GPS (m)"), value("d5", "AccuracyValue", 64, 100, 162, 20, "GPS accuracy (m)"),
				notes,
				{ID: "map", Name: "LocationMap", Type: "map", X: 238, Y: 4, W: 270, H: 178, Bind: &forms.Bind{ColumnID: col("Location")},
					Map: &forms.MapOptions{OverlayQueryID: qs.zones, OverlayGeoColumn: "Boundary", OverlayLabelColumn: "Zone"}},
				{ID: "img", Name: "FirstPhoto", Type: "image", X: 520, Y: 4, W: 160, H: 178, Bind: &forms.Bind{ColumnID: col("Photos")}, MaxImages: 1},
			}},
			PageFooter: reports.Band{Height: 22, Controls: []reports.ReportControl{{ID: "pn", Name: "PageNo", Type: "page_number", W: 60, H: 20}}},
			ReportFooter: reports.Band{Height: 26, Controls: []reports.ReportControl{
				{ID: "tcl", Name: "TotalSightingsLabel", Type: "label", W: 110, H: 20, Caption: "Total sightings:", Style: bold},
				{ID: "tc", Name: "TotalSightings", Type: "textbox", X: 114, W: 60, H: 20, Aggregate: &reports.Aggregate{Func: "count", Scope: "report"}}}},
		},
		Groups: []reports.Group{{ColumnID: col("Species"), ReportSort: "asc",
			Header: reports.Band{Height: 32, Controls: []reports.ReportControl{
				{ID: "g1", Name: "SpeciesName", Type: "textbox", W: 400, H: 24, Bind: &forms.Bind{ColumnID: col("Species")}, Style: map[string]string{"bold": "true", "font_size": "14"}}}},
			Footer: reports.Band{Height: 26, Controls: []reports.ReportControl{
				{ID: "gcl", Name: "SpeciesCountLabel", Type: "label", W: 70, H: 20, Caption: "Sightings:", Style: bold},
				{ID: "gc", Name: "SpeciesCount", Type: "textbox", X: 74, W: 60, H: 20, Aggregate: &reports.Aggregate{Func: "count", Scope: "group"}}}}}},
		Sorts: []reports.ReportSort{{ColumnID: col("Observed at"), Desc: true}},
	}
	return reports.ReportInput{Name: "Biosecurity sightings report", SourceKind: "table", SourceID: t.sig.id(), ReportLayout: lay,
		PageSetup: reports.PageSetup{Paper: "a4", Orientation: "portrait", MarginTop: 48, MarginBot: 48, MarginLeft: 48, MarginRight: 48}}
}

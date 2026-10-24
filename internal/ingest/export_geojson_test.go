package ingest

import (
	"encoding/json"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

func TestExportGeoJSON(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Sightings", catalog.ColumnSpec{DisplayName: "Species", Type: catalog.ShortText},
		catalog.ColumnSpec{DisplayName: "Spot", Type: catalog.GeoPoint},
		catalog.ColumnSpec{DisplayName: "Zone", Type: catalog.GeoArea},
		catalog.ColumnSpec{DisplayName: "Seen", Type: catalog.Integer})
	if err := e.l.Write(e.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(e.ctx, `INSERT INTO "Sightings"("Species", "Spot", "Zone", "Seen") VALUES
			('Kudzu', '{"type":"Point","coordinates":[-0.14,51.5]}', '{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}', 3),
			('Reed', NULL, NULL, NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The second request goes through the filtered/sorted path.
	for _, req := range []ExportRequest{
		{SourceKind: "table", SourceID: tb.ID, Format: "geojson"},
		{SourceKind: "table", SourceID: tb.ID, Format: "geojson", View: &records.ListQuery{Sort: []records.Sort{{Col: col2(nil, tb, "Species").ID, Desc: true}}}},
	} {
		_, body := e.export(req, query.Policy{})
		var fc struct {
			Type     string `json:"type"`
			Features []struct {
				Type     string          `json:"type"`
				ID       int64           `json:"id"`
				Geometry json.RawMessage `json:"geometry"`
				Props    map[string]any  `json:"properties"`
			} `json:"features"`
		}
		if err := json.Unmarshal([]byte(body), &fc); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, body)
		}
		if fc.Type != "FeatureCollection" || len(fc.Features) != 2 {
			t.Fatalf("%s", body)
		}
		kudzu, reed := fc.Features[0], fc.Features[1]
		if req.View != nil {
			kudzu, reed = fc.Features[1], fc.Features[0]
		}
		if kudzu.Type != "Feature" || kudzu.ID == 0 || !strings.Contains(string(kudzu.Geometry), `"Point"`) ||
			kudzu.Props["Species"] != "Kudzu" || kudzu.Props["Seen"] != float64(3) {
			t.Fatalf("%+v", kudzu)
		}
		zone, ok := kudzu.Props["Zone"].(map[string]any)
		if !ok || zone["type"] != "Polygon" {
			t.Fatalf("other geo fields are properties: %v", kudzu.Props)
		}
		if string(reed.Geometry) != "null" || reed.Props["Zone"] != nil {
			t.Fatalf("record without a location: %+v", reed)
		}
	}
}

func TestExportGeoJSONNeedsGeoTable(t *testing.T) {
	e := newEnv(t)
	plain := e.table("Plain", catalog.ColumnSpec{DisplayName: "Name", Type: catalog.ShortText})
	if _, err := e.svc.StartExport(e.ctx, e.l, "u1", query.Policy{}, ExportRequest{SourceKind: "table", SourceID: plain.ID, Format: "geojson"}); fcode(err) != "BAD_REQUEST" {
		t.Fatalf("table without geo: %v", err)
	}
	if _, err := e.svc.StartExport(e.ctx, e.l, "u1", query.Policy{}, ExportRequest{SourceKind: "query", SourceID: "q", Format: "geojson"}); fcode(err) != "BAD_REQUEST" {
		t.Fatalf("query: %v", err)
	}
}

package ingest

import (
	"encoding/json"
	"io"

	"abidata/internal/catalog"
	"abidata/internal/fault"
)

// geojsonSink writes a table as a GeoJSON FeatureCollection (RFC 7946, ADR
// 0010). The first geo field is each feature's geometry (null when the record
// has none); the record id is the feature id and every other field is a
// property, named as in the other formats.
type geojsonSink struct {
	w     io.Writer
	geom  int // position of the geometry field in a row (ID is 0), -1 until bound
	names [][]byte
	count int
}

// bindGeometry picks the geometry field among a table's exported fields (rows
// carry the ID first). It fails when there is none.
func (g *geojsonSink) bindGeometry(cols []catalog.Column) error {
	for i, c := range cols {
		if c.Type == catalog.GeoPoint || c.Type == catalog.GeoArea {
			g.geom = i + 1
			return nil
		}
	}
	return fault.New(fault.BadRequest, "GeoJSON needs a table with a geographic field (point or area).")
}

func (g *geojsonSink) header(cols []string) error {
	for _, c := range cols {
		b, _ := json.Marshal(c)
		g.names = append(g.names, b)
	}
	_, err := io.WriteString(g.w, `{"type":"FeatureCollection","features":[`)
	return err
}

func (g *geojsonSink) row(vals []any, _ []bool) error {
	b := make([]byte, 0, 256)
	if g.count > 0 {
		b = append(b, ',')
	}
	b = append(b, "\n"+`{"type":"Feature","id":`...)
	id, err := json.Marshal(vals[0])
	if err != nil {
		return err
	}
	b = append(b, id...)
	b = append(b, `,"geometry":`...)
	geom, err := geoJSON(vals[g.geom])
	if err != nil {
		return err
	}
	b = append(b, geom...)
	b = append(b, `,"properties":{`...)
	first := true
	for i := 1; i < len(vals); i++ {
		if i == g.geom {
			continue
		}
		if !first {
			b = append(b, ',')
		}
		first = false
		b = append(b, g.names[i]...)
		b = append(b, ':')
		v, err := geoJSON(vals[i])
		if err != nil {
			return err
		}
		b = append(b, v...)
	}
	b = append(b, "}}"...)
	g.count++
	_, err = g.w.Write(b)
	return err
}

func (g *geojsonSink) finish() error { _, err := io.WriteString(g.w, "\n]}\n"); return err }

// geoJSON is a value as JSON. A geographic field's value may arrive as the
// stored GeoJSON text; it is embedded as an object, not as a string.
func geoJSON(v any) ([]byte, error) {
	if s, ok := v.(string); ok && len(s) > 1 && s[0] == '{' && json.Valid([]byte(s)) {
		return []byte(s), nil
	}
	return json.Marshal(v)
}

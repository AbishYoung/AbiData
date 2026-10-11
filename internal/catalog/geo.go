package catalog

import (
	"fmt"
	"strings"

	"abidata/internal/geo"
)

// Geographic fields (ADR 0010) are TEXT columns holding canonical GeoJSON.
// This file holds everything type-specific so other field types merge cleanly.

func isGeo(t FieldType) bool { return t == GeoPoint || t == GeoArea }

// geoTypeCheck is the type-intrinsic CHECK for a geo column: valid JSON of the
// right geometry type. It uses only SQLite builtins so workspace files stay
// readable by stock sqlite3; full validation happens in the records codec.
func geoTypeCheck(q string, t FieldType) string {
	types := "'Point'"
	if t == GeoArea {
		types = "'Polygon','MultiPolygon'"
	}
	return fmt.Sprintf("%s IS NULL OR (json_valid(%s) AND json_extract(%s, '$.type') IN (%s))", q, q, q, types)
}

// convertGeo converts into or out of a geo field. Geo fields convert only to
// and from short and long text: a point is written as "lat, lon" and an area
// as its GeoJSON; text is parsed with the same rules as API input. Blank text
// becomes empty.
func convertGeo(from, to FieldType, v any) (any, bool) {
	s, ok := v.(string)
	if !ok {
		return nil, false
	}
	switch {
	case isGeo(to) && (from == ShortText || from == LongText):
		if strings.TrimSpace(s) == "" {
			return nil, true
		}
		if to == GeoPoint {
			out, err := geo.NormalizePoint(s)
			if err != nil {
				return nil, false
			}
			return out, true
		}
		out, err := geo.NormalizeArea(s)
		if err != nil {
			return nil, false
		}
		return out, true
	case from == GeoPoint && (to == ShortText || to == LongText):
		p, err := geo.ParsePoint(s)
		if err != nil {
			return nil, false
		}
		return p.Text(), true
	case from == GeoArea && (to == ShortText || to == LongText):
		if _, err := geo.ParseArea(s); err != nil {
			return nil, false
		}
		return s, true
	}
	return nil, false
}

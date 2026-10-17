package catalog

import (
	"strings"

	"abidata/internal/geo"
)

// Geographic fields (ADR 0010) are TEXT columns holding canonical GeoJSON.
// This file holds everything type-specific so other field types merge cleanly.

func isGeo(t FieldType) bool { return t == GeoPoint || t == GeoArea }

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

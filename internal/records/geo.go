package records

import (
	"encoding/json"
	"fmt"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/geo"
)

// Geographic fields (ADR 0010): the API value is a GeoJSON geometry object and
// the stored value is its canonical text. Everything geo-specific in this
// package lives here (and in filter_geo.go) so other field types merge cleanly.

func isGeoType(t catalog.FieldType) bool { return t == catalog.GeoPoint || t == catalog.GeoArea }

// decodeGeo validates and normalises an API value for a geo_point or geo_area
// column. Points also accept {"lat","lon"}, "lat, lon" and a Feature; areas a
// Feature. Failures are VALIDATION_FAILED errors naming the field.
func decodeGeo(c catalog.Column, v any) (any, error) {
	var out string
	var err error
	if c.Type == catalog.GeoPoint {
		out, err = geo.NormalizePoint(v)
	} else {
		out, err = geo.NormalizeArea(v)
	}
	if err != nil {
		kind := "a location: GeoJSON Point, {\"lat\": .., \"lon\": ..} or \"lat, lon\""
		if c.Type == catalog.GeoArea {
			kind = "an area: GeoJSON Polygon or MultiPolygon"
		}
		return nil, &fault.Error{
			Code:    fault.ValidationFailed,
			Message: fmt.Sprintf("%q must be %s (%s). Nothing was saved.", c.DisplayName, kind, err),
			Details: map[string]any{"fields": []string{c.ID}},
		}
	}
	return out, nil
}

// encodeGeo returns the GeoJSON object for a stored geo value, or the stored
// text unchanged when it is not valid JSON (written outside the records API).
func encodeGeo(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if !json.Valid([]byte(s)) {
		return s
	}
	return geo.Raw(s)
}

func geoTypeRuleMessage(c catalog.Column) string {
	if c.Type == catalog.GeoArea {
		return fmt.Sprintf("%q must be an area (GeoJSON Polygon or MultiPolygon).", c.DisplayName)
	}
	return fmt.Sprintf("%q must be a location (GeoJSON Point).", c.DisplayName)
}

package reports

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/geo"
)

// Geographic values in reports (ADR 0010). Text boxes print them the way the
// datasheet shows them (web/src/lib/geo/format.ts); map controls receive the
// geometry itself.

// geoValue returns a stored geo value as a GeoJSON object for a map control:
// table sources already carry geo.Raw; query sources carry the stored text.
func geoValue(v any) any {
	switch x := v.(type) {
	case geo.Raw:
		return x
	case string:
		if looksLikeGeo(x) && json.Valid([]byte(x)) {
			return geo.Raw(x)
		}
	}
	return nil
}

func looksLikeGeo(s string) bool {
	return strings.HasPrefix(s, `{"type":"Point"`) || strings.HasPrefix(s, `{"type":"Polygon"`) || strings.HasPrefix(s, `{"type":"MultiPolygon"`)
}

// geoText formats a point as "lat, lon" with 5 decimals and an area as
// "Area · 12 points · 3.4 km²". ok is false when v is not a geo value; a
// value of a geo column that cannot be parsed prints as its text.
func geoText(v any, ft catalog.FieldType) (string, bool) {
	var s string
	switch x := v.(type) {
	case geo.Raw:
		s = string(x)
	case string:
		s = x
	default:
		return "", false
	}
	isGeoCol := ft == catalog.GeoPoint || ft == catalog.GeoArea
	if !isGeoCol && !looksLikeGeo(s) {
		return "", false
	}
	if strings.HasPrefix(s, `{"type":"Point"`) || ft == catalog.GeoPoint {
		if p, err := geo.ParsePoint(s); err == nil {
			return pointText(p.Lat, p.Lon), true
		}
	} else if a, err := geo.ParseArea(s); err == nil {
		return areaSummary(a), true
	}
	if isGeoCol {
		return s, true
	}
	return "", false
}

func fixed5(x float64) string {
	t := strconv.FormatFloat(x, 'f', 5, 64)
	if t == "-0.00000" {
		return "0.00000"
	}
	return t
}

func pointText(lat, lon float64) string { return fixed5(lat) + ", " + fixed5(lon) }

// areaSummary is "Area · N points · size"; N counts vertices (ring-closing
// positions excluded) across all rings.
func areaSummary(a *geo.Area) string {
	n := 0
	for _, poly := range a.Polygons {
		for _, r := range poly {
			n += max(len(r)-1, 0)
		}
	}
	unit := "points"
	if n == 1 {
		unit = "point"
	}
	return "Area · " + strconv.Itoa(n) + " " + unit + " · " + areaText(a.AreaM2())
}

// areaText prints square metres below 0.1 km², then km² with one decimal
// below 100 km², then whole km².
func areaText(m2 float64) string {
	if m2 < 100_000 {
		return thousands(strconv.FormatFloat(math.Round(m2), 'f', 0, 64)) + " m²"
	}
	km := m2 / 1e6
	if km < 100 {
		return strconv.FormatFloat(km, 'f', 1, 64) + " km²"
	}
	return thousands(strconv.FormatFloat(math.Round(km), 'f', 0, 64)) + " km²"
}

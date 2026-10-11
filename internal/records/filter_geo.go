package records

import (
	"encoding/json"
	"math"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/geo"
)

// Spatial filter operators for geo_point fields (ADR 0010):
//
//	{"col": <point field>, "op": "within", "value": <GeoJSON Polygon/MultiPolygon>}
//	{"col": <point field>, "op": "near",   "value": {"lat": .., "lon": .., "metres": ..}}
//
// near also accepts [lat, lon, metres] and the text "lat,lon,metres". There is
// no spatial index; both are a scan of the table.

// geoFieldType returns the geo type of a column (including a calculated field
// whose result type is geo), or "" when it is not a geo field.
func geoFieldType(c catalog.Column) catalog.FieldType {
	t := c.Type
	if t == catalog.Calculated {
		t = ResultFieldType(c.Options.ResultType)
	}
	if isGeoType(t) {
		return t
	}
	return ""
}

func geoFilterErr(format string, args ...any) error {
	return fault.Newf(fault.BadRequest, format, args...)
}

// geoCond compiles within and near, and refuses every other comparison on a
// geo field except is_null and not_null.
func (fc *filterCompiler) geoCond(col catalog.Column, q string, f *Filter) (string, bool, error) {
	gt := geoFieldType(col)
	switch f.Op {
	case OpWithin, OpNear:
		if gt != catalog.GeoPoint {
			return "", true, geoFilterErr("%s is only available for location fields.", f.Op)
		}
	case OpIsNull, OpNotNull:
		return "", false, nil
	default:
		if gt != "" {
			return "", true, geoFilterErr("Location and area fields can only be filtered with within, near, is_null or not_null.")
		}
		return "", false, nil
	}
	if f.Op == OpWithin {
		a, err := geo.ParseArea(f.Value)
		if err != nil {
			return "", true, geoFilterErr("The within filter needs an area (GeoJSON Polygon or MultiPolygon): %s.", err)
		}
		fc.args = append(fc.args, a.JSON())
		return "geo_within(" + q + ", ?) = 1", true, nil
	}
	p, metres, err := parseNear(f.Value)
	if err != nil {
		return "", true, err
	}
	fc.args = append(fc.args, p.JSON(), metres)
	return "geo_distance_m(" + q + ", ?) <= ?", true, nil
}

func parseNear(v any) (geo.Point, float64, error) {
	bad := func(detail string) (geo.Point, float64, error) {
		return geo.Point{}, 0, geoFilterErr("The near filter needs a latitude, a longitude and a distance in metres: %s.", detail)
	}
	var lat, lon, metres json.RawMessage
	switch x := v.(type) {
	case string:
		parts := strings.Split(x, ",")
		if len(parts) != 3 {
			return bad("write it as lat,lon,metres")
		}
		lat, lon, metres = json.RawMessage(strings.TrimSpace(parts[0])), json.RawMessage(strings.TrimSpace(parts[1])), json.RawMessage(strings.TrimSpace(parts[2]))
	case []any:
		if len(x) != 3 {
			return bad("use [lat, lon, metres]")
		}
		var err error
		if lat, lon, metres, err = marshal3(x[0], x[1], x[2]); err != nil {
			return bad("the values must be numbers")
		}
	case map[string]any:
		m, ok := x["metres"]
		if !ok {
			m, ok = x["meters"]
		}
		if !ok {
			m, ok = x["m"]
		}
		lo, okLon := x["lon"]
		if !okLon {
			lo = x["lng"]
		}
		var err error
		if lat, lon, metres, err = marshal3(x["lat"], lo, m); err != nil || !ok {
			return bad("use {\"lat\", \"lon\", \"metres\"}")
		}
	default:
		return bad("use {\"lat\", \"lon\", \"metres\"}")
	}
	var nums [3]float64
	for i, raw := range []json.RawMessage{lat, lon, metres} {
		f, ok := rawNumber(raw)
		if !ok {
			return bad("the values must be numbers")
		}
		nums[i] = f
	}
	p, err := geo.NewPoint(nums[0], nums[1])
	if err != nil {
		return geo.Point{}, 0, geoFilterErr("The near filter has an invalid location: %s.", err)
	}
	if nums[2] < 0 || math.IsNaN(nums[2]) || math.IsInf(nums[2], 0) {
		return bad("the distance must be zero or more")
	}
	return p, nums[2], nil
}

func marshal3(a, b, c any) (json.RawMessage, json.RawMessage, json.RawMessage, error) {
	var out [3]json.RawMessage
	for i, v := range []any{a, b, c} {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, nil, nil, err
		}
		out[i] = raw
	}
	return out[0], out[1], out[2], nil
}

func rawNumber(raw json.RawMessage) (float64, bool) {
	var f float64
	if json.Unmarshal(raw, &f) == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		return f, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(strings.TrimSpace(s))
		if json.Unmarshal(raw, &f) == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, true
		}
	}
	return 0, false
}

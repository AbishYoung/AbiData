// Package geo is AbiData's pure-Go geometry kernel for geographic fields
// (ADR 0010): parsing and normalising GeoJSON points and areas, validation
// limits, great-circle distance, point-in-area tests, spherical area, centroid
// and bounding box.
//
// Coordinates are WGS84 [longitude, latitude] in degrees. Containment is
// planar in longitude/latitude (even-odd ray casting); distance and area are
// spherical. Areas whose edges cross the antimeridian (±180° longitude) are
// not supported and are rejected: draw such an area as a MultiPolygon with one
// polygon on each side of the meridian instead.
package geo

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Limits (ADR 0010).
const (
	MaxPositions = 10000   // positions in one area, closing positions included
	MaxJSONBytes = 1 << 20 // 1 MiB of JSON, for input and for the stored value
	// EarthRadiusM is the mean Earth radius used for distance and area.
	EarthRadiusM = 6371008.8
)

// Pos is a longitude/latitude position.
type Pos struct{ Lon, Lat float64 }

// Point is one location, optionally with an altitude in metres.
type Point struct {
	Lon, Lat float64
	Alt      float64
	HasAlt   bool
}

// Ring is a closed ring (first position equals last).
type Ring []Pos

// Polygon is an outer ring followed by hole rings.
type Polygon []Ring

// Area is a Polygon or MultiPolygon.
type Area struct {
	Polygons []Polygon
	Multi    bool // the GeoJSON type is MultiPolygon

	minLon, minLat, maxLon, maxLat float64
}

// Raw is a canonical GeoJSON geometry text. It marshals as a JSON object (not
// as a string), so API values, history and events carry the geometry itself.
type Raw string

// MarshalJSON emits the geometry. Callers only create a Raw from valid JSON.
func (r Raw) MarshalJSON() ([]byte, error) { return []byte(r), nil }

// String returns the GeoJSON text.
func (r Raw) String() string { return string(r) }

// Object decodes the geometry into generic JSON values (nil when invalid).
func (r Raw) Object() map[string]any {
	var m map[string]any
	if json.Unmarshal([]byte(r), &m) != nil {
		return nil
	}
	return m
}

// ErrAntimeridian is wrapped by errors for areas crossing ±180° longitude.
var ErrAntimeridian = errors.New("areas that cross the 180° meridian are not supported; split the area at the meridian into separate polygons")

func fail(format string, args ...any) error { return fmt.Errorf(format, args...) }

func round7(x float64) float64 {
	r := math.Round(x*1e7) / 1e7
	if r == 0 {
		return 0 // -0
	}
	return r
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func checkLatLon(lat, lon float64) error {
	switch {
	case !finite(lat) || !finite(lon):
		return fail("coordinates must be finite numbers")
	case lat < -90 || lat > 90:
		return fail("latitude must be between -90 and 90")
	case lon < -180 || lon > 180:
		return fail("longitude must be between -180 and 180")
	}
	return nil
}

// NewPoint validates and normalises a point.
func NewPoint(lat, lon float64) (Point, error) {
	if err := checkLatLon(lat, lon); err != nil {
		return Point{}, err
	}
	return Point{Lat: round7(lat), Lon: round7(lon)}, nil
}

// ---- parsing ---------------------------------------------------------------

// inputText converts any accepted input to text: strings as they are,
// everything else marshalled to JSON.
func inputText(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", fail("a value is required")
	case string:
		return x, nil
	case Raw:
		return string(x), nil
	case []byte:
		return string(x), nil
	case json.RawMessage:
		return string(x), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fail("the value is not valid GeoJSON")
	}
	return string(b), nil
}

// ParsePoint accepts a GeoJSON Point (object or text), a Feature holding one,
// {"lat":..,"lon":..}, the text "lat, lon[, altitude]" and WKT POINT(lon lat).
func ParsePoint(v any) (Point, error) {
	s, err := inputText(v)
	if err != nil {
		return Point{}, err
	}
	if len(s) > MaxJSONBytes {
		return Point{}, fail("the value is too large")
	}
	return parsePointText(strings.TrimSpace(s))
}

func parsePointText(s string) (Point, error) {
	if s == "" {
		return Point{}, fail("a location is required")
	}
	if s[0] == '{' {
		return parsePointObject(s, 0)
	}
	if len(s) >= 5 && strings.EqualFold(s[:5], "POINT") {
		return parseWKTPoint(s)
	}
	parts := strings.Split(s, ",")
	if len(parts) < 2 || len(parts) > 3 {
		return Point{}, fail("a location must be written as \"latitude, longitude\"")
	}
	nums := make([]float64, len(parts))
	for i, p := range parts {
		f, err := parseNumber(p)
		if err != nil {
			return Point{}, fail("a location must be written as \"latitude, longitude\"")
		}
		nums[i] = f
	}
	p, err := NewPoint(nums[0], nums[1])
	if err != nil {
		return Point{}, err
	}
	if len(nums) == 3 {
		return withAlt(p, nums[2])
	}
	return p, nil
}

func withAlt(p Point, alt float64) (Point, error) {
	if !finite(alt) {
		return Point{}, fail("altitude must be a finite number")
	}
	p.Alt, p.HasAlt = round7(alt), true
	return p, nil
}

func parseNumber(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 || strings.ContainsAny(s, "xXpP_ ") {
		return 0, errors.New("not a number")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || !finite(f) {
		return 0, errors.New("not a number")
	}
	return f, nil
}

func parseWKTPoint(s string) (Point, error) {
	rest := strings.TrimSpace(s[5:])
	if len(rest) < 2 || rest[0] != '(' || rest[len(rest)-1] != ')' {
		return Point{}, fail("WKT points must look like POINT(longitude latitude)")
	}
	f := strings.Fields(rest[1 : len(rest)-1])
	if len(f) < 2 || len(f) > 3 {
		return Point{}, fail("WKT points must look like POINT(longitude latitude)")
	}
	nums := make([]float64, len(f))
	for i, p := range f {
		n, err := parseNumber(p)
		if err != nil {
			return Point{}, fail("WKT points must look like POINT(longitude latitude)")
		}
		nums[i] = n
	}
	p, err := NewPoint(nums[1], nums[0])
	if err != nil {
		return Point{}, err
	}
	if len(nums) == 3 {
		return withAlt(p, nums[2])
	}
	return p, nil
}

type object map[string]json.RawMessage

func parseObject(s string) (object, error) {
	var o object
	if err := json.Unmarshal([]byte(s), &o); err != nil || o == nil {
		return nil, fail("the value is not valid GeoJSON")
	}
	return o, nil
}

func (o object) str(key string) string {
	var s string
	if raw, ok := o[key]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// numberOf reads a JSON number or numeric string.
func numberOf(raw json.RawMessage) (float64, bool) {
	var f float64
	if json.Unmarshal(raw, &f) == nil && finite(f) {
		return f, true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if f, err := parseNumber(s); err == nil {
			return f, true
		}
	}
	return 0, false
}

func parsePointObject(s string, depth int) (Point, error) {
	o, err := parseObject(s)
	if err != nil {
		return Point{}, err
	}
	switch t := o.str("type"); t {
	case "Feature":
		g, ok := o["geometry"]
		if !ok || depth > 0 {
			return Point{}, fail("the Feature has no geometry")
		}
		return parsePointObject(string(g), depth+1)
	case "Point":
		var c []json.RawMessage
		if json.Unmarshal(o["coordinates"], &c) != nil || len(c) < 2 || len(c) > 3 {
			return Point{}, fail("a Point needs coordinates [longitude, latitude]")
		}
		nums := make([]float64, len(c))
		for i, r := range c {
			f, ok := numberOf(r)
			if !ok {
				return Point{}, fail("a Point needs coordinates [longitude, latitude]")
			}
			nums[i] = f
		}
		p, err := NewPoint(nums[1], nums[0])
		if err != nil {
			return Point{}, err
		}
		if len(nums) == 3 {
			return withAlt(p, nums[2])
		}
		return p, nil
	case "":
		latRaw, hasLat := o["lat"]
		lonRaw, hasLon := o["lon"]
		if !hasLon {
			lonRaw, hasLon = o["lng"]
		}
		if !hasLat || !hasLon {
			return Point{}, fail("a location needs lat and lon, or GeoJSON of type Point")
		}
		lat, ok1 := numberOf(latRaw)
		lon, ok2 := numberOf(lonRaw)
		if !ok1 || !ok2 {
			return Point{}, fail("lat and lon must be numbers")
		}
		p, err := NewPoint(lat, lon)
		if err != nil {
			return Point{}, err
		}
		if altRaw, ok := o["alt"]; ok && string(altRaw) != "null" {
			alt, ok := numberOf(altRaw)
			if !ok {
				return Point{}, fail("alt must be a number")
			}
			return withAlt(p, alt)
		}
		return p, nil
	default:
		return Point{}, fail("expected a Point but found a %s", t)
	}
}

// ParseArea accepts a GeoJSON Polygon or MultiPolygon (object or text), or a
// Feature holding one. It normalises: coordinates rounded to 7 decimals, rings
// closed, outer rings counter-clockwise and holes clockwise, altitudes dropped.
func ParseArea(v any) (*Area, error) {
	s, err := inputText(v)
	if err != nil {
		return nil, err
	}
	if len(s) > MaxJSONBytes {
		return nil, fail("the area is larger than 1 MiB of GeoJSON")
	}
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '{' {
		return nil, fail("an area must be GeoJSON of type Polygon or MultiPolygon")
	}
	return parseAreaObject(s, 0)
}

func parseAreaObject(s string, depth int) (*Area, error) {
	o, err := parseObject(s)
	if err != nil {
		return nil, err
	}
	var polys [][][][]float64
	multi := false
	switch t := o.str("type"); t {
	case "Feature":
		g, ok := o["geometry"]
		if !ok || depth > 0 {
			return nil, fail("the Feature has no geometry")
		}
		return parseAreaObject(string(g), depth+1)
	case "Polygon":
		var p [][][]float64
		if json.Unmarshal(o["coordinates"], &p) != nil {
			return nil, fail("a Polygon needs coordinates: a list of rings of [longitude, latitude] positions")
		}
		polys = [][][][]float64{p}
	case "MultiPolygon":
		multi = true
		if json.Unmarshal(o["coordinates"], &polys) != nil {
			return nil, fail("a MultiPolygon needs coordinates: a list of polygons")
		}
	case "":
		return nil, fail("an area must be GeoJSON of type Polygon or MultiPolygon")
	default:
		return nil, fail("expected a Polygon or MultiPolygon but found a %s", t)
	}
	if len(polys) == 0 {
		return nil, fail("the area has no polygons")
	}
	a := &Area{Multi: multi}
	total := 0
	for _, rings := range polys {
		if len(rings) == 0 {
			return nil, fail("a polygon needs an outer ring")
		}
		var poly Polygon
		for ri, raw := range rings {
			ring, err := normaliseRing(raw, ri == 0)
			if err != nil {
				return nil, err
			}
			total += len(ring)
			if total > MaxPositions {
				return nil, fail("an area may have at most %d positions", MaxPositions)
			}
			poly = append(poly, ring)
		}
		a.Polygons = append(a.Polygons, poly)
	}
	a.computeBBox()
	if len(a.JSON()) > MaxJSONBytes {
		return nil, fail("the area is larger than 1 MiB of GeoJSON")
	}
	return a, nil
}

func normaliseRing(raw [][]float64, outer bool) (Ring, error) {
	if len(raw) > MaxPositions {
		return nil, fail("an area may have at most %d positions", MaxPositions)
	}
	ring := make(Ring, 0, len(raw)+1)
	for _, p := range raw {
		if len(p) < 2 || len(p) > 3 {
			return nil, fail("positions must be [longitude, latitude]")
		}
		if err := checkLatLon(p[1], p[0]); err != nil {
			return nil, err
		}
		ring = append(ring, Pos{round7(p[0]), round7(p[1])})
	}
	if len(ring) > 0 && ring[0] != ring[len(ring)-1] {
		ring = append(ring, ring[0])
	}
	if len(ring) < 4 {
		return nil, fail("each ring needs at least 4 positions (3 corners and the closing position)")
	}
	for i := 1; i < len(ring); i++ {
		if math.Abs(ring[i].Lon-ring[i-1].Lon) > 180 {
			return nil, ErrAntimeridian
		}
	}
	sa := signedPlanarArea(ring)
	if sa == 0 {
		return nil, fail("a ring must enclose an area (its positions are in a line or repeat)")
	}
	// Outer rings are counter-clockwise (positive), holes clockwise.
	if (sa > 0) != outer {
		for i, j := 0, len(ring)-1; i < j; i, j = i+1, j-1 {
			ring[i], ring[j] = ring[j], ring[i]
		}
	}
	return ring, nil
}

// signedPlanarArea is the shoelace area in square degrees (positive when counter-clockwise).
func signedPlanarArea(r Ring) float64 {
	o := r[0]
	var s float64
	for i := 1; i < len(r); i++ {
		a, b := r[i-1], r[i]
		s += (a.Lon-o.Lon)*(b.Lat-o.Lat) - (b.Lon-o.Lon)*(a.Lat-o.Lat)
	}
	return s / 2
}

func (a *Area) computeBBox() {
	a.minLon, a.minLat, a.maxLon, a.maxLat = 180, 90, -180, -90
	for _, poly := range a.Polygons {
		for _, p := range poly[0] {
			a.minLon, a.maxLon = math.Min(a.minLon, p.Lon), math.Max(a.maxLon, p.Lon)
			a.minLat, a.maxLat = math.Min(a.minLat, p.Lat), math.Max(a.maxLat, p.Lat)
		}
	}
}

// ---- formatting ------------------------------------------------------------

func appendNum(b []byte, x float64) []byte { return strconv.AppendFloat(b, x, 'f', -1, 64) }

// JSON returns the canonical GeoJSON text of p.
func (p Point) JSON() string {
	b := append(make([]byte, 0, 64), `{"type":"Point","coordinates":[`...)
	b = appendNum(b, p.Lon)
	b = append(b, ',')
	b = appendNum(b, p.Lat)
	if p.HasAlt {
		b = append(b, ',')
		b = appendNum(b, p.Alt)
	}
	return string(append(b, "]}"...))
}

// Text formats p as "lat, lon" (with ", altitude" when present).
func (p Point) Text() string {
	s := string(appendNum(nil, p.Lat)) + ", " + string(appendNum(nil, p.Lon))
	if p.HasAlt {
		s += ", " + string(appendNum(nil, p.Alt))
	}
	return s
}

// JSON returns the canonical GeoJSON text of a.
func (a *Area) JSON() string {
	b := make([]byte, 0, 256)
	if a.Multi {
		b = append(b, `{"type":"MultiPolygon","coordinates":[`...)
	} else {
		b = append(b, `{"type":"Polygon","coordinates":`...)
	}
	for i, poly := range a.Polygons {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '[')
		for j, ring := range poly {
			if j > 0 {
				b = append(b, ',')
			}
			b = append(b, '[')
			for k, p := range ring {
				if k > 0 {
					b = append(b, ',')
				}
				b = append(b, '[')
				b = appendNum(b, p.Lon)
				b = append(b, ',')
				b = appendNum(b, p.Lat)
				b = append(b, ']')
			}
			b = append(b, ']')
		}
		b = append(b, ']')
	}
	if a.Multi {
		b = append(b, ']')
	}
	return string(append(b, '}'))
}

// Positions counts the positions in a (closing positions included).
func (a *Area) Positions() int {
	n := 0
	for _, poly := range a.Polygons {
		for _, r := range poly {
			n += len(r)
		}
	}
	return n
}

// NormalizePoint parses v and returns the canonical GeoJSON text.
func NormalizePoint(v any) (string, error) {
	p, err := ParsePoint(v)
	if err != nil {
		return "", err
	}
	return p.JSON(), nil
}

// NormalizeArea parses v and returns the canonical GeoJSON text.
func NormalizeArea(v any) (string, error) {
	a, err := ParseArea(v)
	if err != nil {
		return "", err
	}
	return a.JSON(), nil
}

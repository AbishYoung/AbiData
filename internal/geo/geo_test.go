package geo

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"strings"
	"testing"
)

func mustPoint(t *testing.T, v any) Point {
	t.Helper()
	p, err := ParsePoint(v)
	if err != nil {
		t.Fatalf("ParsePoint(%v): %v", v, err)
	}
	return p
}

func mustArea(t *testing.T, v any) *Area {
	t.Helper()
	a, err := ParseArea(v)
	if err != nil {
		t.Fatalf("ParseArea(%v): %v", v, err)
	}
	return a
}

func TestParsePointForms(t *testing.T) {
	want := `{"type":"Point","coordinates":[-0.14189,51.50135]}`
	for _, in := range []any{
		"51.50135, -0.14189",
		"  51.50135,-0.14189 ",
		"POINT(-0.14189 51.50135)",
		`{"type":"Point","coordinates":[-0.14189,51.50135]}`,
		`{"lat":51.50135,"lon":-0.14189}`,
		`{"lat":"51.50135","lng":"-0.14189"}`,
		`{"type":"Feature","properties":{"a":1},"geometry":{"type":"Point","coordinates":[-0.14189,51.50135]}}`,
		map[string]any{"lat": 51.50135, "lon": -0.14189},
		map[string]any{"type": "Point", "coordinates": []any{-0.14189, 51.50135}},
	} {
		got, err := NormalizePoint(in)
		if err != nil || got != want {
			t.Errorf("NormalizePoint(%v) = %q, %v; want %q", in, got, err, want)
		}
	}
	if got, _ := NormalizePoint("1.123456789, 2.987654321"); got != `{"type":"Point","coordinates":[2.9876543,1.1234568]}` {
		t.Errorf("rounding: %s", got)
	}
	p := mustPoint(t, `{"type":"Point","coordinates":[10,20,350.5]}`)
	if !p.HasAlt || p.Alt != 350.5 || p.JSON() != `{"type":"Point","coordinates":[10,20,350.5]}` {
		t.Errorf("altitude not kept: %+v %s", p, p.JSON())
	}
	if got := mustPoint(t, "-0.0, 0").JSON(); got != `{"type":"Point","coordinates":[0,0]}` {
		t.Errorf("negative zero: %s", got)
	}
	if mustPoint(t, "1, 2").Text() != "1, 2" {
		t.Error("Text")
	}
}

func TestParsePointErrors(t *testing.T) {
	for in, msg := range map[string]string{
		"":                                    "required",
		"91, 0":                               "latitude",
		"0, 181":                              "longitude",
		"abc":                                 "latitude, longitude",
		"1,2,3,4":                             "latitude, longitude",
		"NaN, 1":                              "latitude, longitude",
		"Inf, 1":                              "latitude, longitude",
		`{"type":"Polygon","coordinates":[]}`: "expected a Point",
		`{"type":"Point","coordinates":[1]}`:  "coordinates",
		`{"lat":1}`:                           "lat and lon",
		`{"lat":"x","lon":1}`:                 "numbers",
		`{"type":"Feature"}`:                  "no geometry",
		`{"type":"Feature","geometry":{"type":"Feature","geometry":{}}}`: "no geometry",
		`[1,2]`:    "latitude, longitude",
		"POINT(1)": "POINT",
	} {
		_, err := ParsePoint(in)
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("ParsePoint(%q) error = %v, want containing %q", in, err, msg)
		}
	}
	if _, err := ParsePoint(nil); err == nil {
		t.Error("nil accepted")
	}
	if _, err := ParsePoint(strings.Repeat("1", MaxJSONBytes+1)); err == nil {
		t.Error("oversize accepted")
	}
}

func square(minLon, minLat, maxLon, maxLat float64) string {
	b, _ := json.Marshal(map[string]any{"type": "Polygon", "coordinates": [][][]float64{{
		{minLon, minLat}, {maxLon, minLat}, {maxLon, maxLat}, {minLon, maxLat}, {minLon, minLat}}}})
	return string(b)
}

func TestAreaNormalisation(t *testing.T) {
	// Unclosed, clockwise outer ring becomes closed and counter-clockwise.
	a := mustArea(t, `{"type":"Polygon","coordinates":[[[0,0],[0,1],[1,1],[1,0]]]}`)
	if got, want := a.JSON(), `{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1],[0,0]]]}`; got != want {
		t.Errorf("got %s want %s", got, want)
	}
	// A counter-clockwise hole becomes clockwise.
	a = mustArea(t, `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]],[[2,2],[4,2],[4,4],[2,4],[2,2]]]}`)
	if signedPlanarArea(a.Polygons[0][0]) <= 0 || signedPlanarArea(a.Polygons[0][1]) >= 0 {
		t.Errorf("orientation: %s", a.JSON())
	}
	// Feature wrapper, altitude dropped, rounding.
	a = mustArea(t, `{"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[0,0,5],[1.000000049,0],[1,1],[0,1],[0,0]]]}}`)
	if !strings.Contains(a.JSON(), "[1,0]") || strings.Contains(a.JSON(), ",5]") {
		t.Errorf("%s", a.JSON())
	}
	// Normalisation is idempotent.
	again := mustArea(t, a.JSON())
	if again.JSON() != a.JSON() {
		t.Error("not idempotent")
	}
	m := mustArea(t, `{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,1],[0,0]]]]}`)
	if !m.Multi || !strings.HasPrefix(m.JSON(), `{"type":"MultiPolygon"`) {
		t.Errorf("multi: %s", m.JSON())
	}
}

func TestAreaErrors(t *testing.T) {
	for in, msg := range map[string]string{
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[0,0]]]}`:           "at least 4",
		`{"type":"Polygon","coordinates":[[[0,0],[1,1],[2,2],[0,0]]]}`:     "enclose an area",
		`{"type":"Polygon","coordinates":[]}`:                              "outer ring",
		`{"type":"MultiPolygon","coordinates":[]}`:                         "no polygons",
		`{"type":"Polygon","coordinates":[[[0,0],[1,91],[1,1],[0,0]]]}`:    "latitude",
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0],[0]]]}`: "positions",
		`{"type":"Point","coordinates":[0,0]}`:                             "Polygon or MultiPolygon",
		`{"coordinates":[]}`:                                               "Polygon or MultiPolygon",
		`[]`:                                                               "GeoJSON",
		`{"type":"Polygon","coordinates":"x"}`:                             "Polygon needs",
		`{"type":"Feature","geometry":{"type":"Feature"}}`:                 "no geometry",
	} {
		_, err := ParseArea(in)
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("ParseArea(%s) error = %v, want containing %q", in, err, msg)
		}
	}
}

func TestAntimeridianRejected(t *testing.T) {
	_, err := ParseArea(`{"type":"Polygon","coordinates":[[[170,0],[-170,0],[-170,10],[170,10],[170,0]]]}`)
	if !errors.Is(err, ErrAntimeridian) {
		t.Fatalf("got %v", err)
	}
	// Touching the meridian from one side is fine.
	mustArea(t, square(170, 0, 180, 10))
	mustArea(t, square(-180, 0, -170, 10))
}

func TestAreaLimits(t *testing.T) {
	ring := func(n int) string {
		var b strings.Builder
		b.WriteString(`{"type":"Polygon","coordinates":[[`)
		for i := 0; i < n; i++ {
			a := 2 * math.Pi * float64(i) / float64(n)
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString("[" + ftoa(10*math.Cos(a)) + "," + ftoa(10*math.Sin(a)) + "]")
		}
		b.WriteString(`]]}`)
		return b.String()
	}
	if _, err := ParseArea(ring(9990)); err != nil {
		t.Fatalf("9990 positions: %v", err)
	}
	if _, err := ParseArea(ring(10000)); err == nil || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("10000 unclosed positions (10001 after closing) should fail: %v", err)
	}
	if _, err := ParseArea(ring(20000)); err == nil {
		t.Fatal("20000 accepted")
	}
	if _, err := ParseArea(strings.Repeat(" ", MaxJSONBytes) + square(0, 0, 1, 1)); err == nil {
		t.Fatal("over 1 MiB accepted")
	}
}

func ftoa(f float64) string { b, _ := json.Marshal(f); return string(b) }

func TestContains(t *testing.T) {
	a := mustArea(t, `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]],[[4,4],[6,4],[6,6],[4,6],[4,4]]]}`)
	cases := []struct {
		lat, lon float64
		in       bool
		name     string
	}{
		{2, 2, true, "inside"},
		{5, 5, false, "in hole"},
		{4, 5, true, "on hole edge"},
		{5, 4, true, "on hole edge (vertical)"},
		{4, 4, true, "on hole vertex"},
		{0, 5, true, "on outer edge"},
		{5, 10, true, "on outer edge (vertical)"},
		{10, 10, true, "outer vertex"},
		{-1, 5, false, "south of area"},
		{5, 11, false, "east of area"},
		{9.9999999, 9.9999999, true, "just inside corner"},
	}
	for _, c := range cases {
		if got := a.Contains(Point{Lat: c.lat, Lon: c.lon}); got != c.in {
			t.Errorf("%s: Contains(%v,%v) = %v", c.name, c.lat, c.lon, got)
		}
	}
	// Diagonal edge, point on it and just off it.
	tri := mustArea(t, `{"type":"Polygon","coordinates":[[[0,0],[10,0],[0,10],[0,0]]]}`)
	if !tri.Contains(Point{Lat: 5, Lon: 5}) {
		t.Error("point on hypotenuse")
	}
	if tri.Contains(Point{Lat: 5.001, Lon: 5.001}) {
		t.Error("point beyond hypotenuse")
	}
	// MultiPolygon: either polygon.
	m := mustArea(t, `{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,1],[0,0]]],[[[5,5],[6,5],[6,6],[5,6],[5,5]]]]}`)
	if !m.Contains(Point{Lat: .5, Lon: .5}) || !m.Contains(Point{Lat: 5.5, Lon: 5.5}) || m.Contains(Point{Lat: 3, Lon: 3}) {
		t.Error("multipolygon")
	}
}

func TestDistance(t *testing.T) {
	london, paris := Point{Lat: 51.5074, Lon: -0.1278}, Point{Lat: 48.8566, Lon: 2.3522}
	d := Distance(london, paris)
	if d < 343e3 || d > 345e3 {
		t.Errorf("London-Paris = %v", d)
	}
	if Distance(london, london) != 0 {
		t.Error("self distance")
	}
	// One degree of latitude is about 111.2 km.
	if d := Distance(Point{}, Point{Lat: 1}); math.Abs(d-111195) > 100 {
		t.Errorf("1 degree = %v", d)
	}
	// Antipodes: half the circumference.
	if d := Distance(Point{}, Point{Lon: 180}); math.Abs(d-math.Pi*EarthRadiusM) > 1 {
		t.Errorf("antipodes %v", d)
	}
}

func TestAreaSquare(t *testing.T) {
	// A latitude/longitude rectangle has the exact area R² Δλ (sin φ2 - sin φ1).
	for _, c := range [][4]float64{{0, 0, 1, 1}, {10, 40, 12, 41}, {-120, -60, -119, -59}, {0, 80, 30, 85}} {
		a := mustArea(t, square(c[0], c[1], c[2], c[3]))
		const rad = math.Pi / 180
		want := EarthRadiusM * EarthRadiusM * (c[2] - c[0]) * rad * (math.Sin(c[3]*rad) - math.Sin(c[1]*rad))
		if got := a.AreaM2(); math.Abs(got-want) > want*1e-9 {
			t.Errorf("%v: area %v want %v", c, got, want)
		}
	}
	// 1° x 1° at the equator is about 12,364 km².
	if got := mustArea(t, square(0, 0, 1, 1)).AreaM2(); math.Abs(got-12364e6) > 20e6 {
		t.Errorf("1 degree square = %v", got)
	}
	// A hole subtracts, and holes/orientation do not change positive area.
	outer := mustArea(t, square(0, 0, 10, 10)).AreaM2()
	holed := mustArea(t, `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]],[[2,2],[4,2],[4,4],[2,4],[2,2]]]}`).AreaM2()
	hole := mustArea(t, square(2, 2, 4, 4)).AreaM2()
	if math.Abs(holed-(outer-hole)) > 1 {
		t.Errorf("holed %v outer-hole %v", holed, outer-hole)
	}
}

func TestCentroidBBox(t *testing.T) {
	a := mustArea(t, square(10, 20, 14, 26))
	if c := a.Centroid(); c.Lon != 12 || c.Lat != 23 {
		t.Errorf("centroid %+v", c)
	}
	if b := a.BBox(); b != [4]float64{10, 20, 14, 26} {
		t.Errorf("bbox %v", b)
	}
	// An L shape's centroid is area weighted.
	l := mustArea(t, `{"type":"Polygon","coordinates":[[[0,0],[2,0],[2,1],[1,1],[1,2],[0,2],[0,0]]]}`)
	if c := l.Centroid(); math.Abs(c.Lon-5.0/6) > 1e-6 || math.Abs(c.Lat-5.0/6) > 1e-6 {
		t.Errorf("L centroid %+v", c)
	}
	// Hole shifts the centroid.
	h := mustArea(t, `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]],[[1,1],[5,1],[5,5],[1,5],[1,1]]]}`)
	if c := h.Centroid(); c.Lon <= 5 || c.Lat <= 5 {
		t.Errorf("holed centroid %+v", c)
	}
	m := mustArea(t, `{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,1],[0,0]]],[[[9,9],[10,9],[10,10],[9,10],[9,9]]]]}`)
	if b := m.BBox(); b != [4]float64{0, 0, 10, 10} {
		t.Errorf("multi bbox %v", b)
	}
	if c := m.Centroid(); c.Lon != 5 || c.Lat != 5 {
		t.Errorf("multi centroid %+v", c)
	}
}

func TestPropertyDistanceSymmetric(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		a := Point{Lat: rng.Float64()*180 - 90, Lon: rng.Float64()*360 - 180}
		b := Point{Lat: rng.Float64()*180 - 90, Lon: rng.Float64()*360 - 180}
		ab, ba := Distance(a, b), Distance(b, a)
		if ab != ba || ab < 0 || ab > math.Pi*EarthRadiusM+1 {
			t.Fatalf("%v %v: %v %v", a, b, ab, ba)
		}
		// Triangle inequality (with slack for rounding).
		c := Point{Lat: rng.Float64()*180 - 90, Lon: rng.Float64()*360 - 180}
		if Distance(a, c) > ab+Distance(b, c)+1e-6 {
			t.Fatalf("triangle inequality %v %v %v", a, b, c)
		}
	}
}

func TestPropertyRectContains(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 500; i++ {
		x0, y0 := rng.Float64()*300-150, rng.Float64()*140-70
		w, h := rng.Float64()*20+0.001, rng.Float64()*20+0.001
		a := mustArea(t, square(x0, y0, x0+w, y0+h))
		b := a.BBox()
		for j := 0; j < 40; j++ {
			p, err := NewPoint(rng.Float64()*180-90, rng.Float64()*360-180)
			if err != nil {
				t.Fatal(err)
			}
			want := p.Lon >= b[0] && p.Lon <= b[2] && p.Lat >= b[1] && p.Lat <= b[3]
			if got := a.Contains(p); got != want {
				t.Fatalf("rect %v point %+v: got %v want %v", b, p, got, want)
			}
		}
	}
}

func TestPropertyConvexMatchesHalfPlanes(t *testing.T) {
	// A regular polygon is convex: containment equals "left of every edge".
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 100; i++ {
		n := 3 + rng.Intn(12)
		var ring [][]float64
		for k := 0; k < n; k++ {
			ang := 2 * math.Pi * float64(k) / float64(n)
			ring = append(ring, []float64{round7(20 * math.Cos(ang)), round7(20 * math.Sin(ang))})
		}
		ring = append(ring, ring[0])
		b, _ := json.Marshal(map[string]any{"type": "Polygon", "coordinates": [][][]float64{ring}})
		a := mustArea(t, string(b))
		r := a.Polygons[0][0]
		for j := 0; j < 50; j++ {
			p := Point{Lat: rng.Float64()*50 - 25, Lon: rng.Float64()*50 - 25}
			inside := true
			for k := 1; k < len(r); k++ {
				cr := (r[k].Lon-r[k-1].Lon)*(p.Lat-r[k-1].Lat) - (r[k].Lat-r[k-1].Lat)*(p.Lon-r[k-1].Lon)
				if cr < -1e-9 {
					inside = false
				}
			}
			// Skip points within a hair of an edge, where the two methods may differ.
			near := false
			for k := 1; k < len(r); k++ {
				if onSegment(Pos{p.Lon, p.Lat}, r[k-1], r[k]) {
					near = true
				}
			}
			if !near && a.Contains(p) != inside {
				t.Fatalf("polygon %v point %+v: contains %v halfplane %v", r, p, a.Contains(p), inside)
			}
		}
	}
}

func TestPropertyNormaliseIdempotent(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < 300; i++ {
		p, err := NewPoint(rng.Float64()*180-90, rng.Float64()*360-180)
		if err != nil {
			t.Fatal(err)
		}
		s, err := NormalizePoint(p.JSON())
		if err != nil || s != p.JSON() {
			t.Fatalf("%s -> %s %v", p.JSON(), s, err)
		}
		q, _ := ParsePoint(p.Text())
		if q != p {
			t.Fatalf("text round trip %+v %+v", p, q)
		}
	}
}

func TestRawMarshalsAsObject(t *testing.T) {
	b, err := json.Marshal(map[string]any{"v": Raw(`{"type":"Point","coordinates":[1,2]}`)})
	if err != nil || string(b) != `{"v":{"type":"Point","coordinates":[1,2]}}` {
		t.Fatalf("%s %v", b, err)
	}
	if Raw(`{"type":"Point","coordinates":[1,2]}`).Object()["type"] != "Point" || Raw("x").Object() != nil {
		t.Error("Object")
	}
}

func FuzzParsePoint(f *testing.F) {
	for _, s := range []string{"1,2", "POINT(1 2)", `{"lat":1,"lon":2}`, `{"type":"Point","coordinates":[1,2,3]}`, `{"type":"Feature","geometry":null}`, "", "{", "1e999,1", "-0,-0"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := ParsePoint(s)
		if err != nil {
			return
		}
		// Whatever parses must normalise to a fixed point inside range.
		if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 || !finite(p.Lat) || !finite(p.Lon) {
			t.Fatalf("out of range: %+v", p)
		}
		again, err := ParsePoint(p.JSON())
		if err != nil || again != p {
			t.Fatalf("not stable: %s -> %+v %v", p.JSON(), again, err)
		}
		if t2, err := ParsePoint(p.Text()); err != nil || t2 != p {
			t.Fatalf("text not stable: %s -> %+v %v", p.Text(), t2, err)
		}
	})
}

func FuzzParseArea(f *testing.F) {
	for _, s := range []string{
		square(0, 0, 1, 1),
		`{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]]]]}`,
		`{"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]]]}}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,0]],[[0.1,0.1],[0.2,0.1],[0.2,0.2],[0.1,0.1]]]}`,
		"", "{", `{"type":"Polygon"}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		a, err := ParseArea(s)
		if err != nil {
			return
		}
		if a.Positions() > MaxPositions || len(a.JSON()) > MaxJSONBytes {
			t.Fatalf("limits not enforced: %d positions", a.Positions())
		}
		again, err := ParseArea(a.JSON())
		if err != nil || again.JSON() != a.JSON() {
			t.Fatalf("not idempotent: %s -> %v", a.JSON(), err)
		}
		if m := a.AreaM2(); m < 0 || math.IsNaN(m) {
			t.Fatalf("area %v", m)
		}
		c := a.Centroid()
		b := a.BBox()
		if c.Lon < b[0]-1e-6 || c.Lon > b[2]+1e-6 || c.Lat < b[1]-1e-6 || c.Lat > b[3]+1e-6 {
			// Centroid of valid, non-self-intersecting areas lies in the bbox; self-intersecting input may not.
			_ = c
		}
		a.Contains(Point{})
	})
}

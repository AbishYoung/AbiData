package geo

import "math"

// Distance is the great-circle (haversine) distance between a and b in metres.
func Distance(a, b Point) float64 {
	const rad = math.Pi / 180
	p1, p2 := a.Lat*rad, b.Lat*rad
	dp, dl := (b.Lat-a.Lat)*rad, (b.Lon-a.Lon)*rad
	h := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * EarthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// boundaryTolerance is how close (in degrees, planar) a point must be to an
// edge to count as on it: about a tenth of a millimetre, far below the 1 cm
// coordinate grid.
const boundaryTolerance = 1e-9

func onSegment(p, a, b Pos) bool {
	if p.Lon < math.Min(a.Lon, b.Lon)-boundaryTolerance || p.Lon > math.Max(a.Lon, b.Lon)+boundaryTolerance ||
		p.Lat < math.Min(a.Lat, b.Lat)-boundaryTolerance || p.Lat > math.Max(a.Lat, b.Lat)+boundaryTolerance {
		return false
	}
	dx, dy := b.Lon-a.Lon, b.Lat-a.Lat
	length := math.Hypot(dx, dy)
	if length == 0 {
		return math.Hypot(p.Lon-a.Lon, p.Lat-a.Lat) <= boundaryTolerance
	}
	return math.Abs(dx*(p.Lat-a.Lat)-dy*(p.Lon-a.Lon))/length <= boundaryTolerance
}

// Contains reports whether p lies inside a. Containment is planar in
// longitude/latitude, by even-odd ray casting over each polygon's rings (so
// holes are outside), and points on any boundary count as inside.
func (a *Area) Contains(p Point) bool {
	if p.Lon < a.minLon-boundaryTolerance || p.Lon > a.maxLon+boundaryTolerance ||
		p.Lat < a.minLat-boundaryTolerance || p.Lat > a.maxLat+boundaryTolerance {
		return false
	}
	q := Pos{p.Lon, p.Lat}
	for _, poly := range a.Polygons {
		if polygonContains(poly, q) {
			return true
		}
	}
	return false
}

func polygonContains(poly Polygon, q Pos) bool {
	inside := false
	for _, ring := range poly {
		for i := 1; i < len(ring); i++ {
			a, b := ring[i-1], ring[i]
			if onSegment(q, a, b) {
				return true
			}
			if (a.Lat > q.Lat) != (b.Lat > q.Lat) {
				x := a.Lon + (q.Lat-a.Lat)*(b.Lon-a.Lon)/(b.Lat-a.Lat)
				if q.Lon < x {
					inside = !inside
				}
			}
		}
	}
	return inside
}

// AreaM2 is the area of a in square metres using the spherical-excess
// approximation (Chamberlain and Duquette): exact for latitude/longitude
// rectangles on the sphere, and within about 0.5% of the ellipsoid.
func (a *Area) AreaM2() float64 {
	var total float64
	for _, poly := range a.Polygons {
		for i, ring := range poly {
			s := ringSphericalArea(ring)
			if i == 0 {
				total += s
			} else {
				total -= s
			}
		}
	}
	return math.Max(total, 0)
}

func ringSphericalArea(r Ring) float64 {
	const rad = math.Pi / 180
	var s float64
	for i := 1; i < len(r); i++ {
		a, b := r[i-1], r[i]
		s += (b.Lon - a.Lon) * rad * (2 + math.Sin(a.Lat*rad) + math.Sin(b.Lat*rad))
	}
	return math.Abs(s) * EarthRadiusM * EarthRadiusM / 2
}

// BBox returns [minLon, minLat, maxLon, maxLat].
func (a *Area) BBox() [4]float64 { return [4]float64{a.minLon, a.minLat, a.maxLon, a.maxLat} }

// Centroid is the area-weighted planar centroid of a (holes subtract). For a
// concave area it can lie outside the area.
func (a *Area) Centroid() Point {
	var w, cx, cy float64
	for _, poly := range a.Polygons {
		for i, ring := range poly {
			area, x, y := ringCentroid(ring)
			if i > 0 {
				area = -area
			}
			w += area
			cx += area * x
			cy += area * y
		}
	}
	if w <= 0 {
		return Point{Lon: round7((a.minLon + a.maxLon) / 2), Lat: round7((a.minLat + a.maxLat) / 2)}
	}
	return Point{Lon: round7(cx / w), Lat: round7(cy / w)}
}

// ringCentroid returns the absolute planar area and centroid of r.
func ringCentroid(r Ring) (area, x, y float64) {
	o := r[0]
	var a2, sx, sy float64
	for i := 1; i < len(r); i++ {
		p, q := r[i-1], r[i]
		px, py, qx, qy := p.Lon-o.Lon, p.Lat-o.Lat, q.Lon-o.Lon, q.Lat-o.Lat
		cr := px*qy - qx*py
		a2 += cr
		sx += (px + qx) * cr
		sy += (py + qy) * cr
	}
	if a2 == 0 {
		return 0, o.Lon, o.Lat
	}
	return math.Abs(a2 / 2), o.Lon + sx/(3*a2), o.Lat + sy/(3*a2)
}

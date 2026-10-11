package sqlitex

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/geo"
)

// GeoFunctionNames lists the SQL functions registered on every connection
// (ADR 0010). They are deterministic and innocuous, so they may be used in
// calculated fields, CHECK constraints and views even with trusted_schema=OFF.
// Every one returns NULL for invalid input rather than raising an error.
var GeoFunctionNames = []string{
	"geo_point", "geo_lat", "geo_lon", "geo_distance_m", "geo_within", "geo_area_m2", "geo_centroid", "geo_bbox",
}

// areaCache keeps recently parsed areas so a spatial join, which tests many
// points against the same few areas, parses each area once. It is bounded by
// the bytes of JSON it holds.
type areaCache struct {
	mu    sync.Mutex
	m     map[string]*geo.Area
	bytes int
}

const areaCacheBytes = 8 << 20

func (c *areaCache) get(s string) *geo.Area {
	c.mu.Lock()
	if a, ok := c.m[s]; ok {
		c.mu.Unlock()
		return a
	}
	c.mu.Unlock()
	a, err := geo.ParseArea(s)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || c.bytes+len(s) > areaCacheBytes {
		c.m, c.bytes = map[string]*geo.Area{}, 0
	}
	if len(s) <= areaCacheBytes {
		c.m[s] = a
		c.bytes += len(s)
	}
	return a
}

// badArea is auxiliary data remembering that a constant argument is not a valid area.
type badArea struct{}

// textArg returns a TEXT argument ("" and false for NULL and other types).
func textArg(v sqlite3.Value) (string, bool) {
	if v.Type() != sqlite3.TEXT {
		return "", false
	}
	return v.Text(), true
}

// floatArg reads a numeric argument; numeric text is accepted.
func floatArg(v sqlite3.Value) (float64, bool) {
	switch v.Type() {
	case sqlite3.INTEGER, sqlite3.FLOAT:
		return v.Float(), true
	case sqlite3.TEXT:
		f, err := strconv.ParseFloat(strings.TrimSpace(v.Text()), 64)
		return f, err == nil
	}
	return 0, false
}

func pointArg(v sqlite3.Value) (geo.Point, bool) {
	s, ok := textArg(v)
	if !ok {
		return geo.Point{}, false
	}
	p, err := geo.ParsePoint(s)
	return p, err == nil
}

// registerGeo installs the geo_* functions on c.
func registerGeo(c *sqlite3.Conn) error {
	const flags = sqlite3.DETERMINISTIC | sqlite3.INNOCUOUS
	cache := &areaCache{}
	// area parses argument n. SQLite keeps auxiliary data only for arguments
	// that are constant for the statement (a bound parameter or literal), so a
	// query that tests every row against one area parses it once.
	area := func(ctx sqlite3.Context, n int, v sqlite3.Value) *geo.Area {
		switch x := ctx.GetAuxData(n).(type) {
		case *geo.Area:
			return x
		case badArea:
			return nil
		}
		s, ok := textArg(v)
		if !ok {
			return nil
		}
		a := cache.get(s)
		if a == nil {
			ctx.SetAuxData(n, badArea{})
		} else {
			ctx.SetAuxData(n, a)
		}
		return a
	}
	fns := []struct {
		name string
		n    int
		fn   func(ctx sqlite3.Context, arg ...sqlite3.Value)
	}{
		{"geo_point", 2, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			lat, ok1 := floatArg(arg[0])
			lon, ok2 := floatArg(arg[1])
			if !ok1 || !ok2 {
				ctx.ResultNull()
				return
			}
			p, err := geo.NewPoint(lat, lon)
			if err != nil {
				ctx.ResultNull()
				return
			}
			ctx.ResultText(p.JSON())
		}},
		{"geo_lat", 1, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			if p, ok := pointArg(arg[0]); ok {
				ctx.ResultFloat(p.Lat)
			} else {
				ctx.ResultNull()
			}
		}},
		{"geo_lon", 1, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			if p, ok := pointArg(arg[0]); ok {
				ctx.ResultFloat(p.Lon)
			} else {
				ctx.ResultNull()
			}
		}},
		{"geo_distance_m", 2, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			a, ok1 := pointArg(arg[0])
			b, ok2 := pointArg(arg[1])
			if !ok1 || !ok2 {
				ctx.ResultNull()
				return
			}
			ctx.ResultFloat(geo.Distance(a, b))
		}},
		{"geo_within", 2, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			p, ok := pointArg(arg[0])
			a := area(ctx, 1, arg[1])
			if !ok || a == nil {
				ctx.ResultNull()
				return
			}
			ctx.ResultBool(a.Contains(p))
		}},
		{"geo_area_m2", 1, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			if a := area(ctx, 0, arg[0]); a != nil {
				ctx.ResultFloat(a.AreaM2())
			} else {
				ctx.ResultNull()
			}
		}},
		{"geo_centroid", 1, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			if a := area(ctx, 0, arg[0]); a != nil {
				ctx.ResultText(a.Centroid().JSON())
			} else {
				ctx.ResultNull()
			}
		}},
		{"geo_bbox", 1, func(ctx sqlite3.Context, arg ...sqlite3.Value) {
			a := area(ctx, 0, arg[0])
			if a == nil {
				ctx.ResultNull()
				return
			}
			b := a.BBox()
			out, err := json.Marshal(b[:])
			if err != nil {
				ctx.ResultNull()
				return
			}
			ctx.ResultText(string(out))
		}},
	}
	for _, f := range fns {
		if err := c.CreateFunction(f.name, f.n, flags, f.fn); err != nil {
			return err
		}
	}
	return nil
}

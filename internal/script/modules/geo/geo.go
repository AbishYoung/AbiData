// Package geo registers the `geo` script module: points and areas for
// geographic fields (ADR 0010). Geometry values are Lua tables that mirror
// GeoJSON, so they can be stored in geo_point and geo_area fields as they are.
package geo

import (
	lua "github.com/yuin/gopher-lua"

	g "abidata/internal/geo"
	"abidata/internal/script"
)

var allCtx = []script.Context{script.UIEvent, script.Report, script.Job}

func init() {
	script.Register(script.ModuleDef{Doc: doc(), Build: build})
}

const geometryNote = "A point is a table {type = \"Point\", coordinates = {longitude, latitude}} and an area is a GeoJSON Polygon or MultiPolygon table. " +
	"Everywhere a point is expected you can also pass {lat = .., lon = ..}, the text \"lat, lon\", or a GeoJSON Feature; everywhere an area is expected, a Feature or GeoJSON text. " +
	"Coordinates are rounded to 7 decimal places. Areas that cross the 180° meridian are not supported."

func doc() script.ModuleDoc {
	pointParam := func(name, d string) script.ParamDoc {
		return script.ParamDoc{Name: name, Type: "table|string", Doc: d}
	}
	return script.ModuleDoc{
		Name: "geo",
		Doc: "Locations and areas. Use these with geo_point and geo_area fields: records.get returns their values as geometry tables and records.insert and records.update accept them.\n\n" +
			geometryNote + "\n\nDistances are in metres along the Earth's surface (haversine) and areas in square metres (spherical approximation).",
		Contexts: allCtx,
		Funcs: []script.FuncDoc{
			{
				Name: "point", Signature: "geo.point(lat, lon, alt?)",
				Params: []script.ParamDoc{
					{Name: "lat", Type: "number", Doc: "Latitude in degrees, -90 to 90."},
					{Name: "lon", Type: "number", Doc: "Longitude in degrees, -180 to 180."},
					{Name: "alt", Type: "number", Opt: true, Doc: "Altitude in metres, kept as the third coordinate."},
				},
				Returns: "table",
				Errors:  []string{"lat or lon is not a number", "lat is outside -90 to 90 or lon is outside -180 to 180"},
				Doc:     "Builds a point. Note the argument order: latitude first, as people write coordinates; the stored coordinates are [longitude, latitude] as GeoJSON requires.",
				Example: "local p = geo.point(51.50135, -0.14189)\nreturn p.coordinates[1]  -- -0.14189 (the longitude)",
			},
			{
				Name: "lat", Signature: "geo.lat(point)",
				Params:  []script.ParamDoc{pointParam("point", "A point.")},
				Returns: "number", Errors: []string{"point is not a valid point"},
				Doc:     "Returns a point's latitude.",
				Example: "return geo.lat(\"51.5, -0.14\")  -- 51.5",
			},
			{
				Name: "lon", Signature: "geo.lon(point)",
				Params:  []script.ParamDoc{pointParam("point", "A point.")},
				Returns: "number", Errors: []string{"point is not a valid point"},
				Doc:     "Returns a point's longitude.",
				Example: "return geo.lon(\"51.5, -0.14\")  -- -0.14",
			},
			{
				Name: "distance", Signature: "geo.distance(a, b)",
				Params:  []script.ParamDoc{pointParam("a", "A point."), pointParam("b", "A point.")},
				Returns: "number", Errors: []string{"a or b is not a valid point"},
				Doc:     "The great-circle distance between two points, in metres. It is symmetric: distance(a, b) equals distance(b, a).",
				Example: "return geo.distance(geo.point(51.5074, -0.1278), geo.point(48.8566, 2.3522))  -- about 343,500",
			},
			{
				Name: "within", Signature: "geo.within(point, area)",
				Params:  []script.ParamDoc{pointParam("point", "A point."), {Name: "area", Type: "table|string", Doc: "A Polygon or MultiPolygon."}},
				Returns: "boolean", Errors: []string{"point is not a valid point", "area is not a valid area"},
				Doc:     "Tests whether a point is inside an area. Holes are outside, and a point exactly on the boundary counts as inside.",
				Example: "local area = records.get(\"Zones\", 1).Fence\nreturn geo.within(geo.point(5, 5), area)",
			},
			{
				Name: "area", Signature: "geo.area(area)",
				Params:  []script.ParamDoc{{Name: "area", Type: "table|string", Doc: "A Polygon or MultiPolygon."}},
				Returns: "number", Errors: []string{"area is not a valid area"},
				Doc:     "The area in square metres (holes are subtracted).",
				Example: "return geo.area(records.get(\"Zones\", 1).Fence) / 10000  -- hectares",
			},
			{
				Name: "centroid", Signature: "geo.centroid(area)",
				Params:  []script.ParamDoc{{Name: "area", Type: "table|string", Doc: "A Polygon or MultiPolygon."}},
				Returns: "table", Errors: []string{"area is not a valid area"},
				Doc:     "The area-weighted centre of an area, as a point. For a concave area it can lie outside the area.",
				Example: "return geo.centroid(records.get(\"Zones\", 1).Fence)",
			},
			{
				Name: "bbox", Signature: "geo.bbox(area)",
				Params:  []script.ParamDoc{{Name: "area", Type: "table|string", Doc: "A Polygon or MultiPolygon."}},
				Returns: "table", Errors: []string{"area is not a valid area"},
				Doc:     "The bounding box as a list {minLon, minLat, maxLon, maxLat}.",
				Example: "local b = geo.bbox(records.get(\"Zones\", 1).Fence)\nreturn b[3] - b[1]  -- width in degrees",
			},
		},
	}
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()

	toLua := func(raw g.Raw) lua.LValue { return script.ToLua(r, raw.Object()) }
	point := func(L *lua.LState, n int) g.Point {
		v := L.Get(n)
		if v == lua.LNil {
			L.ArgError(n, "a point is required")
		}
		p, err := g.ParsePoint(script.FromLua(r, v))
		if err != nil {
			L.ArgError(n, "invalid point: "+err.Error())
		}
		return p
	}
	area := func(L *lua.LState, n int) *g.Area {
		v := L.Get(n)
		if v == lua.LNil {
			L.ArgError(n, "an area is required")
		}
		a, err := g.ParseArea(script.FromLua(r, v))
		if err != nil {
			L.ArgError(n, "invalid area: "+err.Error())
		}
		return a
	}

	t.RawSetString("point", L.NewFunction(func(L *lua.LState) int {
		lat, lon := L.CheckNumber(1), L.CheckNumber(2)
		p, err := g.NewPoint(float64(lat), float64(lon))
		if err != nil {
			L.ArgError(1, err.Error())
		}
		if L.Get(3) != lua.LNil {
			if p, err = withAlt(p, float64(L.CheckNumber(3))); err != nil {
				L.ArgError(3, err.Error())
			}
		}
		L.Push(toLua(g.Raw(p.JSON())))
		return 1
	}))
	t.RawSetString("lat", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(point(L, 1).Lat))
		return 1
	}))
	t.RawSetString("lon", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(point(L, 1).Lon))
		return 1
	}))
	t.RawSetString("distance", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(g.Distance(point(L, 1), point(L, 2))))
		return 1
	}))
	t.RawSetString("within", L.NewFunction(func(L *lua.LState) int {
		p := point(L, 1)
		L.Push(lua.LBool(area(L, 2).Contains(p)))
		return 1
	}))
	t.RawSetString("area", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LNumber(area(L, 1).AreaM2()))
		return 1
	}))
	t.RawSetString("centroid", L.NewFunction(func(L *lua.LState) int {
		L.Push(toLua(g.Raw(area(L, 1).Centroid().JSON())))
		return 1
	}))
	t.RawSetString("bbox", L.NewFunction(func(L *lua.LState) int {
		b := area(L, 1).BBox()
		out := L.CreateTable(4, 0)
		for _, x := range b {
			out.Append(lua.LNumber(x))
		}
		L.Push(out)
		return 1
	}))
	return t
}

func withAlt(p g.Point, alt float64) (g.Point, error) {
	return g.ParsePoint(map[string]any{"lat": p.Lat, "lon": p.Lon, "alt": alt})
}

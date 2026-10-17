package query

import (
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/geo"
	"abidata/internal/sqlgen"
)

// Geographic support for visual queries (ADR 0010): the spatial join, the
// "is within" criterion and the "area" parameter. All of it compiles to
// geo_within, which is a registered SQL function (internal/sqlitex).

const (
	typeGeoPoint = "geo_point"
	typeGeoArea  = "geo_area"
)

// colGeoType returns "geo_point" or "geo_area" for a catalog column (including
// a calculated field with such a result type), or "" when it is neither or unknown.
func colGeoType(c *catalog.Column) string {
	if c == nil {
		return ""
	}
	switch t := FieldDisplayType(c); t {
	case typeGeoPoint, typeGeoArea:
		return t
	}
	return ""
}

// joinCondition builds the ON condition of one join column pair.
func joinCondition(j Join, le, re sqlgen.Col, lc, rc *catalog.Column) (sqlgen.Expr, error) {
	switch j.Op {
	case "", "eq":
		return sqlgen.Binary{Op: sqlgen.OpEq, L: le, R: re}, nil
	case "within":
		lt, rt := colGeoType(lc), colGeoType(rc)
		sys := func(id string) bool { return id == "id" || id == "_version" }
		if sys(j.LeftColumnID) || sys(j.RightColumnID) || (lc != nil && lt == "") || (rc != nil && rt == "") || (lt != "" && lt == rt) {
			return nil, bad("A spatial join needs one location field and one area field.")
		}
		if lt == typeGeoArea || rt == typeGeoPoint {
			return geoWithin(re, le), nil
		}
		return geoWithin(le, re), nil
	}
	return nil, bad("Unknown join condition %q.", j.Op)
}

func geoWithin(point, area sqlgen.Expr) sqlgen.Expr {
	return sqlgen.Func{Name: "geo_within", Args: []sqlgen.Expr{point, area}}
}

// withinCriterion builds "is within": col (a location) inside an area given as
// an area parameter, a GeoJSON literal, or another source's area field.
func withinCriterion(expr sqlgen.Expr, col *catalog.Column, c Criterion, params map[string]Param,
	ref func(alias, colID string) (sqlgen.Col, *catalog.Column, error)) (sqlgen.Expr, error) {
	if col != nil && colGeoType(col) != typeGeoPoint {
		return nil, bad("'is within' is only available for location fields.")
	}
	given := 0
	for _, ok := range []bool{c.Param != "", c.Value != nil, c.RefAlias != "" || c.RefColumnID != ""} {
		if ok {
			given++
		}
	}
	if given != 1 {
		return nil, bad("'is within' needs exactly one area: a parameter, a GeoJSON area, or an area field of another table.")
	}
	switch {
	case c.Param != "":
		p, ok := params[strings.ToLower(c.Param)]
		if !ok {
			return nil, bad("Parameter %q is not declared.", c.Param)
		}
		if p.Type != "area" {
			return nil, bad("Parameter %q must have the type area to be used with 'is within'.", c.Param)
		}
		return geoWithin(expr, sqlgen.Param{Name: p.Name, Type: sqlgen.GeoArea}), nil
	case c.Value != nil:
		area, err := geo.NormalizeArea(c.Value)
		if err != nil {
			return nil, bad("'is within' needs an area (GeoJSON Polygon or MultiPolygon): %s.", err)
		}
		return geoWithin(expr, sqlgen.Lit{Value: area, Type: sqlgen.GeoArea}), nil
	}
	if c.RefAlias == "" || c.RefColumnID == "" || ref == nil {
		return nil, bad("'is within' needs both the table and the field of the area.")
	}
	re, rc, err := ref(c.RefAlias, c.RefColumnID)
	if err != nil {
		return nil, err
	}
	if rc != nil && colGeoType(rc) != typeGeoArea {
		return nil, bad("%q is not an area field.", rc.DisplayName)
	}
	return geoWithin(expr, re), nil
}

// areaParamProblem validates a value for an "area" parameter ("" when fine).
func areaParamProblem(v any) string {
	if _, err := geo.ParseArea(v); err != nil {
		return "a GeoJSON Polygon or MultiPolygon (" + err.Error() + ")"
	}
	return ""
}

// looksLikeGeo types a sampled text value that is GeoJSON ("" when it is not).
func looksLikeGeo(s string) string {
	switch {
	case strings.HasPrefix(s, `{"type":"Point"`):
		return typeGeoPoint
	case strings.HasPrefix(s, `{"type":"Polygon"`), strings.HasPrefix(s, `{"type":"MultiPolygon"`):
		return typeGeoArea
	}
	return ""
}

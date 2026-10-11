package query

import (
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/geo"
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

// joinCondition renders the ON condition of one join column pair.
func joinCondition(j Join, le, re string, lc, rc *catalog.Column) (string, error) {
	switch j.Op {
	case "", "eq":
		return le + " = " + re, nil
	case "within":
		lt, rt := colGeoType(lc), colGeoType(rc)
		sys := func(id string) bool { return id == "id" || id == "_version" }
		if sys(j.LeftColumnID) || sys(j.RightColumnID) || (lc != nil && lt == "") || (rc != nil && rt == "") || (lt != "" && lt == rt) {
			return "", bad("A spatial join needs one location field and one area field.")
		}
		if lt == typeGeoArea || rt == typeGeoPoint {
			return "geo_within(" + re + ", " + le + ")", nil
		}
		return "geo_within(" + le + ", " + re + ")", nil
	}
	return "", bad("Unknown join condition %q.", j.Op)
}

// withinCriterion renders "is within": col (a location) inside an area given as
// an area parameter, a GeoJSON literal, or another source's area field.
func withinCriterion(expr string, col *catalog.Column, c Criterion, params map[string]Param,
	ref func(alias, colID string) (string, *catalog.Column, error)) (string, error) {
	if col != nil && colGeoType(col) != typeGeoPoint {
		return "", bad("'is within' is only available for location fields.")
	}
	given := 0
	for _, ok := range []bool{c.Param != "", c.Value != nil, c.RefAlias != "" || c.RefColumnID != ""} {
		if ok {
			given++
		}
	}
	if given != 1 {
		return "", bad("'is within' needs exactly one area: a parameter, a GeoJSON area, or an area field of another table.")
	}
	switch {
	case c.Param != "":
		p, ok := params[strings.ToLower(c.Param)]
		if !ok {
			return "", bad("Parameter %q is not declared.", c.Param)
		}
		if p.Type != "area" {
			return "", bad("Parameter %q must have the type area to be used with 'is within'.", c.Param)
		}
		return "geo_within(" + expr + ", :" + p.Name + ")", nil
	case c.Value != nil:
		area, err := geo.NormalizeArea(c.Value)
		if err != nil {
			return "", bad("'is within' needs an area (GeoJSON Polygon or MultiPolygon): %s.", err)
		}
		lit, err := sqlLiteral(area)
		if err != nil {
			return "", err
		}
		return "geo_within(" + expr + ", " + lit + ")", nil
	}
	if c.RefAlias == "" || c.RefColumnID == "" || ref == nil {
		return "", bad("'is within' needs both the table and the field of the area.")
	}
	re, rc, err := ref(c.RefAlias, c.RefColumnID)
	if err != nil {
		return "", err
	}
	if rc != nil && colGeoType(rc) != typeGeoArea {
		return "", bad("%q is not an area field.", rc.DisplayName)
	}
	return "geo_within(" + expr + ", " + re + ")", nil
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

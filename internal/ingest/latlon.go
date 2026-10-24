package ingest

import (
	"strconv"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
)

// latLon is a point read from two staged columns (ADR 0010).
type latLon struct{ lat, lon any }

// input converts the pair to the {lat, lon} shorthand records.DecodeValue
// accepts. Both empty is no value; one empty is an error. Range checks are
// the records codec's.
func (p latLon) input(c catalog.Column) (any, error) {
	lat, latBlank, err := coord(p.lat)
	if err != nil {
		return nil, fault.Newf(fault.BadRequest, "%q: the latitude is not a number.", c.DisplayName)
	}
	lon, lonBlank, err := coord(p.lon)
	if err != nil {
		return nil, fault.Newf(fault.BadRequest, "%q: the longitude is not a number.", c.DisplayName)
	}
	switch {
	case latBlank && lonBlank:
		return nil, nil
	case latBlank || lonBlank:
		return nil, fault.Newf(fault.BadRequest, "%q needs both a latitude and a longitude.", c.DisplayName)
	}
	return map[string]any{"lat": lat, "lon": lon}, nil
}

// coord reads one staged coordinate: a number, or text holding one.
func coord(v any) (f float64, blank bool, err error) {
	if b, ok := v.([]byte); ok {
		v = string(b)
	}
	switch x := v.(type) {
	case nil:
		return 0, true, nil
	case int64:
		return float64(x), false, nil
	case float64:
		return x, false, nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, true, nil
		}
		f, err = strconv.ParseFloat(s, 64)
		return f, false, err
	}
	return 0, false, strconv.ErrSyntax
}

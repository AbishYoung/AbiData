package ingest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
)

// geoJSONGeometryKey names the column that holds each feature's geometry when
// a GeoJSON file is read as a table (ADR 0010).
const geoJSONGeometryKey = "geometry"

// readGeoJSONFeatures calls fn once per feature of a GeoJSON FeatureCollection
// (or a single Feature) at path. The keys are the feature's property names in
// file order followed by "geometry", whose value is the geometry object (or
// null). A property that is itself called "geometry" is renamed so it does not
// collide.
func readGeoJSONFeatures(path string, fn func(keys []string, vals []json.RawMessage) error) error {
	f, err := os.Open(path) //nolint:gosec // path built from a validated import id under the data dir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 64<<10))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return badFile("A GeoJSON file must hold one FeatureCollection object.")
	}
	idx := 0
	emit := func(feature json.RawMessage) error {
		idx++
		if len(feature) > maxRecordBytes {
			return badFile("Feature %d is longer than %d MiB.", idx, maxRecordBytes>>20)
		}
		var ft struct {
			Type       string          `json:"type"`
			Geometry   json.RawMessage `json:"geometry"`
			Properties json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(feature, &ft); err != nil || ft.Type != "Feature" {
			return badFile("Item %d is not a GeoJSON Feature.", idx)
		}
		var keys []string
		var vals []json.RawMessage
		if p := bytes.TrimSpace(ft.Properties); len(p) > 0 && p[0] == '{' {
			if !jsonDepthOK(p) {
				return badFile("The properties of feature %d are nested more than %d levels deep.", idx, maxJSONDepth)
			}
			if err := orderedObject(p, func(k string, v json.RawMessage) error {
				if k == geoJSONGeometryKey {
					k = geoJSONGeometryKey + " (property)"
				}
				for i, have := range keys {
					if have == k {
						vals[i] = v
						return nil
					}
				}
				if len(keys) >= MaxColumns-1 {
					return badFile("Feature %d has more than %d properties.", idx, MaxColumns-1)
				}
				keys, vals = append(keys, k), append(vals, v)
				return nil
			}); err != nil {
				return badFile("The properties of feature %d are not valid: %v", idx, err)
			}
		}
		g := ft.Geometry
		if len(g) == 0 {
			g = json.RawMessage("null")
		}
		return fn(append(keys, geoJSONGeometryKey), append(vals, g))
	}
	seen := false
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return badFile("The GeoJSON file is not valid: %v", err)
		}
		key, _ := kt.(string)
		if key == "features" {
			seen = true
			if t, err := dec.Token(); err != nil || t != json.Delim('[') {
				return badFile("\"features\" must be an array.")
			}
			for dec.More() {
				var el json.RawMessage
				if err := dec.Decode(&el); err != nil {
					return badFile("The GeoJSON file is not valid: %v", err)
				}
				if err := emit(el); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return badFile("The GeoJSON file is not valid: %v", err)
			}
			continue
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return badFile("The GeoJSON file is not valid: %v", err)
		}
		if key != "type" {
			continue
		}
		var typ string
		_ = json.Unmarshal(raw, &typ)
		switch typ {
		case "Feature":
			return singleFeature(path, emit)
		case "FeatureCollection":
		default:
			return badFile("The GeoJSON file must be a FeatureCollection or a Feature, not %q.", typ)
		}
	}
	if !seen {
		return badFile("The GeoJSON file has no \"features\" array.")
	}
	return nil
}

// singleFeature reads a file that is one Feature rather than a collection.
func singleFeature(path string, emit func(json.RawMessage) error) error {
	b, err := os.ReadFile(path) //nolint:gosec // see readGeoJSONFeatures
	if err != nil {
		return err
	}
	return emit(b)
}

// orderedObject calls fn for each member of a JSON object, in file order.
func orderedObject(raw []byte, fn func(key string, val json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
		if err := fn(key, v); err != nil {
			return err
		}
	}
	return nil
}

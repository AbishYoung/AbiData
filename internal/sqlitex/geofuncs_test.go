package sqlitex

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

const testSquare = `{"type":"Polygon","coordinates":[[[0,0],[10,0],[10,10],[0,10],[0,0]],[[4,4],[6,4],[6,6],[4,6],[4,4]]]}`

func TestGeoFunctions(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []Kind{Writer, Reader} {
		path := filepath.Join(t.TempDir(), "ws.db")
		w, err := Open(ctx, path, Options{Kind: Writer})
		if err != nil {
			t.Fatal(err)
		}
		c := w
		if kind == Reader {
			w.Close()
			w, err = Open(ctx, path, Options{Kind: Writer})
			if err != nil {
				t.Fatal(err)
			}
			if c, err = Open(ctx, path, Options{Kind: Reader}); err != nil {
				t.Fatal(err)
			}
			defer c.Close()
		}
		defer w.Close()
		one := func(sql string, args ...any) any {
			t.Helper()
			var v any
			if err := c.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return v
		}
		if got := one(`SELECT geo_point(51.50135, -0.14189)`); got != `{"type":"Point","coordinates":[-0.14189,51.50135]}` {
			t.Errorf("geo_point = %v", got)
		}
		if got := one(`SELECT geo_point(?, ?)`, int64(1), int64(2)); got != `{"type":"Point","coordinates":[2,1]}` {
			t.Errorf("geo_point ints = %v", got)
		}
		for _, sql := range []string{
			`SELECT geo_point(91, 0)`, `SELECT geo_point(NULL, 0)`, `SELECT geo_point('x', 0)`,
			`SELECT geo_lat('nonsense')`, `SELECT geo_lon(NULL)`, `SELECT geo_lat(5)`,
			`SELECT geo_distance_m('x', geo_point(0,0))`, `SELECT geo_within(geo_point(0,0), 'bad')`,
			`SELECT geo_within(NULL, '` + testSquare + `')`, `SELECT geo_area_m2('bad')`,
			`SELECT geo_centroid(geo_point(0,0))`, `SELECT geo_bbox('')`,
		} {
			if got := one(sql); got != nil {
				t.Errorf("%s = %v, want NULL", sql, got)
			}
		}
		if got := one(`SELECT geo_lat(geo_point(51.5, -0.1))`); got != 51.5 {
			t.Errorf("geo_lat = %v", got)
		}
		if got := one(`SELECT geo_lon('-0.1, 51.5')`); got != nil {
			// "lat, lon" text parses as lat=-0.1 lon=51.5
			if got != 51.5 {
				t.Errorf("geo_lon text = %v", got)
			}
		}
		d, _ := one(`SELECT geo_distance_m(geo_point(51.5074,-0.1278), geo_point(48.8566,2.3522))`).(float64)
		if d < 343e3 || d > 345e3 {
			t.Errorf("distance = %v", d)
		}
		for _, tc := range []struct {
			lat, lon float64
			want     int64
		}{{2, 2, 1}, {5, 5, 0}, {5, 4, 1}, {0, 5, 1}, {20, 20, 0}} {
			if got := one(`SELECT geo_within(geo_point(?, ?), ?)`, tc.lat, tc.lon, testSquare); got != tc.want {
				t.Errorf("geo_within(%v,%v) = %v, want %d", tc.lat, tc.lon, got, tc.want)
			}
		}
		if a, _ := one(`SELECT geo_area_m2(?)`, testSquare).(float64); a < 1.17e12 || a > 1.19e12 {
			t.Errorf("area = %v", a)
		}
		if got := one(`SELECT geo_bbox(?)`, testSquare); got != `[0,0,10,10]` {
			t.Errorf("bbox = %v", got)
		}
		if got, _ := one(`SELECT geo_lon(geo_centroid(?))`, testSquare).(float64); math.Abs(got-5) > 0.5 {
			t.Errorf("centroid lon = %v", got)
		}
		// Usable in a generated column and a CHECK under trusted_schema=OFF.
		if kind == Writer {
			if _, err := c.Exec(ctx, `CREATE TABLE p (id INTEGER PRIMARY KEY, lat REAL, lon REAL,
				pt TEXT GENERATED ALWAYS AS (geo_point(lat, lon)) VIRTUAL,
				CHECK (geo_lat(pt) IS NULL OR geo_lat(pt) <= 90)) STRICT`); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Exec(ctx, `INSERT INTO p (lat, lon) VALUES (1, 2)`); err != nil {
				t.Fatal(err)
			}
			if got := one(`SELECT pt FROM p`); got != `{"type":"Point","coordinates":[2,1]}` {
				t.Errorf("generated = %v", got)
			}
		}
	}
}

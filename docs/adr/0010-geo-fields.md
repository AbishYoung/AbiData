# ADR 0010: Geographic fields (points and areas)

Status: Proposed · 2026-10-11

## Context
Users want to record where things are and to draw boundaries. The driving use case is **tracking invasive species**:
- Field crews record sightings on a phone, with the GPS position, photos (see ADR 0011) and notes.
- Coordinators draw **areas** such as quarantine zones, survey areas and treatment areas.
- They then ask questions like "which sightings fall inside a quarantine zone", "sightings within 500 m of this one" and "how big is this treatment area".
- Reports and forms show the data on a map.

The rest of the platform is SQLite per workspace with no extensions, a Lua sandbox, and a CSP that only allows the app itself.

## Decision

**Two field types.**
- `geo_point` is a single location.
- `geo_area` is a fence: a polygon, optionally with holes, or a multipolygon.

**Storage and API values.**
- Both types are stored in one `TEXT` column holding a canonical **GeoJSON geometry** (RFC 7946):
  - WGS84, `[longitude, latitude]` order;
  - `Point` for `geo_point`;
  - `Polygon` or `MultiPolygon` for `geo_area`.
- The API reads and writes the same GeoJSON object. On input it also accepts these shorthands:
  - `{"lat": .., "lon": ..}` and the string `"lat, lon"` for points;
  - a GeoJSON `Feature` (the geometry is taken from it).
- Values are normalised on write:
  - coordinates are rounded to 7 decimal places (about 1 cm);
  - rings are closed;
  - the outer ring is made counter-clockwise and holes clockwise.
- Validation:
  - latitude must be in [-90, 90] and longitude in [-180, 180];
  - each ring needs at least 4 positions;
  - an area may have at most 10,000 positions and at most 1 MiB of JSON;
  - a third (altitude) coordinate is accepted for points and kept.
- Positional accuracy is not part of the geometry. Tables that need it add an ordinary number field. The sample's "GPS accuracy (m)" field is an example.

**SQL functions.** These are registered on every workspace connection, deterministic, and allowed by the query authorizer.
- `geo_point(lat, lon)` builds a point.
- `geo_lat(p)` and `geo_lon(p)` read a point's coordinates.
- `geo_distance_m(a, b)` is the haversine distance between two points.
- `geo_within(p, area)` tests a point against an area by even-odd ray casting, with holes. Points on the boundary count as inside.
- `geo_area_m2(area)` uses the spherical-excess approximation.
- `geo_centroid(area)` returns a point.
- `geo_bbox(area)` returns `[minLon, minLat, maxLon, maxLat]` as JSON.

Invalid input returns NULL rather than raising, so one bad row cannot break a query. The functions work in calculated fields, SQL queries and visual-query expressions.

**Records API filters.**
- `within:<area GeoJSON>` and `near:<lat>,<lon>,<metres>` are added for `geo_point` fields, on the same filter syntax and validation as the existing operators.
- There is no spatial index. A full scan is fine for the expected size (tens of thousands of points); docs/limits state this.

**Visual queries.**
- A **spatial join** (`point within area`) joins two sources, for example Sightings and Zones.
- A criterion `is within` compares a point field with an area parameter, or with an area field of a joined source.
- Both compile to `geo_within`.

**Lua.** A `geo` module provides `geo.point(lat, lon)`, `geo.distance(a, b)`, `geo.within(p, area)`, `geo.area(a)`, `geo.centroid(a)` and `geo.bbox(a)`. Geometry values are Lua tables mirroring the GeoJSON.

**Import and export.**
- CSV import can map one text column (`"lat, lon"`, WKT `POINT(lon lat)` or GeoJSON) into a point. It can also map two numeric columns as latitude and longitude.
- Importing a GeoJSON `FeatureCollection` creates rows: properties become fields and the geometry becomes a `geo_point` or `geo_area` field. This is how existing boundary files come in.
- CSV export writes GeoJSON text.
- Tables with a geo field can also be exported as a GeoJSON `FeatureCollection`.

**Maps in the browser.**
- Rendering uses **Leaflet** (BSD-2), a new web dependency. It is small and mature, and it handles drawing, touch and keyboard pan and zoom.
- **Base map tiles are off by default:** a self-hosted install must not send users' locations to a third party unless an administrator chooses to.
- Admin console > Settings > **Maps** sets a tile URL template and an attribution, for example OpenStreetMap, a commercial provider or a self-hosted tile server. The server adds that origin to the CSP `img-src` only while it is set.
- Without tiles, maps draw on a plain graticule background with the geometry, so they still work offline and in print.

**Where geo appears in the UI.**
- **Datasheet:**
  - points display as `51.50135, -0.14189`;
  - areas display as `Area · 12 points · 3.4 km²`;
  - the editors open a map dialog.
- **Point editor:**
  - latitude and longitude inputs, and click on the map to set the point;
  - **Use my location** (browser geolocation, needs HTTPS or localhost), which fills an accuracy field when the form has one.
- **Area editor:** click to add vertices, drag to move them, Delete to remove one, hole drawing, and paste or import GeoJSON.
- **Forms:** a new **Map** control bound to a point or area field. It has an optional **overlay**: a saved query with a geo column, drawn read-only, for example quarantine zones while recording a sighting.
- **Reports:** a **Map** control rendered as SVG (geometry, overlay and graticule, plus tiles if configured). It prints reliably.
- **Phones:** full-screen map editors, large touch targets and a prominent "Use my location" button.

## Consequences
- No SpatiaLite and no native code: the functions are pure Go. Spatial joins are O(n·m), with limits documented.
- GeoJSON in TEXT stays readable to scripts, exports and the API, and survives backups unchanged.
- Maps without tiles are plain. This is deliberate: privacy by default, and the setting is one field away.

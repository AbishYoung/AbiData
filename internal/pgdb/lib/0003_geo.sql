-- <prefix>_lib, part 3: the geo_* functions of ADR 0010, a port of
-- internal/geo. They take what the Go functions registered on SQLite take:
-- GeoJSON text or jsonb (a column), a Feature holding the geometry, and for
-- points also {"lat":…,"lon":…}, "lat, lon[, alt]" and WKT POINT(lon lat).
-- Invalid input gives NULL. Results that are geometry are canonical GeoJSON
-- text, exactly as internal/geo writes it.

-- abi_geo_round7 is internal/geo's round7: math.Round(x*1e7)/1e7, with -0 as 0.
CREATE OR REPLACE FUNCTION @lib.abi_geo_round7(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN round(@lib.abi_exact(x * 1e7)) = 0 THEN 0::double precision
	ELSE round(@lib.abi_exact(x * 1e7))::double precision / 1e7 END;

-- abi_geo_num formats a coordinate as strconv's 'f', -1 does.
CREATE OR REPLACE FUNCTION @lib.abi_geo_num(x double precision) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN x::numeric::text;

-- abi_geo_jnum formats a number as encoding/json does: 'f' between 1e-6 and
-- 1e21, otherwise an exponent without leading zeros (1e-7).
CREATE OR REPLACE FUNCTION @lib.abi_geo_jnum(x double precision) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	t text;
	m text[];
BEGIN
	IF x = 0 OR (abs(x) >= 1e-6 AND abs(x) < 1e21) THEN
		RETURN x::numeric::text;
	END IF;
	t := x::text; -- shortest form, such as 1e-07
	m := regexp_match(t, '^(-?[0-9.]+)e([+-])0*([0-9]+)$');
	IF m IS NULL THEN
		RETURN t;
	END IF;
	RETURN m[1] || 'e' || m[2] || m[3];
END $$;

-- abi_geo_number reads a JSON number, or a string holding one (numberOf).
CREATE OR REPLACE FUNCTION @lib.abi_geo_number(j jsonb) RETURNS double precision
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
	IF jsonb_typeof(j) = 'number' THEN
		RETURN (j #>> '{}')::double precision;
	ELSIF jsonb_typeof(j) = 'string' THEN
		RETURN @lib.abi_geo_parse_number(j #>> '{}');
	END IF;
	RETURN NULL;
END $$;

-- abi_geo_parse_number is parseNumber: a finite decimal number of at most 64
-- characters.
CREATE OR REPLACE FUNCTION @lib.abi_geo_parse_number(s text) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN length(btrim(s)) BETWEEN 1 AND 64 AND btrim(s) ~ '^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$'
	AND abs(btrim(s)::numeric) < 1.7976931348623157e308 THEN btrim(s)::double precision END;

-- abi_geo_point_of is NewPoint plus withAlt: a canonical point, or NULL.
CREATE OR REPLACE FUNCTION @lib.abi_geo_point_of(lat double precision, lon double precision, alt double precision DEFAULT NULL,
	OUT plon double precision, OUT plat double precision, OUT palt double precision)
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
BEGIN
	IF lat IS NULL OR lon IS NULL OR lat < -90 OR lat > 90 OR lon < -180 OR lon > 180
		OR lat IN ('NaN', 'Infinity', '-Infinity') OR lon IN ('NaN', 'Infinity', '-Infinity') THEN
		RETURN;
	END IF;
	plat := @lib.abi_geo_round7(lat);
	plon := @lib.abi_geo_round7(lon);
	IF alt IS NOT NULL THEN
		palt := @lib.abi_geo_round7(alt);
	END IF;
END $$;

-- abi_geo_pt parses a point (ParsePoint). The text form also covers jsonb
-- columns.
CREATE OR REPLACE FUNCTION @lib.abi_geo_pt(j jsonb, depth integer DEFAULT 0,
	OUT plon double precision, OUT plat double precision, OUT palt double precision)
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	c jsonb;
	r record;
	n integer;
	v double precision[];
	i integer;
BEGIN
	IF jsonb_typeof(j) = 'string' THEN
		SELECT * INTO plon, plat, palt FROM @lib.abi_geo_pt(j #>> '{}');
		RETURN;
	END IF;
	IF jsonb_typeof(j) <> 'object' THEN
		RETURN;
	END IF;
	CASE coalesce(j ->> 'type', '')
	WHEN 'Feature' THEN
		IF depth = 0 AND j ? 'geometry' THEN
			SELECT * INTO plon, plat, palt FROM @lib.abi_geo_pt(j -> 'geometry', 1);
		END IF;
	WHEN 'Point' THEN
		c := j -> 'coordinates';
		IF jsonb_typeof(c) <> 'array' OR jsonb_array_length(c) NOT BETWEEN 2 AND 3 THEN
			RETURN;
		END IF;
		n := jsonb_array_length(c);
		FOR i IN 0 .. n - 1 LOOP
			v[i + 1] := @lib.abi_geo_number(c -> i);
			IF v[i + 1] IS NULL THEN
				RETURN;
			END IF;
		END LOOP;
		r := @lib.abi_geo_point_of(v[2], v[1], v[3]);
		plon := r.plon; plat := r.plat; palt := r.palt;
	WHEN '' THEN
		IF NOT j ? 'lat' OR NOT (j ? 'lon' OR j ? 'lng') THEN
			RETURN;
		END IF;
		v[1] := @lib.abi_geo_number(j -> 'lat');
		v[2] := @lib.abi_geo_number(coalesce(j -> 'lon', j -> 'lng'));
		IF v[1] IS NULL OR v[2] IS NULL THEN
			RETURN;
		END IF;
		IF j ? 'alt' AND jsonb_typeof(j -> 'alt') <> 'null' THEN
			v[3] := @lib.abi_geo_number(j -> 'alt');
			IF v[3] IS NULL THEN
				RETURN;
			END IF;
		END IF;
		r := @lib.abi_geo_point_of(v[1], v[2], v[3]);
		plon := r.plon; plat := r.plat; palt := r.palt;
	ELSE
		RETURN;
	END CASE;
END $$;

CREATE OR REPLACE FUNCTION @lib.abi_geo_pt(t text,
	OUT plon double precision, OUT plat double precision, OUT palt double precision)
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	s text := btrim(t);
	parts text[];
	m text[];
	v double precision[];
	r record;
	i integer;
BEGIN
	IF s = '' OR length(t) > 1048576 THEN
		RETURN;
	END IF;
	IF left(s, 1) = '{' THEN
		IF NOT pg_input_is_valid(s, 'jsonb') THEN
			RETURN;
		END IF;
		SELECT * INTO plon, plat, palt FROM @lib.abi_geo_pt(s::jsonb);
		RETURN;
	END IF;
	IF upper(left(s, 5)) = 'POINT' THEN
		m := regexp_match(btrim(substr(s, 6)), '^\((.*)\)$');
		IF m IS NULL THEN
			RETURN;
		END IF;
		parts := regexp_split_to_array(btrim(m[1]), '\s+');
		IF array_length(parts, 1) NOT BETWEEN 2 AND 3 THEN
			RETURN;
		END IF;
		FOR i IN 1 .. array_length(parts, 1) LOOP
			v[i] := @lib.abi_geo_parse_number(parts[i]);
			IF v[i] IS NULL THEN
				RETURN;
			END IF;
		END LOOP;
		r := @lib.abi_geo_point_of(v[2], v[1], v[3]);
	ELSE
		parts := string_to_array(s, ',');
		IF array_length(parts, 1) NOT BETWEEN 2 AND 3 THEN
			RETURN;
		END IF;
		FOR i IN 1 .. array_length(parts, 1) LOOP
			v[i] := @lib.abi_geo_parse_number(parts[i]);
			IF v[i] IS NULL THEN
				RETURN;
			END IF;
		END LOOP;
		r := @lib.abi_geo_point_of(v[1], v[2], v[3]);
	END IF;
	plon := r.plon; plat := r.plat; palt := r.palt;
END $$;

-- abi_geo_point_json is Point.JSON.
CREATE OR REPLACE FUNCTION @lib.abi_geo_point_json(lon double precision, lat double precision, alt double precision DEFAULT NULL)
RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN CASE WHEN lon IS NOT NULL AND lat IS NOT NULL THEN
	'{"type":"Point","coordinates":[' || @lib.abi_geo_num(lon) || ',' || @lib.abi_geo_num(lat)
	|| coalesce(',' || @lib.abi_geo_num(alt), '') || ']}' END;

-- abi_geo_area parses an area (ParseArea) into its normalised polygons: a
-- jsonb array of polygons, each an array of rings, each an array of
-- [lon, lat] (rounded, closed), with the bounding box of the outer rings.
CREATE OR REPLACE FUNCTION @lib.abi_geo_area(j jsonb, depth integer DEFAULT 0,
	OUT polys jsonb, OUT minlon double precision, OUT minlat double precision,
	OUT maxlon double precision, OUT maxlat double precision)
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	raw jsonb;
	poly jsonb;
	ring jsonb;
	pos jsonb;
	outp jsonb;
	outr jsonb;
	lon double precision; lat double precision;
	plon double precision; plat double precision;
	flon double precision; flat double precision;
	total integer := 0;
	ri integer;
	n integer;
	sa double precision;
BEGIN
	IF jsonb_typeof(j) = 'string' THEN
		SELECT * INTO polys, minlon, minlat, maxlon, maxlat FROM @lib.abi_geo_area(j #>> '{}');
		RETURN;
	END IF;
	IF jsonb_typeof(j) <> 'object' THEN
		RETURN;
	END IF;
	CASE coalesce(j ->> 'type', '')
	WHEN 'Feature' THEN
		IF depth = 0 AND j ? 'geometry' THEN
			SELECT * INTO polys, minlon, minlat, maxlon, maxlat FROM @lib.abi_geo_area(j -> 'geometry', 1);
		END IF;
		RETURN;
	WHEN 'Polygon' THEN
		raw := jsonb_build_array(j -> 'coordinates');
	WHEN 'MultiPolygon' THEN
		raw := j -> 'coordinates';
	ELSE
		RETURN;
	END CASE;
	-- json.Unmarshal into float64 slices: arrays all the way down, numbers at
	-- the bottom (null counts as 0 there, as Go decodes it).
	IF jsonb_typeof(raw) <> 'array' OR jsonb_array_length(raw) = 0 THEN
		RETURN;
	END IF;
	minlon := 180; minlat := 90; maxlon := -180; maxlat := -90;
	polys := '[]';
	FOR poly IN SELECT value FROM jsonb_array_elements(raw) LOOP
		IF jsonb_typeof(poly) <> 'array' OR jsonb_array_length(poly) = 0 THEN
			polys := NULL;
			RETURN;
		END IF;
		outp := '[]';
		ri := 0;
		FOR ring IN SELECT value FROM jsonb_array_elements(poly) LOOP
			IF jsonb_typeof(ring) <> 'array' THEN
				polys := NULL;
				RETURN;
			END IF;
			outr := '[]';
			n := 0;
			FOR pos IN SELECT value FROM jsonb_array_elements(ring) LOOP
				IF jsonb_typeof(pos) <> 'array' OR jsonb_array_length(pos) NOT BETWEEN 2 AND 3
					OR EXISTS (SELECT 1 FROM jsonb_array_elements(pos) e WHERE jsonb_typeof(e.value) NOT IN ('number', 'null')) THEN
					polys := NULL;
					RETURN;
				END IF;
				lon := coalesce((pos ->> 0)::double precision, 0);
				lat := coalesce((pos ->> 1)::double precision, 0);
				IF lat < -90 OR lat > 90 OR lon < -180 OR lon > 180 THEN
					polys := NULL;
					RETURN;
				END IF;
				lon := @lib.abi_geo_round7(lon);
				lat := @lib.abi_geo_round7(lat);
				IF n = 0 THEN
					flon := lon; flat := lat;
				ELSIF abs(lon - plon) > 180 THEN
					polys := NULL; -- crosses the antimeridian
					RETURN;
				END IF;
				outr := outr || jsonb_build_array(jsonb_build_array(lon, lat));
				plon := lon; plat := lat;
				n := n + 1;
				IF ri = 0 THEN
					minlon := least(minlon, lon); maxlon := greatest(maxlon, lon);
					minlat := least(minlat, lat); maxlat := greatest(maxlat, lat);
				END IF;
			END LOOP;
			IF n > 0 AND (plon <> flon OR plat <> flat) THEN
				IF abs(flon - plon) > 180 THEN
					polys := NULL;
					RETURN;
				END IF;
				outr := outr || jsonb_build_array(jsonb_build_array(flon, flat));
				n := n + 1;
			END IF;
			total := total + n;
			IF n < 4 OR total > 10000 THEN
				polys := NULL;
				RETURN;
			END IF;
			-- A ring must enclose an area.
			SELECT sum(((a.p ->> 0)::double precision - flon) * ((b.p ->> 1)::double precision - flat)
				- ((b.p ->> 0)::double precision - flon) * ((a.p ->> 1)::double precision - flat)) / 2
			INTO sa
			FROM jsonb_array_elements(outr) WITH ORDINALITY a(p, i)
			JOIN jsonb_array_elements(outr) WITH ORDINALITY b(p, i) ON b.i = a.i + 1;
			IF sa = 0 THEN
				polys := NULL;
				RETURN;
			END IF;
			outp := outp || jsonb_build_array(outr);
			ri := ri + 1;
		END LOOP;
		polys := polys || jsonb_build_array(outp);
	END LOOP;
END $$;

CREATE OR REPLACE FUNCTION @lib.abi_geo_area(t text,
	OUT polys jsonb, OUT minlon double precision, OUT minlat double precision,
	OUT maxlon double precision, OUT maxlat double precision)
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
	IF length(t) > 1048576 OR left(btrim(t), 1) <> '{' OR NOT pg_input_is_valid(btrim(t), 'jsonb') THEN
		RETURN;
	END IF;
	SELECT * INTO polys, minlon, minlat, maxlon, maxlat FROM @lib.abi_geo_area(btrim(t)::jsonb);
END $$;

-- The geometry kernel, on normalised polygons.

CREATE OR REPLACE FUNCTION @lib.abi_geo_on_segment(px double precision, py double precision,
	ax double precision, ay double precision, bx double precision, by_ double precision) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	tol constant double precision := 1e-9;
	dx double precision := bx - ax;
	dy double precision := by_ - ay;
	len double precision;
BEGIN
	IF px < least(ax, bx) - tol OR px > greatest(ax, bx) + tol OR py < least(ay, by_) - tol OR py > greatest(ay, by_) + tol THEN
		RETURN false;
	END IF;
	len := sqrt(dx * dx + dy * dy);
	IF len = 0 THEN
		RETURN sqrt((px - ax) ^ 2 + (py - ay) ^ 2) <= tol;
	END IF;
	RETURN abs(dx * (py - ay) - dy * (px - ax)) / len <= tol;
END $$;

CREATE OR REPLACE FUNCTION @lib.abi_geo_contains(polys jsonb, qx double precision, qy double precision) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	poly jsonb;
	ring jsonb;
	inside boolean;
	i integer;
	ax double precision; ay double precision; bx double precision; by_ double precision;
BEGIN
	FOR poly IN SELECT value FROM jsonb_array_elements(polys) LOOP
		inside := false;
		FOR ring IN SELECT value FROM jsonb_array_elements(poly) LOOP
			FOR i IN 1 .. jsonb_array_length(ring) - 1 LOOP
				ax := (ring -> (i - 1) ->> 0)::double precision; ay := (ring -> (i - 1) ->> 1)::double precision;
				bx := (ring -> i ->> 0)::double precision; by_ := (ring -> i ->> 1)::double precision;
				IF @lib.abi_geo_on_segment(qx, qy, ax, ay, bx, by_) THEN
					RETURN true;
				END IF;
				IF (ay > qy) <> (by_ > qy) AND qx < ax + (qy - ay) * (bx - ax) / (by_ - ay) THEN
					inside := NOT inside;
				END IF;
			END LOOP;
		END LOOP;
		IF inside THEN
			RETURN true;
		END IF;
	END LOOP;
	RETURN false;
END $$;

-- Distance is the haversine great-circle distance in metres.
CREATE OR REPLACE FUNCTION @lib.abi_geo_distance(lat1 double precision, lon1 double precision,
	lat2 double precision, lon2 double precision) RETURNS double precision
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	rad constant double precision := pi() / 180;
	p1 double precision := lat1 * rad;
	p2 double precision := lat2 * rad;
	dp double precision := (lat2 - lat1) * rad;
	dl double precision := (lon2 - lon1) * rad;
	h double precision;
BEGIN
	h := sin(dp / 2) * sin(dp / 2) + cos(p1) * cos(p2) * sin(dl / 2) * sin(dl / 2);
	RETURN 2 * 6371008.8 * asin(least(1, sqrt(h)));
END $$;

-- AreaM2: spherical excess per ring, holes subtracted.
CREATE OR REPLACE FUNCTION @lib.abi_geo_area_m2(polys jsonb) RETURNS double precision
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	rad constant double precision := pi() / 180;
	poly jsonb;
	ring jsonb;
	ri integer;
	i integer;
	s double precision;
	total double precision := 0;
BEGIN
	FOR poly IN SELECT value FROM jsonb_array_elements(polys) LOOP
		ri := 0;
		FOR ring IN SELECT value FROM jsonb_array_elements(poly) LOOP
			s := 0;
			FOR i IN 1 .. jsonb_array_length(ring) - 1 LOOP
				s := s + ((ring -> i ->> 0)::double precision - (ring -> (i - 1) ->> 0)::double precision) * rad
					* (2 + sin((ring -> (i - 1) ->> 1)::double precision * rad) + sin((ring -> i ->> 1)::double precision * rad));
			END LOOP;
			s := abs(s) * 6371008.8 * 6371008.8 / 2;
			total := total + CASE WHEN ri = 0 THEN s ELSE -s END;
			ri := ri + 1;
		END LOOP;
	END LOOP;
	RETURN greatest(total, 0);
END $$;

-- Centroid: area-weighted planar centroid (holes subtract), or the middle of
-- the bounding box when the weights cancel.
CREATE OR REPLACE FUNCTION @lib.abi_geo_centroid(polys jsonb, minlon double precision, minlat double precision,
	maxlon double precision, maxlat double precision) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	poly jsonb;
	ring jsonb;
	ri integer;
	i integer;
	ox double precision; oy double precision;
	px double precision; py double precision; qx double precision; qy double precision;
	cr double precision; a2 double precision; sx double precision; sy double precision;
	area double precision; x double precision; y double precision;
	w double precision := 0; cx double precision := 0; cy double precision := 0;
BEGIN
	FOR poly IN SELECT value FROM jsonb_array_elements(polys) LOOP
		ri := 0;
		FOR ring IN SELECT value FROM jsonb_array_elements(poly) LOOP
			ox := (ring -> 0 ->> 0)::double precision; oy := (ring -> 0 ->> 1)::double precision;
			a2 := 0; sx := 0; sy := 0;
			FOR i IN 1 .. jsonb_array_length(ring) - 1 LOOP
				px := (ring -> (i - 1) ->> 0)::double precision - ox; py := (ring -> (i - 1) ->> 1)::double precision - oy;
				qx := (ring -> i ->> 0)::double precision - ox; qy := (ring -> i ->> 1)::double precision - oy;
				cr := px * qy - qx * py;
				a2 := a2 + cr;
				sx := sx + (px + qx) * cr;
				sy := sy + (py + qy) * cr;
			END LOOP;
			IF a2 = 0 THEN
				area := 0; x := ox; y := oy;
			ELSE
				area := abs(a2 / 2); x := ox + sx / (3 * a2); y := oy + sy / (3 * a2);
			END IF;
			IF ri > 0 THEN
				area := -area;
			END IF;
			w := w + area;
			cx := cx + area * x;
			cy := cy + area * y;
			ri := ri + 1;
		END LOOP;
	END LOOP;
	IF w <= 0 THEN
		RETURN @lib.abi_geo_point_json(@lib.abi_geo_round7((minlon + maxlon) / 2), @lib.abi_geo_round7((minlat + maxlat) / 2));
	END IF;
	RETURN @lib.abi_geo_point_json(@lib.abi_geo_round7(cx / w), @lib.abi_geo_round7(cy / w));
END $$;

-- The public functions. Each has a text and a jsonb form.

CREATE OR REPLACE FUNCTION @lib.geo_point(lat double precision, lon double precision) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN (SELECT @lib.abi_geo_point_json(p.plon, p.plat) FROM @lib.abi_geo_point_of(lat, lon) p);

DO $do$
DECLARE
	ty text;
BEGIN
	FOREACH ty IN ARRAY ARRAY['text', 'jsonb'] LOOP
		EXECUTE format($f$
			CREATE OR REPLACE FUNCTION @lib.geo_lat(p %1$s) RETURNS double precision
			LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
			RETURN (@lib.abi_geo_pt(p)).plat;

			CREATE OR REPLACE FUNCTION @lib.geo_lon(p %1$s) RETURNS double precision
			LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
			RETURN (@lib.abi_geo_pt(p)).plon;

			CREATE OR REPLACE FUNCTION @lib.geo_area_m2(a %1$s) RETURNS double precision
			LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
			RETURN @lib.abi_geo_area_m2((@lib.abi_geo_area(a)).polys);

			CREATE OR REPLACE FUNCTION @lib.geo_centroid(a %1$s) RETURNS text
			LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
			RETURN (SELECT @lib.abi_geo_centroid(r.polys, r.minlon, r.minlat, r.maxlon, r.maxlat) FROM @lib.abi_geo_area(a) r);

			CREATE OR REPLACE FUNCTION @lib.geo_bbox(a %1$s) RETURNS text
			LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
			RETURN (SELECT '[' || @lib.abi_geo_jnum(r.minlon) || ',' || @lib.abi_geo_jnum(r.minlat) || ','
				|| @lib.abi_geo_jnum(r.maxlon) || ',' || @lib.abi_geo_jnum(r.maxlat) || ']'
				FROM @lib.abi_geo_area(a) r WHERE r.polys IS NOT NULL);
		$f$, ty);
	END LOOP;
	FOREACH ty IN ARRAY ARRAY['text,text', 'text,jsonb', 'jsonb,text', 'jsonb,jsonb'] LOOP
		EXECUTE format($f$
			CREATE OR REPLACE FUNCTION @lib.geo_distance_m(a %1$s, b %2$s) RETURNS double precision
			LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
			RETURN (SELECT @lib.abi_geo_distance(p.plat, p.plon, q.plat, q.plon) FROM @lib.abi_geo_pt(a) p, @lib.abi_geo_pt(b) q);

			CREATE OR REPLACE FUNCTION @lib.geo_within(p %1$s, a %2$s) RETURNS boolean
			LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
			RETURN (SELECT CASE WHEN q.plon IS NULL OR r.polys IS NULL THEN NULL
				WHEN q.plon < r.minlon - 1e-9 OR q.plon > r.maxlon + 1e-9 OR q.plat < r.minlat - 1e-9 OR q.plat > r.maxlat + 1e-9 THEN false
				ELSE @lib.abi_geo_contains(r.polys, q.plon, q.plat) END
				FROM @lib.abi_geo_pt(p) q, @lib.abi_geo_area(a) r);
		$f$, split_part(ty, ',', 1), split_part(ty, ',', 2));
	END LOOP;
END $do$;

-- abi_geo_text is a stored geometry as internal/geo writes it: jsonb prints
-- spaces after separators, which canonical GeoJSON does not have.
CREATE OR REPLACE FUNCTION @lib.abi_geo_text(g jsonb) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE g ->> 'type'
	WHEN 'Point' THEN '{"type":"Point","coordinates":' || replace((g -> 'coordinates')::text, ' ', '') || '}'
	WHEN 'Polygon' THEN '{"type":"Polygon","coordinates":' || replace((g -> 'coordinates')::text, ' ', '') || '}'
	WHEN 'MultiPolygon' THEN '{"type":"MultiPolygon","coordinates":' || replace((g -> 'coordinates')::text, ' ', '') || '}'
	ELSE g::text END;

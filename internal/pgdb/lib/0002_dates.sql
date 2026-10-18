-- <prefix>_lib, part 2: SQLite's date and time functions (ADR 0012 §4), a
-- port of SQLite's date.c. A time value becomes a Julian day number in
-- milliseconds (iJD), modifiers adjust it, and the result is formatted the way
-- SQLite formats it. Invalid input gives NULL, as on SQLite. The functions
-- are abi_date, abi_time, abi_datetime, abi_julianday, abi_unixepoch and
-- abi_strftime; each takes a time value (text, a number, a date or a
-- timestamptz) and modifiers.
--
-- 'now' is not handled here: the renderer passes statement_timestamp(), so the
-- functions can stay IMMUTABLE without being folded to a stale constant.
-- 'localtime' and 'utc' use the session's TimeZone, as SQLite uses the
-- process's.

-- abi_dt_ymd: the calendar date and time of an iJD.
CREATE OR REPLACE FUNCTION @lib.abi_dt_ymd(ijd bigint, OUT y integer, OUT mo integer, OUT d integer,
	OUT h integer, OUT mi integer, OUT s double precision)
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	z integer; alpha integer; a integer; b integer; c integer; dd integer; e integer; x1 integer; ms integer;
BEGIN
	z := ((ijd + 43200000) / 86400000)::integer;
	alpha := floor((z + 32044.75) / 36524.25)::integer - 52;
	a := z + 1 + alpha - ((alpha + 100) / 4) + 25;
	b := a + 1524;
	c := floor((b - 122.1) / 365.25)::integer;
	dd := (36525 * (c & 32767)) / 100;
	e := floor((b - dd) / 30.6001)::integer;
	x1 := floor(30.6001 * e)::integer;
	d := b - dd - x1;
	mo := CASE WHEN e < 14 THEN e - 1 ELSE e - 13 END;
	y := CASE WHEN mo > 2 THEN c - 4716 ELSE c - 4715 END;
	ms := ((ijd + 43200000) % 86400000)::integer;
	s := (ms % 60000) / 1000.0;
	mi := (ms / 60000) % 60;
	h := ms / 3600000;
END $$;

-- abi_dt_jd: the iJD of a calendar date and time (month and day may be out of
-- range; the arithmetic carries them, as SQLite's computeJD does).
CREATE OR REPLACE FUNCTION @lib.abi_dt_jd(y integer, mo integer, d integer, h integer, mi integer, s double precision)
RETURNS bigint
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	yy integer := y; mm integer := mo; a integer; b integer; x1 integer; x2 integer;
BEGIN
	IF mm <= 2 THEN
		yy := yy - 1;
		mm := mm + 12;
	END IF;
	a := trunc(yy / 100.0)::integer; -- C division truncates toward zero
	b := 2 - a + trunc(a / 4.0)::integer;
	x1 := trunc(36525 * (yy + 4716) / 100.0)::integer;
	x2 := trunc(306001 * (mm + 1) / 10000.0)::integer;
	RETURN trunc((x1 + x2 + d + b - 1524.5) * 86400000)::bigint
		+ h * 3600000 + mi * 60000 + trunc(s * 1000 + 0.5)::bigint;
END $$;

-- abi_dt_parse: the iJD of a time string, or NULL. Accepts what SQLite
-- does: YYYY-MM-DD with an optional time (after T or a space) and timezone
-- (Z or ±HH:MM), HH:MM[:SS[.SSS]] alone (on 2000-01-01), and a number, which
-- is a Julian day number.
CREATE OR REPLACE FUNCTION @lib.abi_dt_parse(t text) RETURNS bigint
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	m text[];
	y integer; mo integer; d integer; h integer := 0; mi integer := 0; s double precision := 0;
	tz integer := 0;
	v text := btrim(t);
	jd double precision;
BEGIN
	m := regexp_match(v, '^(-?)(\d{4})-(\d{2})-(\d{2})(?:(?:T|\s+)(\d{2}):(\d{2})(?::(\d{2}(?:\.\d+)?))?)?\s*(?:(Z)|([+-])(\d{2}):(\d{2}))?$', 'i');
	IF m IS NOT NULL THEN
		y := m[2]::integer * CASE WHEN m[1] = '-' THEN -1 ELSE 1 END;
		mo := m[3]::integer;
		d := m[4]::integer;
		IF mo < 1 OR mo > 12 OR d < 1 OR d > 31 THEN
			RETURN NULL;
		END IF;
		IF m[5] IS NOT NULL THEN
			h := m[5]::integer;
			mi := m[6]::integer;
			s := coalesce(m[7]::double precision, 0);
			IF h > 24 OR mi > 59 OR s >= 60 THEN
				RETURN NULL;
			END IF;
		END IF;
		IF m[10] IS NOT NULL THEN
			tz := (m[10]::integer * 60 + m[11]::integer) * CASE WHEN m[9] = '-' THEN -1 ELSE 1 END;
		END IF;
		RETURN @lib.abi_dt_jd(y, mo, d, h, mi, s) - tz * 60000;
	END IF;
	m := regexp_match(v, '^(\d{2}):(\d{2})(?::(\d{2}(?:\.\d+)?))?\s*(?:(Z)|([+-])(\d{2}):(\d{2}))?$', 'i');
	IF m IS NOT NULL THEN
		h := m[1]::integer;
		mi := m[2]::integer;
		s := coalesce(m[3]::double precision, 0);
		IF h > 24 OR mi > 59 OR s >= 60 THEN
			RETURN NULL;
		END IF;
		IF m[7] IS NOT NULL THEN
			tz := (m[6]::integer * 60 + m[7]::integer) * CASE WHEN m[5] = '-' THEN -1 ELSE 1 END;
		END IF;
		RETURN @lib.abi_dt_jd(2000, 1, 1, h, mi, s) - tz * 60000;
	END IF;
	IF v ~ '^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$' THEN
		jd := v::double precision;
		IF jd < 0 OR jd > 5373484.499999 THEN
			RETURN NULL;
		END IF;
		RETURN trunc(jd * 86400000 + 0.5)::bigint;
	END IF;
	RETURN NULL;
END $$;

-- abi_dt: apply modifiers to a time value. num is the value as a number when
-- it was one (for 'unixepoch' and 'auto'); subsec reports the 'subsec'
-- modifier.
CREATE OR REPLACE FUNCTION @lib.abi_dt(ijd0 bigint, num double precision, mods text[], OUT ijd bigint, OUT subsec boolean)
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	md text;
	m text[];
	r double precision;
	x integer;
	c record;
	z bigint;
	n integer;
	first boolean := true;
	off interval;
BEGIN
	ijd := ijd0;
	subsec := false;
	FOREACH md IN ARRAY coalesce(mods, '{}'::text[]) LOOP
		IF md IS NULL THEN
			ijd := NULL;
			RETURN;
		END IF;
		md := lower(btrim(md));
		IF ijd IS NULL AND NOT (first AND num IS NOT NULL AND md IN ('unixepoch', 'auto')) THEN
			RETURN; -- not a time value; only 'unixepoch'/'auto' may make one of a number
		END IF;
		IF md IN ('unixepoch', 'auto', 'julianday') AND NOT first THEN
			ijd := NULL; -- these must come first
			RETURN;
		END IF;
		IF md = 'unixepoch' OR (md = 'auto' AND num IS NOT NULL AND ijd IS NULL) THEN
			-- A number read as seconds since 1970 ('auto': one outside the
			-- Julian day range).
			IF num IS NULL OR num < -210866760000 OR num > 253402300799 THEN
				ijd := NULL;
				RETURN;
			END IF;
			ijd := trunc(num * 1000 + 210866760000000 + 0.5)::bigint;
		ELSIF md = 'auto' THEN
			NULL; -- text, or a number that is already a Julian day
		ELSIF md = 'julianday' THEN
			IF num IS NULL OR ijd IS NULL THEN
				ijd := NULL;
				RETURN;
			END IF;
		ELSIF md IN ('subsec', 'subsecond') THEN
			subsec := true;
		ELSIF md = 'start of day' OR md = 'start of month' OR md = 'start of year' THEN
			c := @lib.abi_dt_ymd(ijd);
			ijd := @lib.abi_dt_jd(c.y, CASE WHEN md = 'start of year' THEN 1 ELSE c.mo END,
				CASE WHEN md = 'start of day' THEN c.d ELSE 1 END, 0, 0, 0);
		ELSIF md ~ '^weekday [0-6]$' THEN
			n := right(md, 1)::integer;
			z := ((ijd + 129600000) / 86400000) % 7;
			IF z > n THEN
				z := z - 7;
			END IF;
			ijd := ijd + (n - z) * 86400000;
		ELSIF md IN ('localtime', 'utc') THEN
			-- The session's zone offset at that instant.
			off := (to_timestamp((ijd - 210866760000000) / 1000.0) AT TIME ZONE current_setting('TimeZone'))
				- (to_timestamp((ijd - 210866760000000) / 1000.0) AT TIME ZONE 'UTC');
			IF md = 'localtime' THEN
				ijd := ijd + (extract(epoch FROM off) * 1000)::bigint;
			ELSE
				ijd := ijd - (extract(epoch FROM off) * 1000)::bigint;
			END IF;
		ELSE
			m := regexp_match(md, '^([+-]?\d+(?:\.\d*)?|[+-]?\.\d+)\s*(day|hour|minute|second|month|year)s?$');
			IF m IS NOT NULL THEN
				r := m[1]::double precision;
				IF m[2] IN ('month', 'year') THEN
					c := @lib.abi_dt_ymd(ijd);
					x := trunc(r)::integer;
					IF m[2] = 'month' THEN
						c.mo := c.mo + x;
						n := CASE WHEN c.mo > 0 THEN (c.mo - 1) / 12 ELSE (c.mo - 12) / 12 END;
						c.y := c.y + n;
						c.mo := c.mo - n * 12;
					ELSE
						c.y := c.y + x;
					END IF;
					ijd := @lib.abi_dt_jd(c.y, c.mo, c.d, c.h, c.mi, c.s);
					r := r - x;
					IF r <> 0 THEN
						ijd := ijd + trunc(r * CASE WHEN m[2] = 'month' THEN 30.0 ELSE 365.0 END * 86400000
							+ CASE WHEN r < 0 THEN -0.5 ELSE 0.5 END)::bigint;
					END IF;
				ELSE
					ijd := ijd + trunc(r * CASE m[2] WHEN 'day' THEN 86400000.0 WHEN 'hour' THEN 3600000.0
						WHEN 'minute' THEN 60000.0 ELSE 1000.0 END + CASE WHEN r < 0 THEN -0.5 ELSE 0.5 END)::bigint;
				END IF;
			ELSE
				-- ±HH:MM[:SS[.SSS]]: shift by a time of day.
				m := regexp_match(md, '^([+-])(\d{2}):(\d{2})(?::(\d{2}(?:\.\d+)?))?$');
				IF m IS NULL THEN
					ijd := NULL;
					RETURN;
				END IF;
				z := m[2]::bigint * 3600000 + m[3]::bigint * 60000 + trunc(coalesce(m[4]::double precision, 0) * 1000 + 0.5)::bigint;
				ijd := ijd + CASE WHEN m[1] = '-' THEN -z ELSE z END;
			END IF;
		END IF;
		first := false;
	END LOOP;
	IF ijd < 0 OR ijd > 464269060799999 THEN
		ijd := NULL; -- outside 0000-01-01 … 9999-12-31
	END IF;
END $$;

-- abi_dt_in: the starting iJD and number of each kind of time value.
CREATE OR REPLACE FUNCTION @lib.abi_dt_in(t text, OUT ijd bigint, OUT num double precision)
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
	ijd := @lib.abi_dt_parse(t);
	IF btrim(t) ~ '^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$' THEN
		num := btrim(t)::double precision;
	END IF;
END $$;

CREATE OR REPLACE FUNCTION @lib.abi_dt_in(t double precision, OUT ijd bigint, OUT num double precision)
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN (CASE WHEN t >= 0 AND t <= 5373484.499999 THEN trunc(t * 86400000 + 0.5)::bigint END, t);

CREATE OR REPLACE FUNCTION @lib.abi_dt_in(t timestamptz, OUT ijd bigint, OUT num double precision)
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN (round(extract(epoch FROM t) * 1000)::bigint + 210866760000000, NULL::double precision);

CREATE OR REPLACE FUNCTION @lib.abi_dt_in(t date, OUT ijd bigint, OUT num double precision)
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN ((t - DATE '2000-01-01') * 86400000::bigint + 210866760000000 + 946684800000, NULL::double precision);

-- Formatting.
CREATE OR REPLACE FUNCTION @lib.abi_dt_date(ijd bigint) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE c record;
BEGIN
	c := @lib.abi_dt_ymd(ijd);
	RETURN CASE WHEN c.y < 0 THEN '-' ELSE '' END || lpad(abs(c.y)::text, 4, '0') || '-' || lpad(c.mo::text, 2, '0') || '-' || lpad(c.d::text, 2, '0');
END $$;

CREATE OR REPLACE FUNCTION @lib.abi_dt_time(ijd bigint, subsec boolean) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE c record;
BEGIN
	c := @lib.abi_dt_ymd(ijd);
	IF subsec THEN
		RETURN lpad(c.h::text, 2, '0') || ':' || lpad(c.mi::text, 2, '0') || ':' || to_char(c.s, 'FM00.000');
	END IF;
	RETURN lpad(c.h::text, 2, '0') || ':' || lpad(c.mi::text, 2, '0') || ':' || lpad(trunc(c.s)::integer::text, 2, '0');
END $$;

-- abi_dt_format: SQLite's strftime conversions. An unknown one gives NULL.
CREATE OR REPLACE FUNCTION @lib.abi_dt_format(fmt text, ijd bigint, subsec boolean) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	c record;
	iso record;
	out text := '';
	i integer := 1;
	n integer := length(fmt);
	ch text;
	jan01 bigint;
	doy integer;
	mon integer := ((ijd + 43200000) / 86400000 % 7)::integer;   -- days after Monday
	sun integer := ((ijd + 129600000) / 86400000 % 7)::integer;  -- days after Sunday
	h12 integer;
	sec double precision;
	jd double precision;
	digits integer;
BEGIN
	c := @lib.abi_dt_ymd(ijd);
	jan01 := @lib.abi_dt_jd(c.y, 1, 1, 0, 0, 0);
	doy := ((ijd - jan01 + 43200000) / 86400000)::integer;
	h12 := CASE WHEN c.h > 12 THEN c.h - 12 WHEN c.h = 0 THEN 12 ELSE c.h END;
	WHILE i <= n LOOP
		ch := substr(fmt, i, 1);
		IF ch <> '%' THEN
			out := out || ch;
			i := i + 1;
			CONTINUE;
		END IF;
		i := i + 1;
		ch := substr(fmt, i, 1);
		i := i + 1;
		CASE ch
		WHEN 'd' THEN out := out || lpad(c.d::text, 2, '0');
		WHEN 'e' THEN out := out || lpad(c.d::text, 2, ' ');
		WHEN 'f' THEN
			sec := least(c.s, 59.999);
			out := out || to_char(sec, 'FM00.000');
		WHEN 'F' THEN out := out || @lib.abi_dt_date(ijd);
		WHEN 'H' THEN out := out || lpad(c.h::text, 2, '0');
		WHEN 'k' THEN out := out || lpad(c.h::text, 2, ' ');
		WHEN 'I' THEN out := out || lpad(h12::text, 2, '0');
		WHEN 'l' THEN out := out || lpad(h12::text, 2, ' ');
		WHEN 'j' THEN out := out || lpad((doy + 1)::text, 3, '0');
		WHEN 'J' THEN
			-- %.16g of the Julian day number.
			jd := ijd / 86400000.0;
			digits := 16 - (floor(log(greatest(abs(jd), 1e-300)::numeric))::integer + 1);
			out := out || CASE WHEN jd = 0 THEN '0'
				ELSE rtrim(rtrim(round(@lib.abi_exact(jd), greatest(digits, 0))::text, '0'), '.') END;
		WHEN 'm' THEN out := out || lpad(c.mo::text, 2, '0');
		WHEN 'M' THEN out := out || lpad(c.mi::text, 2, '0');
		WHEN 'p' THEN out := out || CASE WHEN c.h >= 12 THEN 'PM' ELSE 'AM' END;
		WHEN 'P' THEN out := out || CASE WHEN c.h >= 12 THEN 'pm' ELSE 'am' END;
		WHEN 'R' THEN out := out || lpad(c.h::text, 2, '0') || ':' || lpad(c.mi::text, 2, '0');
		WHEN 's' THEN
			IF subsec THEN
				out := out || to_char((ijd - 210866760000000) / 1000.0, 'FM999999999990.000');
			ELSE
				out := out || (ijd / 1000 - 210866760000)::text;
			END IF;
		WHEN 'S' THEN out := out || lpad(trunc(c.s)::integer::text, 2, '0');
		WHEN 'T' THEN out := out || @lib.abi_dt_time(ijd, false);
		WHEN 'u' THEN out := out || CASE WHEN sun = 0 THEN 7 ELSE sun END::text;
		WHEN 'w' THEN out := out || sun::text;
		WHEN 'U' THEN out := out || lpad(((doy - sun + 7) / 7)::text, 2, '0');
		WHEN 'W' THEN out := out || lpad(((doy - mon + 7) / 7)::text, 2, '0');
		WHEN 'G', 'g', 'V' THEN
			-- ISO 8601: the week's Thursday decides the year.
			iso := @lib.abi_dt_ymd(ijd + (3 - mon) * 86400000::bigint);
			IF ch = 'G' THEN
				out := out || lpad(iso.y::text, 4, '0');
			ELSIF ch = 'g' THEN
				out := out || lpad((iso.y % 100)::text, 2, '0');
			ELSE
				out := out || lpad(((ijd + (3 - mon) * 86400000::bigint - @lib.abi_dt_jd(iso.y, 1, 1, 0, 0, 0) + 43200000) / 86400000 / 7 + 1)::text, 2, '0');
			END IF;
		WHEN 'Y' THEN out := out || lpad(c.y::text, 4, '0');
		WHEN '%' THEN out := out || '%';
		ELSE RETURN NULL;
		END CASE;
	END LOOP;
	RETURN out;
END $$;

-- The public functions, one set per kind of time value.
DO $do$
DECLARE
	ty text;
BEGIN
	FOREACH ty IN ARRAY ARRAY['text', 'double precision', 'timestamptz', 'date'] LOOP
		EXECUTE format($f$
			CREATE OR REPLACE FUNCTION @lib.abi_date(t %1$s, VARIADIC mods text[] DEFAULT '{}') RETURNS text
			LANGUAGE sql IMMUTABLE PARALLEL SAFE
			RETURN (SELECT @lib.abi_dt_date(r.ijd) FROM @lib.abi_dt((@lib.abi_dt_in(t)).ijd, (@lib.abi_dt_in(t)).num, mods) r);

			CREATE OR REPLACE FUNCTION @lib.abi_time(t %1$s, VARIADIC mods text[] DEFAULT '{}') RETURNS text
			LANGUAGE sql IMMUTABLE PARALLEL SAFE
			RETURN (SELECT @lib.abi_dt_time(r.ijd, r.subsec) FROM @lib.abi_dt((@lib.abi_dt_in(t)).ijd, (@lib.abi_dt_in(t)).num, mods) r);

			CREATE OR REPLACE FUNCTION @lib.abi_datetime(t %1$s, VARIADIC mods text[] DEFAULT '{}') RETURNS text
			LANGUAGE sql IMMUTABLE PARALLEL SAFE
			RETURN (SELECT @lib.abi_dt_date(r.ijd) || ' ' || @lib.abi_dt_time(r.ijd, r.subsec) FROM @lib.abi_dt((@lib.abi_dt_in(t)).ijd, (@lib.abi_dt_in(t)).num, mods) r);

			CREATE OR REPLACE FUNCTION @lib.abi_julianday(t %1$s, VARIADIC mods text[] DEFAULT '{}') RETURNS double precision
			LANGUAGE sql IMMUTABLE PARALLEL SAFE
			RETURN (SELECT r.ijd / 86400000.0::double precision FROM @lib.abi_dt((@lib.abi_dt_in(t)).ijd, (@lib.abi_dt_in(t)).num, mods) r);

			-- unixepoch is an integer, or a real with 'subsec'; the renderer
			-- casts to the type it promised.
			CREATE OR REPLACE FUNCTION @lib.abi_unixepoch(t %1$s, VARIADIC mods text[] DEFAULT '{}') RETURNS double precision
			LANGUAGE sql IMMUTABLE PARALLEL SAFE
			RETURN (SELECT CASE WHEN r.subsec THEN (r.ijd - 210866760000000) / 1000.0::double precision
				ELSE trunc((r.ijd - 210866760000000) / 1000.0) END FROM @lib.abi_dt((@lib.abi_dt_in(t)).ijd, (@lib.abi_dt_in(t)).num, mods) r);

			CREATE OR REPLACE FUNCTION @lib.abi_strftime(fmt text, t %1$s, VARIADIC mods text[] DEFAULT '{}') RETURNS text
			LANGUAGE sql IMMUTABLE PARALLEL SAFE
			RETURN (SELECT @lib.abi_dt_format(fmt, r.ijd, r.subsec) FROM @lib.abi_dt((@lib.abi_dt_in(t)).ijd, (@lib.abi_dt_in(t)).num, mods) r);
		$f$, ty);
	END LOOP;
END $do$;

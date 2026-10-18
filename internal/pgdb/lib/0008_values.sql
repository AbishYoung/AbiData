-- <prefix>_lib, part 8: quote, unhex, typeof and timediff (ADR 0013 Phase F).

-- abi_quote_text is quote() of text: a SQL string literal, NULL as NULL.
CREATE OR REPLACE FUNCTION @lib.abi_quote_text(t text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN CASE WHEN t IS NULL THEN 'NULL' ELSE '''' || replace(t, '''', '''''') || '''' END;

-- abi_quote_real is quote() of a double: its text, NULL as NULL.
CREATE OR REPLACE FUNCTION @lib.abi_quote_real(f double precision) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN coalesce(@lib.abi_real_text(f), 'NULL');

-- abi_quote_blob is quote() of bytes: X'hex'.
CREATE OR REPLACE FUNCTION @lib.abi_quote_blob(b bytea) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN CASE WHEN b IS NULL THEN 'NULL' ELSE 'X''' || (upper(encode(b, 'hex') COLLATE "C")) || '''' END;

-- abi_unhex is unhex(): the bytes of hexadecimal text, with the characters of
-- ignore allowed between digit pairs; NULL when the text is not that.
CREATE OR REPLACE FUNCTION @lib.abi_unhex(t text, ignore text DEFAULT '') RETURNS bytea
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	hex text := '';
	i integer := 1;
	n integer := length(t);
	c text;
BEGIN
	WHILE i <= n LOOP
		c := substr(t, i, 1);
		IF c ~ '[0-9a-fA-F]' THEN
			IF substr(t, i + 1, 1) !~ '^[0-9a-fA-F]$' THEN
				RETURN NULL;
			END IF;
			hex := hex || substr(t, i, 2);
			i := i + 2;
		ELSIF strpos(ignore, c) > 0 THEN
			i := i + 1;
		ELSE
			RETURN NULL;
		END IF;
	END LOOP;
	RETURN decode(hex, 'hex');
END $$;

-- hex() of bytes.
CREATE OR REPLACE FUNCTION @lib.abi_hex(b bytea) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN coalesce(upper(encode(b, 'hex')), '');

-- abi_json_class is typeof() of an extracted JSON value, as json_extract's
-- result is stored on SQLite.
CREATE OR REPLACE FUNCTION @lib.abi_json_class(v jsonb) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN CASE jsonb_typeof(v)
	WHEN 'number' THEN CASE WHEN v::text ~ '[.eE]' THEN 'real' ELSE 'integer' END
	WHEN 'boolean' THEN 'integer'
	WHEN 'string' THEN 'text'
	WHEN 'array' THEN 'text'
	WHEN 'object' THEN 'text'
	ELSE 'null' END;

-- abi_quote_json is quote() of an extracted JSON value.
CREATE OR REPLACE FUNCTION @lib.abi_quote_json(v jsonb) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN CASE jsonb_typeof(v)
	WHEN 'number' THEN CASE WHEN v::text ~ '[.eE]' THEN @lib.abi_quote_real((v #>> '{}')::double precision) ELSE v::text END
	WHEN 'boolean' THEN CASE WHEN v = 'true' THEN '1' ELSE '0' END
	WHEN 'string' THEN @lib.abi_quote_text(v #>> '{}')
	WHEN 'array' THEN @lib.abi_quote_text(@lib.abi_json_min(v))
	WHEN 'object' THEN @lib.abi_quote_text(@lib.abi_json_min(v))
	ELSE 'NULL' END;

-- abi_timediff is timediff() of two iJDs: the calendar time from b to a, as
-- ±YYYY-MM-DD HH:MM:SS.SSS, counted as SQLite's date.c counts it.
CREATE OR REPLACE FUNCTION @lib.abi_timediff(j1 bigint, j2 bigint) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	a record := @lib.abi_dt_ymd(j1);
	b record := @lib.abi_dt_ymd(j2);
	sign text;
	y integer;
	m integer;
	yr integer := b.y;
	bm integer := b.mo;
	bj bigint := j2;
	diff bigint;
	r record;
BEGIN
	IF j1 >= j2 THEN
		sign := '+';
		y := a.y - b.y;
		IF y <> 0 THEN
			yr := a.y;
			bj := @lib.abi_dt_jd(yr, bm, b.d, b.h, b.mi, b.s);
		END IF;
		m := a.mo - b.mo;
		IF m < 0 THEN
			y := y - 1;
			m := m + 12;
		END IF;
		IF m <> 0 THEN
			bm := a.mo;
			bj := @lib.abi_dt_jd(yr, bm, b.d, b.h, b.mi, b.s);
		END IF;
		WHILE j1 < bj LOOP
			m := m - 1;
			IF m < 0 THEN
				m := 11;
				y := y - 1;
			END IF;
			bm := bm - 1;
			IF bm < 1 THEN
				bm := 12;
				yr := yr - 1;
			END IF;
			bj := @lib.abi_dt_jd(yr, bm, b.d, b.h, b.mi, b.s);
		END LOOP;
		diff := j1 - bj;
	ELSE
		sign := '-';
		y := b.y - a.y;
		IF y <> 0 THEN
			yr := a.y;
			bj := @lib.abi_dt_jd(yr, bm, b.d, b.h, b.mi, b.s);
		END IF;
		m := b.mo - a.mo;
		IF m < 0 THEN
			y := y - 1;
			m := m + 12;
		END IF;
		IF m <> 0 THEN
			bm := a.mo;
			bj := @lib.abi_dt_jd(yr, bm, b.d, b.h, b.mi, b.s);
		END IF;
		WHILE j1 > bj LOOP
			m := m - 1;
			IF m < 0 THEN
				m := 11;
				y := y - 1;
			END IF;
			bm := bm + 1;
			IF bm > 12 THEN
				bm := 1;
				yr := yr + 1;
			END IF;
			bj := @lib.abi_dt_jd(yr, bm, b.d, b.h, b.mi, b.s);
		END LOOP;
		diff := bj - j1;
	END IF;
	-- The rest is a time after 0000-01-01 00:00 (iJD 148699540800000).
	r := @lib.abi_dt_ymd(diff + 148699540800000);
	RETURN sign || lpad(y::text, 4, '0') || '-' || lpad(m::text, 2, '0') || '-' || lpad((r.d - 1)::text, 2, '0')
		|| ' ' || lpad(r.h::text, 2, '0') || ':' || lpad(r.mi::text, 2, '0') || ':' || to_char(r.s, 'FM00.000');
END $$;

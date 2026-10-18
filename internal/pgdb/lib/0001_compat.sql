-- <prefix>_lib, part 1 (ADR 0012 §4, ADR 0013 Phase F): the functions the
-- PostgreSQL renderer calls where a builtin's semantics differ from SQLite's.
-- Each keeps SQLite's answer, in particular NULL where SQLite returns NULL and
-- PostgreSQL would raise an error. Names that are also PostgreSQL builtins
-- carry abi_, since pg_catalog is searched before the library.
--
-- @lib is replaced by the quoted schema name. Builtins are resolved from
-- pg_catalog, which is always searched first, so the bodies cannot be
-- redirected by objects in other schemas.

-- SQLite's substr: 1-based; a negative start counts from the end; start 0
-- is before the first character; a negative length takes the characters
-- before the start.
CREATE OR REPLACE FUNCTION @lib.abi_substr(x text, y bigint, z bigint) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	n bigint := length(x);
	p1 bigint := y;
	p2 bigint := abs(z);
BEGIN
	IF p1 < 0 THEN
		p1 := p1 + n;
		IF p1 < 0 THEN
			p2 := p2 + p1;
			p1 := 0;
		END IF;
	ELSIF p1 > 0 THEN
		p1 := p1 - 1;
	ELSIF p2 > 0 THEN
		p2 := p2 - 1;
	END IF;
	IF z < 0 THEN
		p1 := p1 - p2;
		IF p1 < 0 THEN
			p2 := p2 + p1;
			p1 := 0;
		END IF;
	END IF;
	IF p2 <= 0 THEN
		RETURN '';
	END IF;
	RETURN substr(x, (p1 + 1)::integer, least(p2, 1000000000)::integer);
END $$;

CREATE OR REPLACE FUNCTION @lib.abi_substr(x text, y bigint) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_substr(x, y, 1000000000::bigint);

-- The exact value of a double, as numeric. PostgreSQL's own conversion goes
-- through 15 significant digits, so 2.675 (really 2.67499999…) would round up.
CREATE OR REPLACE FUNCTION @lib.abi_exact(x double precision) RETURNS numeric
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	b bigint := ('x' || encode(float8send(x), 'hex'))::bit(64)::bigint;
	e integer := ((b >> 52) & 2047)::integer;
	m numeric := (b & 4503599627370495)::numeric;
	t text;
BEGIN
	IF e = 2047 THEN
		RETURN x::numeric; -- infinity or NaN
	END IF;
	IF e = 0 THEN
		e := 1;
	ELSE
		m := m + 4503599627370496;
	END IF;
	e := e - 1075;
	IF b < 0 THEN
		m := -m;
	END IF;
	IF e >= 0 THEN
		RETURN m * 2::numeric ^ e;
	END IF;
	-- m × 2^e = m × 5^-e / 10^-e: shift the decimal point of an exact integer.
	t := trunc(abs(m * 5::numeric ^ (-e)))::text; -- ^ returns a scale; trunc drops it
	t := lpad(t, greatest(length(t), -e + 1), '0'); -- lpad also truncates
	RETURN (CASE WHEN m < 0 THEN '-' ELSE '' END || left(t, length(t) + e) || '.' || right(t, -e))::numeric;
END $$;
-- round half away from zero, to at most 30 places, of the exact value, as
-- SQLite does.
CREATE OR REPLACE FUNCTION @lib.abi_round(x double precision, n bigint DEFAULT 0) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x IN ('Infinity', '-Infinity', 'NaN') THEN x
	ELSE round(@lib.abi_exact(x), least(greatest(n, 0), 30)::integer)::double precision END;

CREATE OR REPLACE FUNCTION @lib.abi_sqrt(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x < 0 THEN NULL ELSE sqrt(x) END;

CREATE OR REPLACE FUNCTION @lib.abi_exp(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x > 709.78 THEN 'Infinity'::double precision WHEN x < -745.2 THEN 0 ELSE exp(x) END;

CREATE OR REPLACE FUNCTION @lib.abi_ln(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x <= 0 THEN NULL ELSE ln(x) END;

CREATE OR REPLACE FUNCTION @lib.abi_log10(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x <= 0 THEN NULL ELSE log(x) END;

CREATE OR REPLACE FUNCTION @lib.abi_log2(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x <= 0 THEN NULL ELSE ln(x) / ln(2::double precision) END;

-- log(x) is base 10; log(b, x) is base b.
CREATE OR REPLACE FUNCTION @lib.abi_log(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_log10(x);

CREATE OR REPLACE FUNCTION @lib.abi_log(b double precision, x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN b <= 0 OR b = 1 OR x <= 0 THEN NULL ELSE ln(x) / ln(b) END;

CREATE OR REPLACE FUNCTION @lib.abi_acos(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN abs(x) > 1 THEN NULL ELSE acos(x) END;

CREATE OR REPLACE FUNCTION @lib.abi_asin(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN abs(x) > 1 THEN NULL ELSE asin(x) END;

CREATE OR REPLACE FUNCTION @lib.abi_acosh(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x < 1 THEN NULL ELSE acosh(x) END;

CREATE OR REPLACE FUNCTION @lib.abi_atanh(x double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN abs(x) > 1 THEN NULL WHEN x = 1 THEN 'Infinity' WHEN x = -1 THEN '-Infinity' ELSE atanh(x) END;

-- pow: zero to a negative power is infinite and a negative base with a
-- fractional exponent has no real result (NULL), as in C's pow.
CREATE OR REPLACE FUNCTION @lib.abi_pow(x double precision, y double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN x = 0 AND y < 0 THEN 'Infinity'::double precision
	WHEN x < 0 AND y <> trunc(y) THEN NULL ELSE power(x, y) END;

-- mod is C's fmod: the sign follows x, and a zero divisor gives NULL.
CREATE OR REPLACE FUNCTION @lib.abi_mod(x double precision, y double precision) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN y = 0 THEN NULL ELSE x - y * trunc(x / y) END;

-- glob(pattern, text): * any run, ? one character, [...] a set ([^...] its
-- complement); case-sensitive, matching the whole text.
CREATE OR REPLACE FUNCTION @lib.abi_glob(pattern text, x text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	re text := '';
	i integer := 1;
	n integer := length(pattern);
	c text;
	j integer;
BEGIN
	WHILE i <= n LOOP
		c := substr(pattern, i, 1);
		IF c = '*' THEN
			re := re || '.*';
		ELSIF c = '?' THEN
			re := re || '.';
		ELSIF c = '[' THEN
			j := i + 1;
			IF substr(pattern, j, 1) = '^' THEN j := j + 1; END IF;
			IF substr(pattern, j, 1) = ']' THEN j := j + 1; END IF;
			WHILE j <= n AND substr(pattern, j, 1) <> ']' LOOP j := j + 1; END LOOP;
			IF j > n THEN
				RETURN false; -- an unterminated set matches nothing, as in SQLite
			END IF;
			re := re || '[' || CASE WHEN substr(pattern, i + 1, 1) = '^' THEN '^' || replace(substr(pattern, i + 2, j - i - 2), '\', '\\')
				ELSE replace(substr(pattern, i + 1, j - i - 1), '\', '\\') END || ']';
			i := j;
		ELSE
			re := re || regexp_replace(c, '([.^$|()\[\]{}*+?\\])', '\\\1', 'g');
		END IF;
		i := i + 1;
	END LOOP;
	RETURN x ~ ('^(?:' || re || ')$');
END $$;

-- random() is a 64-bit integer on SQLite.
CREATE OR REPLACE FUNCTION @lib.abi_random() RETURNS bigint
LANGUAGE sql VOLATILE PARALLEL SAFE
RETURN (floor(random() * 18446744073709551616::numeric) - 9223372036854775808::numeric)::bigint;

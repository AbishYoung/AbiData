-- <prefix>_lib, part 9: SQLite's printf (and format, its other name), ADR
-- 0013 Phase F. Arguments arrive as SQLite's text of the value; each
-- conversion reads its argument as SQLite would (%d an integer, %f a double,
-- %s text), so the text carries everything a conversion needs. A double is
-- rounded from its exact value, to at most 16 significant digits, as
-- SQLite's printf does.

-- abi_text_int is text read as a 64-bit integer, as SQLite's printf reads
-- %d's argument: an integer exactly, anything else through its leading
-- number, truncated and kept in range.
CREATE OR REPLACE FUNCTION @lib.abi_text_int(t text) RETURNS numeric
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	v numeric;
BEGIN
	IF t IS NULL THEN
		RETURN 0;
	END IF;
	IF t ~ '^\s*[+-]?[0-9]+\s*$' THEN
		v := btrim(t)::numeric;
	ELSE
		v := trunc(@lib.abi_text_num(t)::numeric);
	END IF;
	RETURN greatest(least(v, 9223372036854775807), -9223372036854775808);
END $$;

-- abi_fmt_exp writes n as d.ddd…e±XX with prec decimals.
CREATE OR REPLACE FUNCTION @lib.abi_fmt_exp(n numeric, prec integer) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	e integer := 0;
	m numeric := 0;
BEGIN
	IF n <> 0 THEN
		e := floor(log(abs(n)))::integer;
		m := round(n / power(10::numeric, e), prec);
		IF abs(m) >= 10 THEN
			e := e + 1;
			m := round(n / power(10::numeric, e), prec);
		END IF;
	END IF;
	RETURN round(m, prec)::text || 'e' || CASE WHEN e < 0 THEN '-' ELSE '+' END || lpad(abs(e)::text, 2, '0');
END $$;

-- abi_fmt_strip removes the trailing zeros of a fraction, and then the point.
CREATE OR REPLACE FUNCTION @lib.abi_fmt_strip(t text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN strpos(t, '.') = 0 THEN t
	ELSE regexp_replace(regexp_replace(t, '0+($|e)', '\1'), '\.($|e)', '\1') END;

-- abi_fmt_commas puts a comma between each group of three digits.
CREATE OR REPLACE FUNCTION @lib.abi_fmt_commas(digits text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN reverse(regexp_replace(reverse(digits), '([0-9]{3})(?=[0-9])', '\1,', 'g'));

-- abi_fmt_octal writes a non-negative integer in base 8.
CREATE OR REPLACE FUNCTION @lib.abi_fmt_octal(v numeric) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	out text := '';
BEGIN
	IF v = 0 THEN
		RETURN '0';
	END IF;
	WHILE v > 0 LOOP
		out := mod(v, 8)::text || out;
		v := div(v, 8);
	END LOOP;
	RETURN out;
END $$;

-- abi_fmt_round is a double's exact value rounded to sig significant digits,
-- or to 16 when more are asked for.
CREATE OR REPLACE FUNCTION @lib.abi_fmt_round(f double precision, sig integer) RETURNS numeric
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_real_digits(f, greatest(least(sig, 16), 1));

-- abi_fmt_bytes cuts t to at most n bytes, at a character boundary.
CREATE OR REPLACE FUNCTION @lib.abi_fmt_bytes(t text, n integer) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	k integer := least(length(t), n);
BEGIN
	WHILE octet_length(left(t, k)) > n LOOP
		k := k - 1;
	END LOOP;
	RETURN left(t, k);
END $$;

CREATE OR REPLACE FUNCTION @lib.abi_printf(fmt text, VARIADIC args text[] DEFAULT '{}') RETURNS text
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	out text := '';
	i integer := 1;
	n integer;
	j integer;
	ai integer := 1;
	m text[];
	flags text;
	width integer;
	prec integer;
	conv text;
	arg text;
	body text;
	sign text;
	prefix text;
	iv numeric;
	f double precision;
	r numeric;
	x integer;
	p integer;
	len integer;
BEGIN
	IF fmt IS NULL THEN
		RETURN NULL;
	END IF;
	n := length(fmt);
	WHILE i <= n LOOP
		IF substr(fmt, i, 1) <> '%' THEN
			j := strpos(substr(fmt, i), '%');
			IF j = 0 THEN
				RETURN out || substr(fmt, i);
			END IF;
			out := out || substr(fmt, i, j - 1);
			i := i + j - 1;
			CONTINUE;
		END IF;
		m := regexp_match(substr(fmt, i + 1), '^([-+ 0#,!]*)(\*|[0-9]*)(\.(\*|[0-9]*))?(l{0,2})(.?)');
		i := i + 1 + length(m[1]) + length(m[2]) + coalesce(length(m[3]), 0) + length(m[5]) + length(m[6]);
		flags := m[1];
		conv := m[6];
		width := 0;
		IF m[2] = '*' THEN
			width := @lib.abi_text_int(args[ai])::integer;
			ai := ai + 1;
			IF width < 0 THEN
				flags := flags || '-';
				width := -width;
			END IF;
		ELSIF m[2] <> '' THEN
			width := m[2]::integer;
		END IF;
		prec := NULL;
		IF m[3] IS NOT NULL THEN
			IF m[4] = '*' THEN
				prec := @lib.abi_text_int(args[ai])::integer;
				ai := ai + 1;
				IF prec < 0 THEN
					prec := -prec;
				END IF;
			ELSE
				prec := coalesce(nullif(m[4], ''), '0')::integer;
			END IF;
		END IF;
		IF conv = '' THEN
			-- A % at the end is written as it is.
			RETURN out || '%';
		ELSIF conv = '%' THEN
			out := out || '%';
			CONTINUE;
		END IF;
		arg := args[ai];
		ai := ai + 1;
		sign := '';
		prefix := '';
		CASE
		WHEN conv IN ('d', 'i') THEN
			iv := @lib.abi_text_int(arg);
			body := abs(iv)::text;
			IF prec IS NOT NULL AND length(body) < prec THEN
				body := lpad(body, prec, '0');
			END IF;
			IF strpos(flags, ',') > 0 THEN
				body := @lib.abi_fmt_commas(body);
			END IF;
			sign := CASE WHEN iv < 0 THEN '-' WHEN strpos(flags, '+') > 0 THEN '+' WHEN strpos(flags, ' ') > 0 THEN ' ' ELSE '' END;
		WHEN conv IN ('x', 'X', 'o') THEN
			iv := @lib.abi_text_int(arg);
			IF conv = 'o' THEN
				body := @lib.abi_fmt_octal(CASE WHEN iv < 0 THEN iv + 18446744073709551616 ELSE iv END);
			ELSE
				body := to_hex(iv::bigint);
				IF conv = 'X' THEN
					body := upper(body);
				END IF;
			END IF;
			IF prec IS NOT NULL AND length(body) < prec THEN
				body := lpad(body, prec, '0');
			END IF;
			IF strpos(flags, '#') > 0 AND iv <> 0 THEN
				prefix := CASE conv WHEN 'x' THEN '0x' WHEN 'X' THEN '0X' ELSE '0' END;
			END IF;
		WHEN conv IN ('f', 'F', 'e', 'E', 'g', 'G') THEN
			f := coalesce(@lib.abi_text_num(arg), 0);
			sign := CASE WHEN f < 0 OR (f = 0 AND arg ~ '^\s*-') THEN '-' WHEN strpos(flags, '+') > 0 THEN '+' WHEN strpos(flags, ' ') > 0 THEN ' ' ELSE '' END;
			f := abs(f);
			p := coalesce(prec, 6);
			x := CASE WHEN f = 0 THEN 0 ELSE floor(log(@lib.abi_exact(f)))::integer END;
			IF conv IN ('f', 'F') THEN
				-- Round once: at the precision, or at 16 digits when that is
				-- coarser.
				r := CASE WHEN x + 1 + p > 16 THEN @lib.abi_fmt_round(f, 16) ELSE @lib.abi_exact(f) END;
				body := round(r, p)::text;
				IF p = 0 AND strpos(flags, '#') > 0 THEN
					body := body || '.';
				END IF;
			ELSIF conv IN ('e', 'E') THEN
				body := @lib.abi_fmt_exp(@lib.abi_fmt_round(f, p + 1), p);
			ELSE
				IF p = 0 THEN
					p := 1;
				END IF;
				r := @lib.abi_fmt_round(f, p);
				x := CASE WHEN r = 0 THEN 0 ELSE floor(log(r))::integer END;
				IF x < -4 OR x >= p THEN
					body := @lib.abi_fmt_exp(r, p - 1);
				ELSE
					body := round(r, p - 1 - x)::text;
				END IF;
				IF strpos(flags, '#') = 0 THEN
					body := @lib.abi_fmt_strip(body);
				END IF;
			END IF;
			IF conv IN ('E', 'G') THEN
				body := upper(body);
			END IF;
		WHEN conv IN ('s', 'z') THEN
			body := coalesce(arg, '');
		WHEN conv = 'q' THEN
			body := replace(coalesce(arg, '(NULL)'), '''', '''''');
		WHEN conv = 'Q' THEN
			body := CASE WHEN arg IS NULL THEN 'NULL' ELSE '''' || replace(arg, '''', '''''') || '''' END;
		WHEN conv = 'w' THEN
			body := replace(coalesce(arg, '(NULL)'), '"', '""');
		WHEN conv = 'c' THEN
			body := repeat(left(coalesce(arg, ''), 1), greatest(coalesce(prec, 1), 1));
			prec := NULL;
		ELSE
			-- An unknown conversion makes the result NULL, as on SQLite.
			RETURN NULL;
		END CASE;
		IF prec IS NOT NULL AND conv IN ('s', 'z', 'q', 'Q', 'w') THEN
			body := CASE WHEN strpos(flags, '!') > 0 THEN left(body, prec) ELSE @lib.abi_fmt_bytes(body, prec) END;
		END IF;
		body := prefix || body;
		len := CASE WHEN conv IN ('s', 'z', 'q', 'Q', 'w', 'c') AND strpos(flags, '!') = 0
			THEN octet_length(body) ELSE length(body) END + length(sign);
		IF len < width THEN
			IF strpos(flags, '-') > 0 THEN
				body := sign || body || repeat(' ', width - len);
			ELSIF strpos(flags, '0') > 0 AND conv NOT IN ('s', 'z', 'q', 'Q', 'w', 'c') THEN
				body := sign || prefix || repeat('0', width - len) || substr(body, length(prefix) + 1);
			ELSE
				body := repeat(' ', width - len) || sign || body;
			END IF;
		ELSE
			body := sign || body;
		END IF;
		out := out || body;
	END LOOP;
	RETURN out;
END $$;

-- <prefix>_lib, part 6: SQLite's JSON reading functions (ADR 0013 Phase F).
-- Documents are jsonb. SQLite's json_extract returns whatever type the
-- document holds, which a PostgreSQL function cannot: abi_json_extract returns
-- the jsonb value, and the renderer converts it to the type its context needs
-- with abi_json_num, abi_json_text or abi_json_bool.

-- abi_json_steps parses a SQLite JSON path ($, .key, ."key", [N], [#], [#-N])
-- into steps: 'k' and a key, 'i' and an index, or 'e' and a count back from
-- the end of an array. A malformed path is an error, as on SQLite.
CREATE OR REPLACE FUNCTION @lib.abi_json_steps(p text) RETURNS text[]
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	steps text[] := '{}';
	i integer := 2;
	n integer := length(p);
	j integer;
	m text[];
BEGIN
	IF left(p, 1) <> '$' THEN
		RAISE EXCEPTION 'bad JSON path: %', p USING ERRCODE = '22023';
	END IF;
	WHILE i <= n LOOP
		IF substr(p, i, 1) = '.' THEN
			IF substr(p, i + 1, 1) = '"' THEN
				j := strpos(substr(p, i + 2), '"');
				IF j = 0 THEN
					RAISE EXCEPTION 'bad JSON path: %', p USING ERRCODE = '22023';
				END IF;
				steps := steps || ('k' || substr(p, i + 2, j - 1));
				i := i + j + 2;
			ELSE
				m := regexp_match(substr(p, i + 1), '^[^.\[]+');
				IF m IS NULL THEN
					RAISE EXCEPTION 'bad JSON path: %', p USING ERRCODE = '22023';
				END IF;
				steps := steps || ('k' || m[1]);
				i := i + 1 + length(m[1]);
			END IF;
		ELSIF substr(p, i, 1) = '[' THEN
			m := regexp_match(substr(p, i), '^\[(?:([0-9]+)|#(?:-([0-9]+))?)\]');
			IF m IS NULL THEN
				RAISE EXCEPTION 'bad JSON path: %', p USING ERRCODE = '22023';
			END IF;
			IF m[1] IS NOT NULL THEN
				steps := steps || ('i' || m[1]);
				i := i + length(m[1]) + 2;
			ELSE
				steps := steps || ('e' || coalesce(m[2], '0'));
				i := i + CASE WHEN m[2] IS NULL THEN 3 ELSE length(m[2]) + 4 END;
			END IF;
		ELSE
			RAISE EXCEPTION 'bad JSON path: %', p USING ERRCODE = '22023';
		END IF;
	END LOOP;
	RETURN steps;
END $$;

-- abi_json_get is the value at a path: NULL when there is none, and JSON null
-- (not SQL NULL) when the document holds null there.
CREATE OR REPLACE FUNCTION @lib.abi_json_get(doc jsonb, p text) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	s text;
	k bigint;
	v jsonb := doc;
BEGIN
	FOREACH s IN ARRAY @lib.abi_json_steps(p) LOOP
		CASE left(s, 1)
		WHEN 'k' THEN
			IF jsonb_typeof(v) <> 'object' THEN
				RETURN NULL;
			END IF;
			v := v -> substr(s, 2);
		ELSE
			IF jsonb_typeof(v) <> 'array' THEN
				RETURN NULL;
			END IF;
			k := substr(s, 2)::bigint;
			IF left(s, 1) = 'e' THEN
				k := jsonb_array_length(v) - k;
			END IF;
			IF k < 0 OR k >= jsonb_array_length(v) THEN
				RETURN NULL;
			END IF;
			v := v -> k::integer;
		END CASE;
		IF v IS NULL THEN
			RETURN NULL;
		END IF;
	END LOOP;
	RETURN v;
END $$;

-- abi_json_extract is json_extract: with one path the value there (SQL NULL
-- for JSON null), with several a JSON array of them.
CREATE OR REPLACE FUNCTION @lib.abi_json_extract(doc jsonb, VARIADIC paths text[]) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	v jsonb;
	out jsonb := '[]';
	p text;
BEGIN
	IF doc IS NULL THEN
		RETURN NULL;
	END IF;
	IF cardinality(paths) = 1 THEN
		v := @lib.abi_json_get(doc, paths[1]);
		RETURN CASE WHEN v = 'null' THEN NULL ELSE v END;
	END IF;
	FOREACH p IN ARRAY paths LOOP
		IF p IS NULL THEN
			RETURN NULL;
		END IF;
		out := out || jsonb_build_array(coalesce(@lib.abi_json_get(doc, p), 'null'));
	END LOOP;
	RETURN out;
END $$;

-- abi_json_min is a value's JSON text without spaces, as SQLite writes it.
-- jsonb keeps one copy of a key and orders keys by length, then bytes.
CREATE OR REPLACE FUNCTION @lib.abi_json_min(v jsonb) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
	CASE jsonb_typeof(v)
	WHEN 'object' THEN
		RETURN '{' || coalesce((SELECT string_agg(to_jsonb(k)::text || ':' || @lib.abi_json_min(x), ',')
			FROM jsonb_each(v) AS e(k, x)), '') || '}';
	WHEN 'array' THEN
		RETURN '[' || coalesce((SELECT string_agg(@lib.abi_json_min(x), ',' ORDER BY n)
			FROM jsonb_array_elements(v) WITH ORDINALITY AS a(x, n)), '') || ']';
	ELSE
		RETURN v::text;
	END CASE;
END $$;

-- abi_text_num is SQLite's reading of text as a number: its leading number,
-- or 0.
CREATE OR REPLACE FUNCTION @lib.abi_text_num(t text) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN coalesce((regexp_match(t, '^\s*([+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)'))[1]::double precision, 0);

-- abi_json_num is an extracted value used as a number: true and false are 1
-- and 0, text is read as SQLite reads it, and an array or object is 0.
CREATE OR REPLACE FUNCTION @lib.abi_json_num(v jsonb) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE jsonb_typeof(v)
	WHEN 'number' THEN (v #>> '{}')::double precision
	WHEN 'boolean' THEN CASE WHEN v = 'true' THEN 1 ELSE 0 END
	WHEN 'string' THEN @lib.abi_text_num(v #>> '{}')
	WHEN 'null' THEN NULL
	ELSE 0 END;

-- abi_json_text is an extracted value used as text: a string unquoted, true
-- and false as 1 and 0, an array or object as its JSON. A number with a
-- fraction or an exponent is a double, as SQLite reads it.
CREATE OR REPLACE FUNCTION @lib.abi_json_text(v jsonb) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE jsonb_typeof(v)
	WHEN 'string' THEN v #>> '{}'
	WHEN 'number' THEN CASE WHEN v::text ~ '[.eE]' THEN @lib.abi_real_text((v #>> '{}')::double precision) ELSE v::text END
	WHEN 'boolean' THEN CASE WHEN v = 'true' THEN '1' ELSE '0' END
	WHEN 'null' THEN NULL
	ELSE @lib.abi_json_min(v) END;

-- abi_json_bool is an extracted value used as a condition: true when it is a
-- number other than 0.
CREATE OR REPLACE FUNCTION @lib.abi_json_bool(v jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_json_num(v) <> 0;

-- abi_json_type is json_type: 'object', 'array', 'text', 'integer', 'real',
-- 'true', 'false' or 'null'; NULL when the path has no value.
CREATE OR REPLACE FUNCTION @lib.abi_json_type(doc jsonb, p text DEFAULT '$') RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	v jsonb := @lib.abi_json_get(doc, p);
BEGIN
	RETURN CASE jsonb_typeof(v)
		WHEN 'string' THEN 'text'
		WHEN 'number' THEN CASE WHEN v::text ~ '[.eE]' THEN 'real' ELSE 'integer' END
		WHEN 'boolean' THEN v::text
		ELSE jsonb_typeof(v) END;
END $$;

-- abi_json_array_length is json_array_length: 0 for a value that is not an
-- array, NULL when the path has no value.
CREATE OR REPLACE FUNCTION @lib.abi_json_array_length(doc jsonb, p text DEFAULT '$') RETURNS bigint
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	v jsonb := @lib.abi_json_get(doc, p);
BEGIN
	RETURN CASE WHEN v IS NULL THEN NULL WHEN jsonb_typeof(v) = 'array' THEN jsonb_array_length(v) ELSE 0 END;
END $$;

-- abi_json_valid is json_valid: whether the text is RFC 8259 JSON.
CREATE OR REPLACE FUNCTION @lib.abi_json_valid(t text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
	PERFORM CAST(t AS jsonb);
	RETURN true;
EXCEPTION WHEN others THEN
	RETURN false;
END $$;

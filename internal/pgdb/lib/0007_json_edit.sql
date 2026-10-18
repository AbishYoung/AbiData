-- <prefix>_lib, part 7: SQLite's JSON functions that build and change
-- documents (ADR 0013 Phase F), on jsonb. Paths are SQLite's (abi_json_steps).

-- abi_json_arg is an extracted value used as a JSON argument: true and false
-- are 1 and 0, as json_extract returns them on SQLite.
CREATE OR REPLACE FUNCTION @lib.abi_json_arg(v jsonb) RETURNS jsonb
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN CASE WHEN jsonb_typeof(v) = 'boolean' THEN CASE WHEN v = 'true' THEN '1'::jsonb ELSE '0'::jsonb END ELSE v END;

-- abi_jv is a value as JSON, and abi_json_array and abi_json_object build
-- documents: IMMUTABLE forms of to_jsonb and jsonb_build_*, which accept any
-- type and so are only STABLE, for generated columns. NULL is JSON null.
CREATE OR REPLACE FUNCTION @lib.abi_jv(x text) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN to_jsonb(x);
CREATE OR REPLACE FUNCTION @lib.abi_jv(x bigint) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN to_jsonb(x);
CREATE OR REPLACE FUNCTION @lib.abi_jv(x double precision) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN to_jsonb(x);

CREATE OR REPLACE FUNCTION @lib.abi_json_array(VARIADIC vals jsonb[]) RETURNS jsonb
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN to_jsonb(vals);

-- abi_json_object takes keys and values in turn, as json_object does; a key
-- that repeats keeps its last value.
CREATE OR REPLACE FUNCTION @lib.abi_json_object(keys text[], vals jsonb[]) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	out jsonb := '{}';
	i integer;
BEGIN
	FOR i IN 1..coalesce(cardinality(keys), 0) LOOP
		IF keys[i] IS NULL THEN
			RAISE EXCEPTION 'json_object() labels must be TEXT' USING ERRCODE = '22023';
		END IF;
		out := out || jsonb_build_object(keys[i], coalesce(vals[i], 'null'));
	END LOOP;
	RETURN out;
END $$;

-- abi_json_build is val under the steps of a path that does not exist yet:
-- a key makes an object, and [0] or [#] an array; NULL for any other index,
-- which SQLite does not create.
CREATE OR REPLACE FUNCTION @lib.abi_json_build(steps text[], val jsonb) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	v jsonb := val;
	i integer;
BEGIN
	FOR i IN REVERSE cardinality(steps)..1 LOOP
		IF left(steps[i], 1) = 'k' THEN
			v := jsonb_build_object(substr(steps[i], 2), v);
		ELSIF steps[i] IN ('i0', 'e0') THEN
			v := jsonb_build_array(v);
		ELSE
			RETURN NULL;
		END IF;
	END LOOP;
	RETURN v;
END $$;

-- abi_json_put writes val at steps: mode 'insert' only where nothing is,
-- 'replace' only where something is, 'set' either way. A path through a value
-- of the wrong kind, or past the end of an array, changes nothing.
CREATE OR REPLACE FUNCTION @lib.abi_json_put(v jsonb, steps text[], val jsonb, mode text) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	s text := steps[1];
	rest text[] := steps[2:];
	child jsonb;
	built jsonb;
	k text;
	n integer;
	idx integer;
BEGIN
	IF cardinality(steps) = 0 THEN
		RETURN CASE WHEN mode = 'insert' THEN v ELSE val END;
	END IF;
	IF left(s, 1) = 'k' THEN
		IF jsonb_typeof(v) <> 'object' THEN
			RETURN v;
		END IF;
		k := substr(s, 2);
		child := v -> k;
		IF child IS NULL THEN
			IF mode = 'replace' THEN
				RETURN v;
			END IF;
			built := @lib.abi_json_build(rest, val);
			RETURN CASE WHEN built IS NULL THEN v ELSE v || jsonb_build_object(k, built) END;
		END IF;
		RETURN v || jsonb_build_object(k, @lib.abi_json_put(child, rest, val, mode));
	END IF;
	IF jsonb_typeof(v) <> 'array' THEN
		RETURN v;
	END IF;
	n := jsonb_array_length(v);
	idx := substr(s, 2)::integer;
	IF left(s, 1) = 'e' THEN
		idx := n - idx;
	END IF;
	IF idx >= 0 AND idx < n THEN
		RETURN jsonb_set(v, ARRAY[idx::text], @lib.abi_json_put(v -> idx, rest, val, mode));
	END IF;
	IF idx = n AND mode <> 'replace' THEN
		built := @lib.abi_json_build(rest, val);
		RETURN CASE WHEN built IS NULL THEN v ELSE v || jsonb_build_array(built) END;
	END IF;
	RETURN v;
END $$;

-- abi_json_edit is json_insert, json_replace and json_set: each path and value
-- in turn.
CREATE OR REPLACE FUNCTION @lib.abi_json_edit(mode text, doc jsonb, VARIADIC pv jsonb[]) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	v jsonb := doc;
	i integer := 1;
BEGIN
	IF doc IS NULL THEN
		RETURN NULL;
	END IF;
	WHILE i < cardinality(pv) LOOP
		IF pv[i] IS NULL OR jsonb_typeof(pv[i]) = 'null' THEN
			RETURN NULL;
		END IF;
		v := @lib.abi_json_put(v, @lib.abi_json_steps(pv[i] #>> '{}'), coalesce(pv[i + 1], 'null'), mode);
		i := i + 2;
	END LOOP;
	RETURN v;
END $$;

-- abi_json_del removes the value at steps; NULL when steps is empty (the
-- whole document).
CREATE OR REPLACE FUNCTION @lib.abi_json_del(v jsonb, steps text[]) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	s text := steps[1];
	child jsonb;
	n integer;
	idx integer;
BEGIN
	IF cardinality(steps) = 0 THEN
		RETURN NULL;
	END IF;
	IF left(s, 1) = 'k' THEN
		IF jsonb_typeof(v) <> 'object' OR NOT v ? substr(s, 2) THEN
			RETURN v;
		END IF;
		IF cardinality(steps) = 1 THEN
			RETURN v - substr(s, 2);
		END IF;
		child := @lib.abi_json_del(v -> substr(s, 2), steps[2:]);
		RETURN v || jsonb_build_object(substr(s, 2), child);
	END IF;
	IF jsonb_typeof(v) <> 'array' THEN
		RETURN v;
	END IF;
	n := jsonb_array_length(v);
	idx := substr(s, 2)::integer;
	IF left(s, 1) = 'e' THEN
		idx := n - idx;
	END IF;
	IF idx < 0 OR idx >= n THEN
		RETURN v;
	END IF;
	IF cardinality(steps) = 1 THEN
		RETURN v - idx;
	END IF;
	RETURN jsonb_set(v, ARRAY[idx::text], @lib.abi_json_del(v -> idx, steps[2:]));
END $$;

-- abi_json_remove is json_remove: each path in turn.
CREATE OR REPLACE FUNCTION @lib.abi_json_remove(doc jsonb, VARIADIC paths text[]) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
	v jsonb := doc;
	p text;
BEGIN
	FOREACH p IN ARRAY paths LOOP
		IF v IS NULL OR p IS NULL THEN
			RETURN NULL;
		END IF;
		v := @lib.abi_json_del(v, @lib.abi_json_steps(p));
	END LOOP;
	RETURN v;
END $$;

-- abi_json_patch is json_patch: an RFC 7396 merge patch.
CREATE OR REPLACE FUNCTION @lib.abi_json_patch(target jsonb, patch jsonb) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	out jsonb := target;
	k text;
	pv jsonb;
BEGIN
	IF jsonb_typeof(patch) <> 'object' THEN
		RETURN patch;
	END IF;
	IF jsonb_typeof(out) <> 'object' THEN
		out := '{}';
	END IF;
	FOR k, pv IN SELECT * FROM jsonb_each(patch) LOOP
		IF jsonb_typeof(pv) = 'null' THEN
			out := out - k;
		ELSE
			out := out || jsonb_build_object(k, @lib.abi_json_patch(coalesce(out -> k, 'null'), pv));
		END IF;
	END LOOP;
	RETURN out;
END $$;

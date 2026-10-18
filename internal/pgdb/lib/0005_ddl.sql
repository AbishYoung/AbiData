-- <prefix>_lib, part 5: what user tables' DDL needs on PostgreSQL (ADR 0013
-- Phase F). Generated columns may use only IMMUTABLE functions, so text
-- becomes a date or timestamptz through SQLite's own parser (abi_dt_parse)
-- rather than PostgreSQL's casts, which depend on DateStyle and TimeZone.

-- abi_to_date reads a date (or the date of a date and time) as SQLite would;
-- NULL when it is not one.
CREATE OR REPLACE FUNCTION @lib.abi_to_date(t text) RETURNS date
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
	ijd bigint := @lib.abi_dt_parse(t);
	c record;
BEGIN
	IF ijd IS NULL THEN
		RETURN NULL;
	END IF;
	c := @lib.abi_dt_ymd(ijd);
	-- SQLite counts years astronomically (year 0 is 1 BC); make_date reads a
	-- negative year as BC, which has no year 0.
	RETURN make_date(CASE WHEN c.y <= 0 THEN c.y - 1 ELSE c.y END, c.mo, c.d);
END $$;

-- abi_to_timestamptz reads a date and time as SQLite would (a time without a
-- zone is UTC); NULL when it is not one.
CREATE OR REPLACE FUNCTION @lib.abi_to_timestamptz(t text) RETURNS timestamptz
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN to_timestamp((@lib.abi_dt_parse(t) - 210866760000000) / 1000.0);

-- abi_occ_bump is the optimistic-concurrency safety net (ADR 0012 §4): an
-- UPDATE that leaves "_version" unchanged bumps it.
CREATE OR REPLACE FUNCTION @lib.abi_occ_bump() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	IF NEW."_version" = OLD."_version" THEN
		NEW."_version" := OLD."_version" + 1;
	END IF;
	RETURN NEW;
END $$;

-- abi_att_refs keeps the table's schema's _sys_file_refs in step with its
-- attachments columns (ADR 0011), as the SQLite triggers do. Arguments: the
-- catalog table ID, then a catalog column ID and physical name per
-- attachments column. It runs as its owner: a sandboxed write (Lua db.exec)
-- may change user tables but has no rights on system tables.
CREATE OR REPLACE FUNCTION @lib.abi_att_refs() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE
	refs text := format('%I._sys_file_refs', TG_TABLE_SCHEMA);
	tid text := TG_ARGV[0];
	cid text;
	files jsonb;
	i integer := 1;
BEGIN
	IF TG_OP = 'DELETE' THEN
		EXECUTE format('DELETE FROM %s WHERE table_id = $1 AND row_id = $2', refs) USING tid, OLD."id";
		RETURN NULL;
	END IF;
	WHILE i < TG_NARGS LOOP
		cid := TG_ARGV[i];
		files := to_jsonb(NEW) -> TG_ARGV[i + 1];
		IF jsonb_typeof(files) <> 'array' THEN
			files := '[]';
		END IF;
		IF TG_OP = 'UPDATE' THEN
			EXECUTE format('DELETE FROM %s WHERE table_id = $1 AND row_id = $2 AND column_id = $3
				AND file_id NOT IN (SELECT v #>> ''{}'' FROM jsonb_array_elements($4) v WHERE jsonb_typeof(v) = ''string'')', refs)
				USING tid, OLD."id", cid, files;
		END IF;
		EXECUTE format('INSERT INTO %s (file_id, table_id, row_id, column_id)
			SELECT v #>> ''{}'', $1, $2, $3 FROM jsonb_array_elements($4) v WHERE jsonb_typeof(v) = ''string''
			ON CONFLICT DO NOTHING', refs)
			USING tid, NEW."id", cid, files;
		i := i + 2;
	END LOOP;
	RETURN NULL;
END $$;

-- IMMUTABLE forms of builtins that are only STABLE (they accept any type, so
-- their output could depend on settings), for generated columns. The renderer
-- passes text, so these take text.
CREATE OR REPLACE FUNCTION @lib.abi_concat(VARIADIC parts text[]) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN array_to_string(parts, '');

CREATE OR REPLACE FUNCTION @lib.abi_concat_ws(sep text, VARIADIC parts text[]) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN CASE WHEN sep IS NOT NULL THEN array_to_string(parts, sep) END;

-- hex: the UTF-8 bytes of the text, in upper case; '' for NULL, as on SQLite.
CREATE OR REPLACE FUNCTION @lib.abi_hex(t text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN coalesce(upper(encode(convert_to(t, 'UTF8'), 'hex')), '');

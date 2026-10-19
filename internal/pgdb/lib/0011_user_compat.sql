-- <prefix>_lib, part 11 (ADR 0012 §8): overloads for SQLite idioms in SQL a
-- person typed (saved queries, Lua db), for argument types PostgreSQL's
-- builtins do not take. SQLite stores dates and times as text and has one
-- REAL type, so substr() of a date and round() of a REAL with digits are
-- common there and errors here. Overloads only add signatures pg_catalog
-- lacks, so no builtin call changes meaning. Each answers as SQLite would.

-- substr of a date or time reads its stored text: 2026-01-05, or
-- 2026-01-05T10:00:00.000Z in UTC.
CREATE OR REPLACE FUNCTION @lib.substr(x date, y integer, z integer) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_substr(to_char(x, 'YYYY-MM-DD'), y, z);

CREATE OR REPLACE FUNCTION @lib.substr(x date, y integer) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_substr(to_char(x, 'YYYY-MM-DD'), y);

CREATE OR REPLACE FUNCTION @lib.substr(x timestamptz, y integer, z integer) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_substr(to_char(x AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), y, z);

CREATE OR REPLACE FUNCTION @lib.substr(x timestamptz, y integer) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_substr(to_char(x AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), y);

-- round of a REAL to n digits, as SQLite's round(x, n).
CREATE OR REPLACE FUNCTION @lib.round(x double precision, n integer) RETURNS double precision
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
RETURN @lib.abi_round(x, n);

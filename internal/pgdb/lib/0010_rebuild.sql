-- <prefix>_lib, part 10: the catalog's table rebuild (ADR 0012 Phase 3).
-- SQLite copies the rows with INSERT OR IGNORE, so rows that break the new
-- design are left out and counted. PostgreSQL has no such INSERT: the catalog
-- tries the copy in one statement first and, when a row breaks a rule, copies
-- row by row here instead, each in its own subtransaction.

-- abi_copy_rows runs ins (an INSERT … SELECT … WHERE id = $1) for each id of
-- the table src (a quoted name), and returns how many rows it copied, how
-- many broke a rule, and the first 20 ids of those.
CREATE OR REPLACE FUNCTION @lib.abi_copy_rows(src text, ins text, OUT copied bigint, OUT skipped bigint, OUT sample bigint[])
LANGUAGE plpgsql AS $$
DECLARE
	rid bigint;
BEGIN
	copied := 0;
	skipped := 0;
	sample := '{}';
	FOR rid IN EXECUTE 'SELECT id FROM ' || src || ' ORDER BY id' LOOP
		BEGIN
			EXECUTE ins USING rid;
			copied := copied + 1;
		EXCEPTION WHEN integrity_constraint_violation OR data_exception THEN
			skipped := skipped + 1;
			IF skipped <= 20 THEN
				sample := sample || rid;
			END IF;
		END;
	END LOOP;
END $$;
-- It runs SQL it is given: only the app's role, which owns it, may call it.
REVOKE ALL ON FUNCTION @lib.abi_copy_rows(text, text) FROM PUBLIC;

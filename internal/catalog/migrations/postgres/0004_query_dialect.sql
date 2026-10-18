-- ADR 0012 §4: a saved query records the SQL dialect its sql_text is in.
-- Visual queries are compiled for the workspace's backend; SQL queries keep
-- the dialect they were written in.
ALTER TABLE _sys_queries ADD COLUMN dialect text COLLATE "C" NOT NULL DEFAULT 'sqlite'
  CHECK (dialect IN ('sqlite','postgres'));

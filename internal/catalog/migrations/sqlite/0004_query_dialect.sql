-- ADR 0012 §4: a saved query records the SQL dialect its sql_text is in. SQL
-- queries keep the dialect they were written in; visual queries are compiled
-- for the workspace's backend, so theirs follows it. Every query written so far
-- is SQLite.
ALTER TABLE _sys_queries ADD COLUMN dialect TEXT NOT NULL DEFAULT 'sqlite'
  CHECK (dialect IN ('sqlite','postgres'));

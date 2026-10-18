-- Workspace catalog schema v1 (brief <catalog_schema>).
CREATE TABLE _sys_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;
-- keys: workspace_id, timezone (IANA), locale, created_at, schema_version (bumped on every applied change)

CREATE TABLE _sys_tables (
  id TEXT PRIMARY KEY, physical_name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  display_name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
  track_history INTEGER NOT NULL DEFAULT 0 CHECK (track_history IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_relationships (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  parent_table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,   -- parent key is always id
  child_table_id  TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  child_column_id TEXT NOT NULL,
  on_delete TEXT NOT NULL DEFAULT 'RESTRICT' CHECK (on_delete IN ('RESTRICT','CASCADE','SET NULL')),
  enforce INTEGER NOT NULL DEFAULT 1 CHECK (enforce IN (0,1))
) STRICT;

CREATE TABLE _sys_columns (
  id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name TEXT NOT NULL COLLATE NOCASE, display_name TEXT NOT NULL,
  field_type TEXT NOT NULL CHECK (field_type IN ('autonumber','short_text','long_text','integer','number',
    'currency','yes_no','date','datetime','lookup','hyperlink','calculated')),
  type_options TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(type_options)), -- {"max_length":255} {"expression":"..."} {"format":"..."}
  is_required INTEGER NOT NULL DEFAULT 0 CHECK (is_required IN (0,1)),
  default_sql TEXT, validation_sql TEXT, validation_text TEXT,
  lookup_relationship_id TEXT REFERENCES _sys_relationships(id) ON DELETE SET NULL,
  lookup_display_column_id TEXT,
  description TEXT NOT NULL DEFAULT '', ordinal INTEGER NOT NULL,
  is_hidden INTEGER NOT NULL DEFAULT 0 CHECK (is_hidden IN (0,1)),
  UNIQUE (table_id, physical_name)
) STRICT;

CREATE TABLE _sys_indexes (
  id TEXT PRIMARY KEY, table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name TEXT NOT NULL UNIQUE, is_unique INTEGER NOT NULL CHECK (is_unique IN (0,1)),
  column_ids TEXT NOT NULL CHECK (json_valid(column_ids))
) STRICT;

CREATE TABLE _sys_queries (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  kind TEXT NOT NULL CHECK (kind IN ('visual','sql')),
  visual_model TEXT CHECK (visual_model IS NULL OR json_valid(visual_model)),
  sql_text TEXT NOT NULL,                       -- compiled (visual) or authored (sql)
  parameters TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(parameters)),
  description TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_scripts (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  kind TEXT NOT NULL CHECK (kind IN ('module','form_module','report_module','job')),
  owner_id TEXT,                                -- form/report id for *_module
  source TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_forms (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('table','query','none')), source_id TEXT,
  default_view TEXT NOT NULL DEFAULT 'single' CHECK (default_view IN ('single','continuous','datasheet')),
  allow_edits INTEGER NOT NULL DEFAULT 1, allow_additions INTEGER NOT NULL DEFAULT 1, allow_deletions INTEGER NOT NULL DEFAULT 1,
  layout TEXT NOT NULL CHECK (json_valid(layout)),
  version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_reports (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('table','query')), source_id TEXT NOT NULL,
  layout TEXT NOT NULL CHECK (json_valid(layout)), page_setup TEXT NOT NULL CHECK (json_valid(page_setup)),
  version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_jobs (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  script_id TEXT NOT NULL REFERENCES _sys_scripts(id) ON DELETE RESTRICT,
  schedule TEXT, timezone TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  timeout_seconds INTEGER NOT NULL DEFAULT 300 CHECK (timeout_seconds BETWEEN 1 AND 3600),
  overlap_policy TEXT NOT NULL DEFAULT 'skip' CHECK (overlap_policy IN ('skip','queue')),
  misfire_policy TEXT NOT NULL DEFAULT 'run_once' CHECK (misfire_policy IN ('run_once','skip')),
  max_retries INTEGER NOT NULL DEFAULT 0, retry_backoff_seconds INTEGER NOT NULL DEFAULT 60,
  args TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(args)),
  version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_job_runs (
  id TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES _sys_jobs(id) ON DELETE CASCADE,
  trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('schedule','manual','enqueue','retry')),
  status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','timed_out','cancelled','interrupted')),
  attempt INTEGER NOT NULL DEFAULT 1, triggered_by TEXT,
  queued_at TEXT NOT NULL, started_at TEXT, finished_at TEXT,
  rows_read INTEGER NOT NULL DEFAULT 0, rows_written INTEGER NOT NULL DEFAULT 0,
  error_code TEXT, error_message TEXT, log_text TEXT NOT NULL DEFAULT ''   -- capped 256 KiB, tail kept
) STRICT;
CREATE INDEX _sys_job_runs_job ON _sys_job_runs(job_id, queued_at DESC);

CREATE TABLE _sys_operations (
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','cancelled')),
  progress REAL NOT NULL DEFAULT 0, result TEXT, error_code TEXT, error_message TEXT,
  created_by TEXT NOT NULL, created_at TEXT NOT NULL, finished_at TEXT
) STRICT;

CREATE TABLE _sys_dependencies (
  dependent_kind TEXT NOT NULL, dependent_id TEXT NOT NULL,
  target_kind TEXT NOT NULL CHECK (target_kind IN ('table','column','query','script','form','report')),
  target_id TEXT NOT NULL, confidence TEXT NOT NULL CHECK (confidence IN ('exact','heuristic')),
  PRIMARY KEY (dependent_kind, dependent_id, target_kind, target_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX _sys_dependencies_target ON _sys_dependencies(target_kind, target_id);

CREATE TABLE _sys_idempotency (
  key TEXT PRIMARY KEY, user_id TEXT NOT NULL, request_hash TEXT NOT NULL,
  status_code INTEGER NOT NULL, response TEXT NOT NULL, created_at TEXT NOT NULL
) STRICT;   -- purge > 24h

CREATE TABLE _sys_audit (id INTEGER PRIMARY KEY, at TEXT NOT NULL, user_id TEXT, action TEXT NOT NULL,
  object_kind TEXT, object_id TEXT, details TEXT CHECK (details IS NULL OR json_valid(details))) STRICT;

CREATE TABLE _sys_row_history (id INTEGER PRIMARY KEY, table_id TEXT NOT NULL, row_id INTEGER NOT NULL,
  row_version INTEGER NOT NULL, op TEXT NOT NULL CHECK (op IN ('insert','update','delete')),
  user_id TEXT, at TEXT NOT NULL, before TEXT, after TEXT) STRICT;
CREATE INDEX _sys_row_history_row ON _sys_row_history(table_id, row_id, id);

CREATE TABLE _sys_egress_allowlist (pattern TEXT PRIMARY KEY, ports TEXT NOT NULL DEFAULT '443', note TEXT NOT NULL DEFAULT '') STRICT;

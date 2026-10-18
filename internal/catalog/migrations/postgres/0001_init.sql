-- Workspace catalog schema v1 on PostgreSQL (ADR 0012 §4). The same tables,
-- columns and column types as SQLite's: timestamps are text, flags 0/1
-- integers, JSON text, so the statements are the same on both. NOCASE becomes
-- <prefix>_lib's abi_ci; json_valid its abi_json_valid. An INTEGER PRIMARY KEY
-- (a rowid) becomes an identity column.
CREATE TABLE _sys_meta (key text COLLATE "C" PRIMARY KEY, value text COLLATE "C" NOT NULL);
-- keys: workspace_id, timezone (IANA), locale, created_at, schema_version (bumped on every applied change)

CREATE TABLE _sys_tables (
  id text COLLATE "C" PRIMARY KEY, physical_name text COLLATE @lib.abi_ci NOT NULL UNIQUE,
  display_name text COLLATE "C" NOT NULL, description text COLLATE "C" NOT NULL DEFAULT '',
  track_history bigint NOT NULL DEFAULT 0 CHECK (track_history IN (0,1)),
  version bigint NOT NULL DEFAULT 1, created_at text COLLATE "C" NOT NULL, updated_at text COLLATE "C" NOT NULL
);

CREATE TABLE _sys_relationships (
  id text COLLATE "C" PRIMARY KEY, name text COLLATE @lib.abi_ci NOT NULL UNIQUE,
  parent_table_id text COLLATE "C" NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,   -- parent key is always id
  child_table_id  text COLLATE "C" NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  child_column_id text COLLATE "C" NOT NULL,
  on_delete text COLLATE "C" NOT NULL DEFAULT 'RESTRICT' CHECK (on_delete IN ('RESTRICT','CASCADE','SET NULL')),
  enforce bigint NOT NULL DEFAULT 1 CHECK (enforce IN (0,1))
);

CREATE TABLE _sys_columns (
  id text COLLATE "C" PRIMARY KEY,
  table_id text COLLATE "C" NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name text COLLATE @lib.abi_ci NOT NULL, display_name text COLLATE "C" NOT NULL,
  field_type text COLLATE "C" NOT NULL CONSTRAINT _sys_columns_field_type_check CHECK (field_type IN ('autonumber','short_text','long_text','integer','number',
    'currency','yes_no','date','datetime','lookup','hyperlink','calculated')),
  type_options text COLLATE "C" NOT NULL DEFAULT '{}' CHECK (@lib.abi_json_valid(type_options)),
  is_required bigint NOT NULL DEFAULT 0 CHECK (is_required IN (0,1)),
  default_sql text COLLATE "C", validation_sql text COLLATE "C", validation_text text COLLATE "C",
  lookup_relationship_id text COLLATE "C" REFERENCES _sys_relationships(id) ON DELETE SET NULL,
  lookup_display_column_id text COLLATE "C",
  description text COLLATE "C" NOT NULL DEFAULT '', ordinal bigint NOT NULL,
  is_hidden bigint NOT NULL DEFAULT 0 CHECK (is_hidden IN (0,1)),
  UNIQUE (table_id, physical_name)
);

CREATE TABLE _sys_indexes (
  id text COLLATE "C" PRIMARY KEY, table_id text COLLATE "C" NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name text COLLATE "C" NOT NULL UNIQUE, is_unique bigint NOT NULL CHECK (is_unique IN (0,1)),
  column_ids text COLLATE "C" NOT NULL CHECK (@lib.abi_json_valid(column_ids))
);

CREATE TABLE _sys_queries (
  id text COLLATE "C" PRIMARY KEY, name text COLLATE @lib.abi_ci NOT NULL UNIQUE,
  kind text COLLATE "C" NOT NULL CHECK (kind IN ('visual','sql')),
  visual_model text COLLATE "C" CHECK (visual_model IS NULL OR @lib.abi_json_valid(visual_model)),
  sql_text text COLLATE "C" NOT NULL,
  parameters text COLLATE "C" NOT NULL DEFAULT '[]' CHECK (@lib.abi_json_valid(parameters)),
  description text COLLATE "C" NOT NULL DEFAULT '', version bigint NOT NULL DEFAULT 1,
  updated_at text COLLATE "C" NOT NULL, updated_by text COLLATE "C" NOT NULL
);

CREATE TABLE _sys_scripts (
  id text COLLATE "C" PRIMARY KEY, name text COLLATE @lib.abi_ci NOT NULL UNIQUE,
  kind text COLLATE "C" NOT NULL CHECK (kind IN ('module','form_module','report_module','job')),
  owner_id text COLLATE "C",
  source text COLLATE "C" NOT NULL, version bigint NOT NULL DEFAULT 1,
  updated_at text COLLATE "C" NOT NULL, updated_by text COLLATE "C" NOT NULL
);

CREATE TABLE _sys_forms (
  id text COLLATE "C" PRIMARY KEY, name text COLLATE @lib.abi_ci NOT NULL UNIQUE,
  source_kind text COLLATE "C" NOT NULL CHECK (source_kind IN ('table','query','none')), source_id text COLLATE "C",
  default_view text COLLATE "C" NOT NULL DEFAULT 'single' CHECK (default_view IN ('single','continuous','datasheet')),
  allow_edits bigint NOT NULL DEFAULT 1, allow_additions bigint NOT NULL DEFAULT 1, allow_deletions bigint NOT NULL DEFAULT 1,
  layout text COLLATE "C" NOT NULL CHECK (@lib.abi_json_valid(layout)),
  version bigint NOT NULL DEFAULT 1, updated_at text COLLATE "C" NOT NULL, updated_by text COLLATE "C" NOT NULL
);

CREATE TABLE _sys_reports (
  id text COLLATE "C" PRIMARY KEY, name text COLLATE @lib.abi_ci NOT NULL UNIQUE,
  source_kind text COLLATE "C" NOT NULL CHECK (source_kind IN ('table','query')), source_id text COLLATE "C" NOT NULL,
  layout text COLLATE "C" NOT NULL CHECK (@lib.abi_json_valid(layout)), page_setup text COLLATE "C" NOT NULL CHECK (@lib.abi_json_valid(page_setup)),
  version bigint NOT NULL DEFAULT 1, updated_at text COLLATE "C" NOT NULL, updated_by text COLLATE "C" NOT NULL
);

CREATE TABLE _sys_jobs (
  id text COLLATE "C" PRIMARY KEY, name text COLLATE @lib.abi_ci NOT NULL UNIQUE,
  script_id text COLLATE "C" NOT NULL REFERENCES _sys_scripts(id) ON DELETE RESTRICT,
  schedule text COLLATE "C", timezone text COLLATE "C" NOT NULL, enabled bigint NOT NULL DEFAULT 1,
  timeout_seconds bigint NOT NULL DEFAULT 300 CHECK (timeout_seconds BETWEEN 1 AND 3600),
  overlap_policy text COLLATE "C" NOT NULL DEFAULT 'skip' CHECK (overlap_policy IN ('skip','queue')),
  misfire_policy text COLLATE "C" NOT NULL DEFAULT 'run_once' CHECK (misfire_policy IN ('run_once','skip')),
  max_retries bigint NOT NULL DEFAULT 0, retry_backoff_seconds bigint NOT NULL DEFAULT 60,
  args text COLLATE "C" NOT NULL DEFAULT '{}' CHECK (@lib.abi_json_valid(args)),
  version bigint NOT NULL DEFAULT 1, updated_at text COLLATE "C" NOT NULL, updated_by text COLLATE "C" NOT NULL
);

CREATE TABLE _sys_job_runs (
  id text COLLATE "C" PRIMARY KEY, job_id text COLLATE "C" NOT NULL REFERENCES _sys_jobs(id) ON DELETE CASCADE,
  trigger_kind text COLLATE "C" NOT NULL CHECK (trigger_kind IN ('schedule','manual','enqueue','retry')),
  status text COLLATE "C" NOT NULL CHECK (status IN ('queued','running','succeeded','failed','timed_out','cancelled','interrupted')),
  attempt bigint NOT NULL DEFAULT 1, triggered_by text COLLATE "C",
  queued_at text COLLATE "C" NOT NULL, started_at text COLLATE "C", finished_at text COLLATE "C",
  rows_read bigint NOT NULL DEFAULT 0, rows_written bigint NOT NULL DEFAULT 0,
  error_code text COLLATE "C", error_message text COLLATE "C", log_text text COLLATE "C" NOT NULL DEFAULT ''
);
CREATE INDEX _sys_job_runs_job ON _sys_job_runs(job_id, queued_at DESC);

CREATE TABLE _sys_operations (
  id text COLLATE "C" PRIMARY KEY, kind text COLLATE "C" NOT NULL,
  status text COLLATE "C" NOT NULL CHECK (status IN ('queued','running','succeeded','failed','cancelled')),
  progress double precision NOT NULL DEFAULT 0, result text COLLATE "C", error_code text COLLATE "C", error_message text COLLATE "C",
  created_by text COLLATE "C" NOT NULL, created_at text COLLATE "C" NOT NULL, finished_at text COLLATE "C"
);

CREATE TABLE _sys_dependencies (
  dependent_kind text COLLATE "C" NOT NULL, dependent_id text COLLATE "C" NOT NULL,
  target_kind text COLLATE "C" NOT NULL CHECK (target_kind IN ('table','column','query','script','form','report')),
  target_id text COLLATE "C" NOT NULL, confidence text COLLATE "C" NOT NULL CHECK (confidence IN ('exact','heuristic')),
  PRIMARY KEY (dependent_kind, dependent_id, target_kind, target_id)
);
CREATE INDEX _sys_dependencies_target ON _sys_dependencies(target_kind, target_id);

CREATE TABLE _sys_idempotency (
  key text COLLATE "C" PRIMARY KEY, user_id text COLLATE "C" NOT NULL, request_hash text COLLATE "C" NOT NULL,
  status_code bigint NOT NULL, response text COLLATE "C" NOT NULL, created_at text COLLATE "C" NOT NULL
);   -- purge > 24h

CREATE TABLE _sys_audit (id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, at text COLLATE "C" NOT NULL, user_id text COLLATE "C",
  action text COLLATE "C" NOT NULL, object_kind text COLLATE "C", object_id text COLLATE "C",
  details text COLLATE "C" CHECK (details IS NULL OR @lib.abi_json_valid(details)));

CREATE TABLE _sys_row_history (id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, table_id text COLLATE "C" NOT NULL, row_id bigint NOT NULL,
  row_version bigint NOT NULL, op text COLLATE "C" NOT NULL CHECK (op IN ('insert','update','delete')),
  user_id text COLLATE "C", at text COLLATE "C" NOT NULL, before text COLLATE "C", after text COLLATE "C");
CREATE INDEX _sys_row_history_row ON _sys_row_history(table_id, row_id, id);

CREATE TABLE _sys_egress_allowlist (pattern text COLLATE "C" PRIMARY KEY, ports text COLLATE "C" NOT NULL DEFAULT '443',
  note text COLLATE "C" NOT NULL DEFAULT '');

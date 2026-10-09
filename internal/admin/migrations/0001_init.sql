-- Admin DB schema v1. Timestamps are TEXT 'YYYY-MM-DDTHH:MM:SS.sssZ' UTC.
CREATE TABLE _adm_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE COLLATE NOCASE,
  display_name TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  is_platform_admin INTEGER NOT NULL DEFAULT 0 CHECK (is_platform_admin IN (0,1)),
  disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0,1)),
  created_at TEXT NOT NULL,
  last_login_at TEXT
) STRICT;

CREATE TABLE _adm_sessions (
  id_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  ip TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX _adm_sessions_user ON _adm_sessions(user_id);

CREATE TABLE _adm_login_attempts (
  key TEXT PRIMARY KEY,
  count INTEGER NOT NULL,
  window_start TEXT NOT NULL
) STRICT;

CREATE TABLE _adm_workspaces (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  status TEXT NOT NULL CHECK (status IN ('active','suspended','restoring','needs_repair','deleted')),
  quota_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(quota_json)),
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL
) STRICT;

CREATE TABLE _adm_memberships (
  workspace_id TEXT NOT NULL REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('owner','builder','editor','viewer')),
  PRIMARY KEY (workspace_id, user_id)
) STRICT;
CREATE INDEX _adm_memberships_user ON _adm_memberships(user_id);

CREATE TABLE _adm_job_index (
  workspace_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  next_run_at TEXT,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  running_run_id TEXT,
  last_status TEXT,
  PRIMARY KEY (workspace_id, job_id)
) STRICT;
CREATE INDEX _adm_job_index_next ON _adm_job_index(next_run_at) WHERE enabled = 1;

CREATE TABLE _adm_backups (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('manual','scheduled','restore_point','pre_restore')),
  path TEXT NOT NULL,
  raw_bytes INTEGER NOT NULL,
  gz_bytes INTEGER NOT NULL,
  sha256_gz TEXT NOT NULL,
  created_at TEXT NOT NULL,
  created_by TEXT NOT NULL,
  manifest_json TEXT NOT NULL CHECK (json_valid(manifest_json))
) STRICT;
CREATE INDEX _adm_backups_ws ON _adm_backups(workspace_id, created_at DESC);

CREATE TABLE _adm_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;

CREATE TABLE _adm_audit (
  id INTEGER PRIMARY KEY,
  at TEXT NOT NULL,
  user_id TEXT,
  action TEXT NOT NULL,
  object_kind TEXT,
  object_id TEXT,
  details TEXT CHECK (details IS NULL OR json_valid(details))
) STRICT;

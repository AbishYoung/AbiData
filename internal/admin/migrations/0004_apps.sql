-- Public apps: forms and reports behind a secret link (ADR 0014). The token is
-- the only credential; lookups go by its SHA-256 so the index never compares
-- the secret itself. The token is kept so owners can copy the link again.
CREATE TABLE _adm_apps (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token TEXT NOT NULL,
  token_hash BLOB NOT NULL UNIQUE,
  start_item TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  taken_down_at TEXT,
  taken_down_by TEXT,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;
CREATE INDEX _adm_apps_ws ON _adm_apps(workspace_id, name);

-- The forms and reports an app shows, in menu order. fingerprint pins the
-- versions an owner approved (object, module and the queries it uses).
CREATE TABLE _adm_app_items (
  app_id TEXT NOT NULL REFERENCES _adm_apps(id) ON DELETE CASCADE,
  id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('form','report')),
  object_id TEXT NOT NULL,
  title TEXT NOT NULL,
  position INTEGER NOT NULL,
  fingerprint TEXT NOT NULL,
  PRIMARY KEY (app_id, id)
) STRICT;

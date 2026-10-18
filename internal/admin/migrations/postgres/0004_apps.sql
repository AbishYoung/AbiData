-- Public apps (ADR 0014); see the SQLite migration.
CREATE TABLE _adm_apps (
  id text COLLATE "C" PRIMARY KEY,
  workspace_id text COLLATE "C" NOT NULL REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  name text COLLATE "C" NOT NULL,
  token text COLLATE "C" NOT NULL,
  token_hash bytea NOT NULL UNIQUE,
  start_item text COLLATE "C" NOT NULL DEFAULT '',
  enabled bigint NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  taken_down_at text COLLATE "C",
  taken_down_by text COLLATE "C",
  created_by text COLLATE "C" NOT NULL,
  created_at text COLLATE "C" NOT NULL,
  updated_at text COLLATE "C" NOT NULL
);
CREATE INDEX _adm_apps_ws ON _adm_apps(workspace_id, name);

CREATE TABLE _adm_app_items (
  app_id text COLLATE "C" NOT NULL REFERENCES _adm_apps(id) ON DELETE CASCADE,
  id text COLLATE "C" NOT NULL,
  kind text COLLATE "C" NOT NULL CHECK (kind IN ('form','report')),
  object_id text COLLATE "C" NOT NULL,
  title text COLLATE "C" NOT NULL,
  position bigint NOT NULL,
  fingerprint text COLLATE "C" NOT NULL,
  PRIMARY KEY (app_id, id)
);

-- Workspace API keys (ADR 0009); see the SQLite migration.
CREATE TABLE _adm_api_keys (
  id text COLLATE "C" PRIMARY KEY,
  workspace_id text COLLATE "C" NOT NULL REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  name text COLLATE "C" NOT NULL,
  role text COLLATE "C" NOT NULL CHECK (role IN ('viewer','editor','builder')),
  secret_hash bytea NOT NULL,
  created_by text COLLATE "C" NOT NULL,
  created_at text COLLATE "C" NOT NULL,
  expires_at text COLLATE "C",
  last_used_at text COLLATE "C",
  revoked_at text COLLATE "C"
);
CREATE INDEX _adm_api_keys_ws ON _adm_api_keys(workspace_id, created_at DESC);

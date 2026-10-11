-- Workspace API keys for external clients (ADR 0008). The secret is never
-- stored: secret_hash is HMAC-SHA256(server session key, domain tag + secret).
-- Keys belong to the workspace, not to the user who created them, and can
-- never hold the owner role.
CREATE TABLE _adm_api_keys (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('viewer','editor','builder')),
  secret_hash BLOB NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT,
  last_used_at TEXT,
  revoked_at TEXT
) STRICT;
CREATE INDEX _adm_api_keys_ws ON _adm_api_keys(workspace_id, created_at DESC);

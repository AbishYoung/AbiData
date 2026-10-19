-- User blogs (ADR 0017). A system workspace (system_kind 'blog') belongs to
-- its user: it cannot be shared, transferred or deleted.
ALTER TABLE _adm_workspaces ADD COLUMN system_kind TEXT CHECK (system_kind IN ('blog'));

-- One blog per user. The home (title, intro, the published switch) lives here
-- so the public router finds it by username without opening the workspace.
-- slug is unused while the URL is /b/{username}; it keeps a per-blog slug
-- possible later without a migration.
CREATE TABLE _adm_blogs (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL UNIQUE REFERENCES _adm_users(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL UNIQUE REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  slug TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  intro TEXT NOT NULL DEFAULT '',
  published INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0,1)),
  published_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

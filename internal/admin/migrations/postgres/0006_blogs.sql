-- User blogs (ADR 0017); see the SQLite migration.
ALTER TABLE _adm_workspaces ADD COLUMN system_kind text COLLATE "C" CHECK (system_kind IN ('blog'));

CREATE TABLE _adm_blogs (
  id text COLLATE "C" PRIMARY KEY,
  user_id text COLLATE "C" NOT NULL UNIQUE REFERENCES _adm_users(id) ON DELETE CASCADE,
  workspace_id text COLLATE "C" NOT NULL UNIQUE REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  slug text COLLATE "C" NOT NULL DEFAULT '',
  title text COLLATE "C" NOT NULL DEFAULT '',
  intro text COLLATE "C" NOT NULL DEFAULT '',
  published bigint NOT NULL DEFAULT 0 CHECK (published IN (0,1)),
  published_at text COLLATE "C",
  created_at text COLLATE "C" NOT NULL,
  updated_at text COLLATE "C" NOT NULL
);

-- Usernames (ADR 0017); see the SQLite migration.
ALTER TABLE _adm_users ADD COLUMN username text COLLATE @lib.abi_ci;
ALTER TABLE _adm_users ADD COLUMN username_changed_at text COLLATE "C";
CREATE UNIQUE INDEX _adm_users_username ON _adm_users(username);

CREATE TABLE _adm_username_history (
  username text COLLATE @lib.abi_ci PRIMARY KEY,
  user_id text COLLATE "C" NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  changed_at text COLLATE "C" NOT NULL,
  expires_at text COLLATE "C" NOT NULL
);
CREATE INDEX _adm_username_history_user ON _adm_username_history(user_id);

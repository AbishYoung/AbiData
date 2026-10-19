-- Usernames (ADR 0017): a unique, URL-safe public name per user, stored lower
-- case. Added nullable: the server fills it for existing users at boot (the
-- derivation is Go, not SQL), and every new user gets one at creation.
ALTER TABLE _adm_users ADD COLUMN username TEXT COLLATE NOCASE;
ALTER TABLE _adm_users ADD COLUMN username_changed_at TEXT;
CREATE UNIQUE INDEX _adm_users_username ON _adm_users(username);

-- A changed username stays reserved for its user until expires_at, and
-- redirects to the new one meanwhile. Expired rows are ignored and replaced.
CREATE TABLE _adm_username_history (
  username TEXT PRIMARY KEY COLLATE NOCASE,
  user_id TEXT NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  changed_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
) STRICT;
CREATE INDEX _adm_username_history_user ON _adm_username_history(user_id);

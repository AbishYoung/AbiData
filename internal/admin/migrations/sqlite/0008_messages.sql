-- Internal messaging: user-to-user (direct) and workspace threads with replies.
CREATE TABLE _adm_message_threads (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('direct','workspace')),
  workspace_id TEXT REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  subject TEXT NOT NULL,
  created_by TEXT NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  last_message_at TEXT NOT NULL,
  message_count INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE INDEX _adm_message_threads_ws ON _adm_message_threads(workspace_id, last_message_at DESC);
CREATE INDEX _adm_message_threads_last ON _adm_message_threads(last_message_at DESC);

CREATE TABLE _adm_messages (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES _adm_message_threads(id) ON DELETE CASCADE,
  parent_id TEXT REFERENCES _adm_messages(id) ON DELETE SET NULL,
  sender_id TEXT NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  body TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;
CREATE INDEX _adm_messages_thread ON _adm_messages(thread_id, created_at, id);

CREATE TABLE _adm_message_participants (
  thread_id TEXT NOT NULL REFERENCES _adm_message_threads(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  last_read_at TEXT,
  archived_at TEXT,
  PRIMARY KEY (thread_id, user_id)
) STRICT;
CREATE INDEX _adm_message_participants_user ON _adm_message_participants(user_id, thread_id);

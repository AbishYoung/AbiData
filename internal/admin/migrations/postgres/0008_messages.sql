-- Internal messaging; see the SQLite migration.
CREATE TABLE _adm_message_threads (
  id text COLLATE "C" PRIMARY KEY,
  kind text COLLATE "C" NOT NULL CHECK (kind IN ('direct','workspace')),
  workspace_id text COLLATE "C" REFERENCES _adm_workspaces(id) ON DELETE CASCADE,
  subject text COLLATE "C" NOT NULL,
  created_by text COLLATE "C" NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  created_at text COLLATE "C" NOT NULL,
  last_message_at text COLLATE "C" NOT NULL,
  message_count bigint NOT NULL DEFAULT 1
);
CREATE INDEX _adm_message_threads_ws ON _adm_message_threads(workspace_id, last_message_at DESC);
CREATE INDEX _adm_message_threads_last ON _adm_message_threads(last_message_at DESC);

CREATE TABLE _adm_messages (
  id text COLLATE "C" PRIMARY KEY,
  thread_id text COLLATE "C" NOT NULL REFERENCES _adm_message_threads(id) ON DELETE CASCADE,
  parent_id text COLLATE "C" REFERENCES _adm_messages(id) ON DELETE SET NULL,
  sender_id text COLLATE "C" NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  body text COLLATE "C" NOT NULL,
  created_at text COLLATE "C" NOT NULL
);
CREATE INDEX _adm_messages_thread ON _adm_messages(thread_id, created_at, id);

CREATE TABLE _adm_message_participants (
  thread_id text COLLATE "C" NOT NULL REFERENCES _adm_message_threads(id) ON DELETE CASCADE,
  user_id text COLLATE "C" NOT NULL REFERENCES _adm_users(id) ON DELETE CASCADE,
  last_read_at text COLLATE "C",
  archived_at text COLLATE "C",
  PRIMARY KEY (thread_id, user_id)
);
CREATE INDEX _adm_message_participants_user ON _adm_message_participants(user_id, thread_id);

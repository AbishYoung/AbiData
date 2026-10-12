-- ADR 0011 file attachments: file metadata and references (the field type list is open since 0002).

-- One row per uploaded file. The bytes live in <data>/workspaces/<id>.files/aa/bb/<sha256>.
CREATE TABLE _sys_files (
  id TEXT PRIMARY KEY,
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  size INTEGER NOT NULL CHECK (size >= 0),
  name TEXT NOT NULL, content_type TEXT NOT NULL,
  width INTEGER, height INTEGER,
  created_at TEXT NOT NULL, created_by TEXT,
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','attached')),
  last_unreferenced_at TEXT       -- set when the last reference goes away; the janitor's grace period starts here
) STRICT;
CREATE INDEX _sys_files_sha ON _sys_files(sha256);
CREATE INDEX _sys_files_state ON _sys_files(state, created_at);

-- Which cell refers to which file. Kept by the _att_* triggers below and on each
-- table that has an attachments field, in the same transaction as the row write.
CREATE TABLE _sys_file_refs (
  file_id TEXT NOT NULL REFERENCES _sys_files(id),
  table_id TEXT NOT NULL, row_id INTEGER NOT NULL, column_id TEXT NOT NULL,
  PRIMARY KEY (file_id, table_id, row_id, column_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX _sys_file_refs_cell ON _sys_file_refs(table_id, row_id, column_id);

-- A first reference makes a file attached; losing the last one starts its grace period.
CREATE TRIGGER _att_refs_ins AFTER INSERT ON _sys_file_refs BEGIN
  UPDATE _sys_files SET state = 'attached', last_unreferenced_at = NULL
    WHERE id = NEW.file_id AND (state <> 'attached' OR last_unreferenced_at IS NOT NULL);
END;
CREATE TRIGGER _att_refs_del AFTER DELETE ON _sys_file_refs
  WHEN NOT EXISTS (SELECT 1 FROM _sys_file_refs WHERE file_id = OLD.file_id) BEGIN
  UPDATE _sys_files SET last_unreferenced_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = OLD.file_id;
END;

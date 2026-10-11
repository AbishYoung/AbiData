-- ADR 0011 file attachments: the `attachments` field type, file metadata and references.

-- field_type's CHECK lists the types, so widening it means rebuilding _sys_columns
-- (nothing references this table, so no foreign key is affected).
CREATE TABLE _sys_columns_new (
  id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name TEXT NOT NULL COLLATE NOCASE, display_name TEXT NOT NULL,
  field_type TEXT NOT NULL CHECK (field_type IN ('autonumber','short_text','long_text','integer','number',
    'currency','yes_no','date','datetime','lookup','hyperlink','calculated','attachments')),
  type_options TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(type_options)),
  is_required INTEGER NOT NULL DEFAULT 0 CHECK (is_required IN (0,1)),
  default_sql TEXT, validation_sql TEXT, validation_text TEXT,
  lookup_relationship_id TEXT REFERENCES _sys_relationships(id) ON DELETE SET NULL,
  lookup_display_column_id TEXT,
  description TEXT NOT NULL DEFAULT '', ordinal INTEGER NOT NULL,
  is_hidden INTEGER NOT NULL DEFAULT 0 CHECK (is_hidden IN (0,1)),
  UNIQUE (table_id, physical_name)
) STRICT;
INSERT INTO _sys_columns_new SELECT id, table_id, physical_name, display_name, field_type, type_options, is_required,
  default_sql, validation_sql, validation_text, lookup_relationship_id, lookup_display_column_id, description, ordinal, is_hidden
  FROM _sys_columns;
DROP TABLE _sys_columns;
ALTER TABLE _sys_columns_new RENAME TO _sys_columns;

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

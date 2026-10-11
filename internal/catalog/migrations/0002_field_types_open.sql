-- ADR 0010 adds the field types geo_point and geo_area. _sys_columns carried a
-- closed list of field types in a CHECK, so every new type would need another
-- table rebuild. The list is enforced in Go (FieldType.Valid) before any
-- metadata is written, so the CHECK becomes "not empty" and later field types
-- need no migration.
CREATE TABLE _sys_columns_new (
  id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name TEXT NOT NULL COLLATE NOCASE, display_name TEXT NOT NULL,
  field_type TEXT NOT NULL CHECK (length(field_type) > 0),
  type_options TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(type_options)), -- {"max_length":255} {"expression":"..."} {"format":"..."}
  is_required INTEGER NOT NULL DEFAULT 0 CHECK (is_required IN (0,1)),
  default_sql TEXT, validation_sql TEXT, validation_text TEXT,
  lookup_relationship_id TEXT REFERENCES _sys_relationships(id) ON DELETE SET NULL,
  lookup_display_column_id TEXT,
  description TEXT NOT NULL DEFAULT '', ordinal INTEGER NOT NULL,
  is_hidden INTEGER NOT NULL DEFAULT 0 CHECK (is_hidden IN (0,1)),
  UNIQUE (table_id, physical_name)
) STRICT;

INSERT INTO _sys_columns_new (id, table_id, physical_name, display_name, field_type, type_options, is_required,
  default_sql, validation_sql, validation_text, lookup_relationship_id, lookup_display_column_id, description, ordinal, is_hidden)
SELECT id, table_id, physical_name, display_name, field_type, type_options, is_required,
  default_sql, validation_sql, validation_text, lookup_relationship_id, lookup_display_column_id, description, ordinal, is_hidden
FROM _sys_columns;

DROP TABLE _sys_columns;
ALTER TABLE _sys_columns_new RENAME TO _sys_columns;

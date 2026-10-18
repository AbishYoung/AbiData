-- ADR 0011 file attachments: file metadata and references.

-- One row per uploaded file. The bytes live in <data>/workspaces/<id>.files/aa/bb/<sha256>.
CREATE TABLE _sys_files (
  id text COLLATE "C" PRIMARY KEY,
  sha256 text COLLATE "C" NOT NULL CHECK (length(sha256) = 64),
  size bigint NOT NULL CHECK (size >= 0),
  name text COLLATE "C" NOT NULL, content_type text COLLATE "C" NOT NULL,
  width bigint, height bigint,
  created_at text COLLATE "C" NOT NULL, created_by text COLLATE "C",
  state text COLLATE "C" NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','attached')),
  last_unreferenced_at text COLLATE "C"       -- set when the last reference goes away; the janitor's grace period starts here
);
CREATE INDEX _sys_files_sha ON _sys_files(sha256);
CREATE INDEX _sys_files_state ON _sys_files(state, created_at);

-- Which cell refers to which file, kept in the same transaction as the row write.
CREATE TABLE _sys_file_refs (
  file_id text COLLATE "C" NOT NULL REFERENCES _sys_files(id),
  table_id text COLLATE "C" NOT NULL, row_id bigint NOT NULL, column_id text COLLATE "C" NOT NULL,
  PRIMARY KEY (file_id, table_id, row_id, column_id)
);
CREATE INDEX _sys_file_refs_cell ON _sys_file_refs(table_id, row_id, column_id);

-- A first reference makes a file attached; losing the last one starts its
-- grace period. The functions run with the schema they were created in.
CREATE FUNCTION _att_refs_ins() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
BEGIN
  UPDATE _sys_files SET state = 'attached', last_unreferenced_at = NULL
    WHERE id = NEW.file_id AND (state <> 'attached' OR last_unreferenced_at IS NOT NULL);
  RETURN NULL;
END $$;
CREATE TRIGGER _att_refs_ins AFTER INSERT ON _sys_file_refs FOR EACH ROW EXECUTE FUNCTION _att_refs_ins();

CREATE FUNCTION _att_refs_del() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM _sys_file_refs WHERE file_id = OLD.file_id) THEN
    UPDATE _sys_files SET last_unreferenced_at = to_char(statement_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
      WHERE id = OLD.file_id;
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER _att_refs_del AFTER DELETE ON _sys_file_refs FOR EACH ROW EXECUTE FUNCTION _att_refs_del();

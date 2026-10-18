-- ADR 0010: the field type list is enforced in Go (FieldType.Valid), so the
-- CHECK becomes "not empty". SQLite rebuilds the table; PostgreSQL swaps the
-- constraint.
ALTER TABLE _sys_columns DROP CONSTRAINT _sys_columns_field_type_check,
  ADD CONSTRAINT _sys_columns_field_type_check CHECK (length(field_type) > 0);

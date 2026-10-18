package catalog

import (
	"fmt"
	"strings"

	"abidata/internal/sqlgen"
)

// Constraint names: "ty_<colid>" for type-intrinsic CHECKs, "ck_<colid>" for
// builder validation rules (mapped to validation_text on failure).
func typeConstraintName(colID string) string       { return "ty_" + strings.ToLower(colID) }
func validationConstraintName(colID string) string { return "ck_" + strings.ToLower(colID) }

// ConstraintColumn maps a constraint name from a CHECK failure back to the column
// ID and whether it was the builder's validation rule (vs. a type check).
func ConstraintColumn(name string) (colID string, validation bool, ok bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "ck_"):
		return strings.ToUpper(name[3:]), true, true
	case strings.HasPrefix(lower, "ty_"):
		return strings.ToUpper(name[3:]), false, true
	}
	return "", false, false
}

// calcType maps a calculated column's result_type to the logical type its
// generated column is declared with. Text and dates stay untyped (ANY), as
// the expression's result is not type-checked yet (ADR 0013 Phase D).
func calcType(rt string) sqlgen.Type {
	switch rt {
	case "integer":
		return sqlgen.Integer
	case "currency":
		return sqlgen.Currency
	case "yes_no":
		return sqlgen.Boolean
	case "number":
		return sqlgen.Number
	}
	return sqlgen.Unknown
}

// fieldType is the logical type a catalog field type is stored as.
func fieldType(t FieldType) sqlgen.Type {
	switch t {
	case ShortText, LongText, Hyperlink:
		return sqlgen.Text
	case Integer, Lookup:
		return sqlgen.Integer
	case Currency:
		return sqlgen.Currency
	case Number:
		return sqlgen.Number
	case YesNo:
		return sqlgen.Boolean
	case Date:
		return sqlgen.Date
	case DateTime:
		return sqlgen.DateTime
	case Attachments:
		return sqlgen.Attachments
	case GeoPoint:
		return sqlgen.GeoPoint
	case GeoArea:
		return sqlgen.GeoArea
	}
	return sqlgen.Unknown
}

// columnDef declares one column of a table with columns cols (without foreign
// keys, which are table-level constraints generated from relationships).
// Builder expressions are embedded from their parsed form (see exprs.go).
func columnDef(cols []Column, c Column) sqlgen.ColumnDef {
	d := sqlgen.ColumnDef{Name: c.PhysicalName}
	switch c.Type {
	case Autonumber:
		d.AutoIncrement = true
		return d
	case Calculated:
		d.Type = calcType(c.Options.ResultType)
		d.Generated = exprNode(cols, ExprCalculated, c.Options.Expression)
		return d
	}
	d.Type = fieldType(c.Type)
	d.TypeCheck = typeConstraintName(c.ID)
	switch c.Type {
	case ShortText:
		d.MaxLen = c.MaxLen()
	case Hyperlink:
		d.URL = true
	}
	d.NotNull = c.Required || c.Type == YesNo
	switch {
	case c.DefaultSQL != "":
		d.Default = exprNode(cols, ExprDefault, c.DefaultSQL)
	case c.Type == YesNo:
		d.Default = sqlgen.Lit{Value: 0}
	}
	if c.ValidationSQL != "" {
		d.Checks = []sqlgen.Check{{Name: validationConstraintName(c.ID), Expr: exprNode(cols, ExprValidation, c.ValidationSQL)}}
	}
	return d
}

// versionColumn is the optimistic-concurrency row version every table has.
var versionColumn = sqlgen.ColumnDef{Name: "_version", Type: sqlgen.Integer, NotNull: true, Default: sqlgen.Lit{Value: 1}}

// render renders a schema statement for the workspace's dialect.
func render(s sqlgen.Stmt) (string, error) {
	r, err := sqlgen.Render(sqlgen.SQLite, s)
	if err != nil {
		return "", fmt.Errorf("catalog: %w", err)
	}
	return r.SQL, nil
}

// createTableSQL renders CREATE TABLE for t under physical name `as`, with
// foreign keys for every enforced relationship whose child is t.
func createTableSQL(s *Schema, t *Table, as string) (string, error) {
	ct := &sqlgen.CreateTable{Name: as, Strict: true}
	for _, c := range t.Columns {
		ct.Columns = append(ct.Columns, columnDef(t.Columns, c))
		if c.Type == Autonumber {
			ct.Columns = append(ct.Columns, versionColumn)
		}
	}
	for _, r := range s.Relationships {
		if r.ChildTableID != t.ID || !r.Enforce {
			continue
		}
		col, ok := t.Column(r.ChildColumnID)
		if !ok {
			return "", fmt.Errorf("catalog: relationship %s child column %s missing", r.ID, r.ChildColumnID)
		}
		parent, ok := s.Table(r.ParentTableID)
		if !ok {
			return "", fmt.Errorf("catalog: relationship %s parent table %s missing", r.ID, r.ParentTableID)
		}
		parentName := parent.PhysicalName
		if parent.ID == t.ID {
			parentName = as // self-reference follows the new name during rebuild
		}
		ct.ForeignKeys = append(ct.ForeignKeys, sqlgen.ForeignKey{Name: "fk_" + strings.ToLower(r.ID),
			Column: col.PhysicalName, RefTable: parentName, OnDelete: sqlgen.RefAction(r.OnDelete)})
	}
	return render(ct)
}

func createIndexSQL(t *Table, x Index) (string, error) {
	ci := &sqlgen.CreateIndex{Name: x.PhysicalName, Table: t.PhysicalName, Unique: x.Unique}
	for _, id := range x.ColumnIDs {
		c, ok := t.Column(id)
		if !ok {
			return "", fmt.Errorf("catalog: index %s column %s missing", x.ID, id)
		}
		// Text keys use NOCASE: the datasheet sorts text case-insensitively (as
		// Access does), and only an index with the same collation can serve it.
		// Unique text indexes are therefore case-insensitive too.
		ci.Columns = append(ci.Columns, sqlgen.Col{Name: c.PhysicalName, CI: isTextType(c)})
	}
	return render(ci)
}

func occTriggerName(t *Table) string { return "_occ_" + strings.ToLower(t.ID) }

// occTrigger is the safety-net trigger: any UPDATE that left _version
// unchanged bumps it (recursive_triggers stays OFF, so it does not re-fire).
func occTrigger(t *Table) sqlgen.Stmt {
	return &sqlgen.OCCBump{Name: occTriggerName(t), Table: t.PhysicalName}
}

// isTextType reports whether c holds text that sorts case-insensitively.
func isTextType(c *Column) bool {
	switch c.Type {
	case ShortText, LongText, Hyperlink:
		return true
	case Calculated:
		return c.Options.ResultType == "" || c.Options.ResultType == "text"
	}
	return false
}

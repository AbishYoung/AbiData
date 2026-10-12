package catalog

import (
	"fmt"
	"strings"
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

// calcSQLType maps a calculated column's result_type to a STRICT column type.
func calcSQLType(rt string) string {
	switch rt {
	case "integer", "currency", "yes_no":
		return "INTEGER"
	case "number":
		return "REAL"
	case "text", "date", "datetime", "":
		return "ANY"
	}
	return "ANY"
}

// columnDef renders one column definition (without foreign keys, which are
// table-level constraints generated from relationships).
func columnDef(c Column) string {
	q := Quote(c.PhysicalName)
	var b strings.Builder
	b.WriteString(q)
	b.WriteByte(' ')
	if c.Type == Autonumber {
		b.WriteString("INTEGER PRIMARY KEY AUTOINCREMENT")
		return b.String()
	}
	if c.Type == Calculated {
		fmt.Fprintf(&b, "%s GENERATED ALWAYS AS (%s) VIRTUAL", calcSQLType(c.Options.ResultType), c.Options.Expression)
		return b.String()
	}
	tyCheck := ""
	switch c.Type {
	case ShortText:
		b.WriteString("TEXT")
		tyCheck = fmt.Sprintf("length(%s) <= %d", q, c.MaxLen())
	case LongText:
		b.WriteString("TEXT")
	case Integer, Currency, Lookup:
		b.WriteString("INTEGER")
	case Number:
		b.WriteString("REAL")
	case YesNo:
		b.WriteString("INTEGER")
		tyCheck = fmt.Sprintf("%s IN (0,1)", q)
	case Date:
		b.WriteString("TEXT")
		tyCheck = fmt.Sprintf("%s IS NULL OR date(%s) IS %s", q, q, q)
	case DateTime:
		b.WriteString("TEXT")
		tyCheck = fmt.Sprintf("%s IS NULL OR strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', %s) IS %s", q, q, q)
	case Hyperlink:
		b.WriteString("TEXT")
		tyCheck = fmt.Sprintf("%s IS NULL OR %s GLOB 'http*://*'", q, q)
	case Attachments:
		b.WriteString("TEXT")
		tyCheck = fmt.Sprintf("%s IS NULL OR (json_valid(%s) AND json_type(%s) = 'array')", q, q, q)
	case GeoPoint, GeoArea:
		b.WriteString("TEXT")
		tyCheck = geoTypeCheck(q, c.Type)
	default:
		b.WriteString("ANY")
	}
	if c.Required || c.Type == YesNo {
		b.WriteString(" NOT NULL")
	}
	switch {
	case c.DefaultSQL != "":
		fmt.Fprintf(&b, " DEFAULT (%s)", c.DefaultSQL)
	case c.Type == YesNo:
		b.WriteString(" DEFAULT 0")
	}
	if tyCheck != "" {
		fmt.Fprintf(&b, " CONSTRAINT %s CHECK (%s)", Quote(typeConstraintName(c.ID)), tyCheck)
	}
	if c.ValidationSQL != "" {
		fmt.Fprintf(&b, " CONSTRAINT %s CHECK (%s)", Quote(validationConstraintName(c.ID)), c.ValidationSQL)
	}
	return b.String()
}

// createTableSQL renders CREATE TABLE for t under physical name `as`, with
// foreign keys for every enforced relationship whose child is t.
func createTableSQL(s *Schema, t *Table, as string) (string, error) {
	var parts []string
	for _, c := range t.Columns {
		parts = append(parts, columnDef(c))
		if c.Type == Autonumber {
			parts = append(parts, `"_version" INTEGER NOT NULL DEFAULT 1`)
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
		parts = append(parts, fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s(\"id\") ON DELETE %s",
			Quote("fk_"+strings.ToLower(r.ID)), Quote(col.PhysicalName), Quote(parentName), r.OnDelete))
	}
	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n) STRICT", Quote(as), strings.Join(parts, ",\n  ")), nil
}

func createIndexSQL(t *Table, x Index) (string, error) {
	cols := make([]string, 0, len(x.ColumnIDs))
	for _, id := range x.ColumnIDs {
		c, ok := t.Column(id)
		if !ok {
			return "", fmt.Errorf("catalog: index %s column %s missing", x.ID, id)
		}
		// Text keys use NOCASE: the datasheet sorts text case-insensitively (as
		// Access does), and only an index with the same collation can serve it.
		// Unique text indexes are therefore case-insensitive too.
		if isTextType(c) {
			cols = append(cols, Quote(c.PhysicalName)+" COLLATE NOCASE")
		} else {
			cols = append(cols, Quote(c.PhysicalName))
		}
	}
	u := ""
	if x.Unique {
		u = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", u, Quote(x.PhysicalName), Quote(t.PhysicalName), strings.Join(cols, ", ")), nil
}

func occTriggerName(t *Table) string { return "_occ_" + strings.ToLower(t.ID) }

// occTriggerSQL is the safety-net trigger: any UPDATE that left _version
// unchanged bumps it (recursive_triggers stays OFF, so it does not re-fire).
func occTriggerSQL(t *Table) string {
	q := Quote(t.PhysicalName)
	return fmt.Sprintf(`CREATE TRIGGER %s AFTER UPDATE ON %s FOR EACH ROW WHEN NEW."_version" = OLD."_version" `+
		`BEGIN UPDATE %s SET "_version" = OLD."_version" + 1 WHERE "id" = NEW."id"; END`,
		Quote(occTriggerName(t)), q, q)
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

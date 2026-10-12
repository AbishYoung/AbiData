package catalog

import (
	"fmt"
	"strings"
)

// ADR 0011: the `attachments` field type. A cell holds a JSON array of file
// ids (TEXT). The files themselves are described in _sys_files; which cell
// refers to which file is kept in _sys_file_refs by the triggers built here,
// so every write path (row API, batch, imports, scripts, cascades) keeps the
// references consistent in the same transaction.

// Attachments is the attachments field type.
const Attachments FieldType = "attachments"

func init() { FieldTypes = append(FieldTypes, Attachments) }

// Per-cell file count limits (type option max_files).
const (
	DefaultMaxFiles = 20
	MaxMaxFiles     = 100
)

// MaxFilesLimit returns the effective max_files of an attachments column.
func (c Column) MaxFilesLimit() int {
	if c.Options.MaxFiles > 0 {
		return c.Options.MaxFiles
	}
	return DefaultMaxFiles
}

// attachmentColumns lists t's attachments columns.
func attachmentColumns(t *Table) []Column {
	var out []Column
	for _, c := range t.Columns {
		if c.Type == Attachments {
			out = append(out, c)
		}
	}
	return out
}

func attachTriggerName(t *Table, kind string) string {
	return "_att_" + strings.ToLower(t.ID) + "_" + kind
}

// attachTriggerNames lists the reference triggers a table with attachments has.
func attachTriggerNames(t *Table) []string {
	return []string{attachTriggerName(t, "i"), attachTriggerName(t, "u"), attachTriggerName(t, "d")}
}

// HasAttachTriggers reports whether t should carry reference triggers.
func HasAttachTriggers(t *Table) bool { return len(attachmentColumns(t)) > 0 }

// attachTriggersSQL renders the statements that (re)create t's reference
// triggers: a DROP IF EXISTS for each, then the CREATEs when t has attachments
// columns. They refer to the columns by physical name, so they are rebuilt
// whenever the table is rebuilt or a column is added or renamed.
func attachTriggersSQL(t *Table) []string {
	var out []string
	for _, n := range attachTriggerNames(t) {
		out = append(out, "DROP TRIGGER IF EXISTS "+Quote(n))
	}
	cols := attachmentColumns(t)
	if len(cols) == 0 {
		return out
	}
	tq := Quote(t.PhysicalName)
	insertRefs := func(c Column) string {
		return fmt.Sprintf(`INSERT OR IGNORE INTO _sys_file_refs(file_id, table_id, row_id, column_id) `+
			`SELECT value, '%s', NEW."id", '%s' FROM json_each(NEW.%s) WHERE type = 'text';`, t.ID, c.ID, Quote(c.PhysicalName))
	}
	var ins, upd strings.Builder
	var names []string
	for _, c := range cols {
		ins.WriteString(insertRefs(c) + " ")
		fmt.Fprintf(&upd, `DELETE FROM _sys_file_refs WHERE table_id = '%s' AND row_id = OLD."id" AND column_id = '%s' `+
			`AND file_id NOT IN (SELECT value FROM json_each(NEW.%s) WHERE type = 'text'); `, t.ID, c.ID, Quote(c.PhysicalName))
		upd.WriteString(insertRefs(c) + " ")
		names = append(names, Quote(c.PhysicalName))
	}
	out = append(out,
		fmt.Sprintf(`CREATE TRIGGER %s AFTER INSERT ON %s FOR EACH ROW BEGIN %s END`, Quote(attachTriggerName(t, "i")), tq, ins.String()),
		fmt.Sprintf(`CREATE TRIGGER %s AFTER UPDATE OF %s ON %s FOR EACH ROW BEGIN %s END`,
			Quote(attachTriggerName(t, "u")), strings.Join(names, ", "), tq, upd.String()),
		fmt.Sprintf(`CREATE TRIGGER %s AFTER DELETE ON %s FOR EACH ROW BEGIN `+
			`DELETE FROM _sys_file_refs WHERE table_id = '%s' AND row_id = OLD."id"; END`, Quote(attachTriggerName(t, "d")), tq, t.ID),
	)
	return out
}

// syncAttachTriggers recreates t's reference triggers to match its columns.
func (p *planner) syncAttachTriggers(t *Table) error {
	for _, q := range attachTriggersSQL(t) {
		if err := p.exec(q); err != nil {
			return err
		}
	}
	return nil
}

// checkAttachmentsSpec validates an attachments column definition.
func checkAttachmentsSpec(c *Column, existing *Column) error {
	if existing != nil && (existing.Type == Attachments) != (c.Type == Attachments) {
		return badf("Attachment fields cannot be converted to or from other types. Add a new field instead.")
	}
	if c.Type != Attachments {
		c.Options.MaxFiles = 0
		return nil
	}
	if c.Options.MaxFiles < 0 || c.Options.MaxFiles > MaxMaxFiles {
		return badf("The number of files per cell must be between 1 and %d.", MaxMaxFiles)
	}
	if c.Options.MaxFiles == 0 {
		c.Options.MaxFiles = DefaultMaxFiles
	}
	if c.DefaultSQL != "" {
		return badf("An attachments field cannot have a default value.")
	}
	return nil
}

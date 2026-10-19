package catalog

import (
	"strings"

	"abidata/internal/sqlgen"
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

// AttachTriggers returns the statements that (re)create t's reference
// triggers: a DROP IF EXISTS for each, then the CREATEs when t has attachments
// columns. They refer to the columns by physical name, so they are rebuilt
// whenever the table is rebuilt or a column is added or renamed.
func AttachTriggers(t *Table) []sqlgen.Stmt {
	var out []sqlgen.Stmt
	for _, n := range attachTriggerNames(t) {
		out = append(out, &sqlgen.DropTrigger{Name: n, Table: t.PhysicalName, IfExists: true})
	}
	var cols []sqlgen.AttachmentColumn
	for _, c := range attachmentColumns(t) {
		cols = append(cols, sqlgen.AttachmentColumn{ID: c.ID, Name: c.PhysicalName})
	}
	if len(cols) == 0 {
		return out
	}
	for _, ev := range []struct {
		suffix string
		event  sqlgen.TriggerEvent
	}{{"i", sqlgen.OnInsert}, {"u", sqlgen.OnUpdate}, {"d", sqlgen.OnDelete}} {
		out = append(out, &sqlgen.AttachmentRefs{Name: attachTriggerName(t, ev.suffix), Event: ev.event,
			Table: t.PhysicalName, TableID: t.ID, Columns: cols})
	}
	return out
}

// syncAttachTriggers recreates t's reference triggers to match its columns.
func (p *planner) syncAttachTriggers(t *Table) error {
	for _, s := range AttachTriggers(t) {
		if err := p.execStmt(s); err != nil {
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

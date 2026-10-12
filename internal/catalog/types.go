package catalog

import (
	"encoding/json"
	"strings"
)

// FieldType is an Access-style field type (brief <catalog_schema> mapping table).
type FieldType string

const (
	Autonumber FieldType = "autonumber"
	ShortText  FieldType = "short_text"
	LongText   FieldType = "long_text"
	Integer    FieldType = "integer"
	Number     FieldType = "number"
	Currency   FieldType = "currency"
	YesNo      FieldType = "yes_no"
	Date       FieldType = "date"
	DateTime   FieldType = "datetime"
	Lookup     FieldType = "lookup"
	Hyperlink  FieldType = "hyperlink"
	Calculated FieldType = "calculated"
	GeoPoint   FieldType = "geo_point" // ADR 0010: GeoJSON Point in TEXT
	GeoArea    FieldType = "geo_area"  // ADR 0010: GeoJSON Polygon or MultiPolygon in TEXT
)

// FieldTypes lists every type in display order.
var FieldTypes = []FieldType{Autonumber, ShortText, LongText, Integer, Number, Currency, YesNo, Date, DateTime, Lookup, Hyperlink, Calculated, GeoPoint, GeoArea}

// Valid reports whether t is a known field type.
func (t FieldType) Valid() bool {
	for _, x := range FieldTypes {
		if x == t {
			return true
		}
	}
	return false
}

// DefaultMaxLength is short_text's default max_length.
const DefaultMaxLength = 255

// CurrencyScale is the integer scale of currency values (×10⁴).
const CurrencyScale = 10000

// TypeOptions are per-type settings stored as JSON in _sys_columns.type_options.
type TypeOptions struct {
	MaxLength  int    `json:"max_length,omitempty"`  // short_text
	Expression string `json:"expression,omitempty"`  // calculated: SQL expression over this table's physical columns
	ResultType string `json:"result_type,omitempty"` // calculated: "text" | "integer" | "number" | "currency" | "yes_no" | "date" | "datetime" | "geo_point" | "geo_area" (default "text")
	Format     string `json:"format,omitempty"`      // display format hint (UI only)
	MaxFiles   int    `json:"max_files,omitempty"`   // attachments: files per cell (default 20, at most 100)
}

// Table is a user table with its columns (ordered by Ordinal) and indexes.
type Table struct {
	ID           string   `json:"id"`
	PhysicalName string   `json:"physical_name"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	TrackHistory bool     `json:"track_history"`
	Version      int64    `json:"version"`
	Columns      []Column `json:"columns"`
	Indexes      []Index  `json:"indexes"`
}

// Column is one field. The system column _version is implicit and never listed.
type Column struct {
	ID                    string      `json:"id"`
	TableID               string      `json:"table_id"`
	PhysicalName          string      `json:"physical_name"`
	DisplayName           string      `json:"display_name"`
	Type                  FieldType   `json:"field_type"`
	Options               TypeOptions `json:"type_options"`
	Required              bool        `json:"is_required"`
	DefaultSQL            string      `json:"default_sql,omitempty"`
	ValidationSQL         string      `json:"validation_sql,omitempty"`
	ValidationText        string      `json:"validation_text,omitempty"`
	LookupRelationshipID  string      `json:"lookup_relationship_id,omitempty"`
	LookupDisplayColumnID string      `json:"lookup_display_column_id,omitempty"`
	Description           string      `json:"description"`
	Ordinal               int         `json:"ordinal"`
	Hidden                bool        `json:"is_hidden"`
}

// MaxLen returns the effective short_text limit.
func (c Column) MaxLen() int {
	if c.Options.MaxLength > 0 {
		return c.Options.MaxLength
	}
	return DefaultMaxLength
}

// Index is a secondary index over one or more columns.
type Index struct {
	ID           string   `json:"id"`
	TableID      string   `json:"table_id"`
	PhysicalName string   `json:"physical_name"`
	Unique       bool     `json:"is_unique"`
	ColumnIDs    []string `json:"column_ids"`
}

// OnDelete is a referential action.
type OnDelete string

const (
	Restrict OnDelete = "RESTRICT"
	Cascade  OnDelete = "CASCADE"
	SetNull  OnDelete = "SET NULL"
)

// Relationship links child.column → parent.id.
type Relationship struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ParentTableID string   `json:"parent_table_id"`
	ChildTableID  string   `json:"child_table_id"`
	ChildColumnID string   `json:"child_column_id"`
	OnDelete      OnDelete `json:"on_delete"`
	Enforce       bool     `json:"enforce"`
}

// Schema is an in-memory snapshot of the catalog at SchemaVersion.
type Schema struct {
	Version       int64          `json:"schema_version"`
	Tables        []Table        `json:"tables"`
	Relationships []Relationship `json:"relationships"`

	tablesByID map[string]*Table
	colsByID   map[string]*Column
	relsByID   map[string]*Relationship
}

func (s *Schema) index() {
	s.tablesByID = map[string]*Table{}
	s.colsByID = map[string]*Column{}
	s.relsByID = map[string]*Relationship{}
	for i := range s.Tables {
		t := &s.Tables[i]
		s.tablesByID[t.ID] = t
		for j := range t.Columns {
			s.colsByID[t.Columns[j].ID] = &t.Columns[j]
		}
	}
	for i := range s.Relationships {
		s.relsByID[s.Relationships[i].ID] = &s.Relationships[i]
	}
}

// Table returns the table with id.
func (s *Schema) Table(id string) (*Table, bool) { t, ok := s.tablesByID[id]; return t, ok }

// Column returns the column with id.
func (s *Schema) Column(id string) (*Column, bool) { c, ok := s.colsByID[id]; return c, ok }

// Relationship returns the relationship with id.
func (s *Schema) Relationship(id string) (*Relationship, bool) { r, ok := s.relsByID[id]; return r, ok }

// TableByPhysical finds a table by physical name (case-insensitive).
func (s *Schema) TableByPhysical(name string) (*Table, bool) {
	for i := range s.Tables {
		if strings.EqualFold(s.Tables[i].PhysicalName, name) {
			return &s.Tables[i], true
		}
	}
	return nil, false
}

// ColumnByPhysical finds a column of t by physical name (case-insensitive).
func (t *Table) ColumnByPhysical(name string) (*Column, bool) {
	for i := range t.Columns {
		if strings.EqualFold(t.Columns[i].PhysicalName, name) {
			return &t.Columns[i], true
		}
	}
	return nil, false
}

// Column returns t's column with id.
func (t *Table) Column(id string) (*Column, bool) {
	for i := range t.Columns {
		if t.Columns[i].ID == id {
			return &t.Columns[i], true
		}
	}
	return nil, false
}

// ---- schema change ops -----------------------------------------------------

// OpKind names a schema change operation.
type OpKind string

const (
	OpCreateTable        OpKind = "create_table"
	OpRenameTable        OpKind = "rename_table"
	OpDropTable          OpKind = "drop_table"
	OpAddColumn          OpKind = "add_column"
	OpRenameColumn       OpKind = "rename_column"
	OpAlterColumn        OpKind = "alter_column"
	OpDropColumn         OpKind = "drop_column"
	OpReorderColumns     OpKind = "reorder_columns"
	OpCreateIndex        OpKind = "create_index"
	OpDropIndex          OpKind = "drop_index"
	OpCreateRelationship OpKind = "create_relationship"
	OpDropRelationship   OpKind = "drop_relationship"
	OpSetTableProps      OpKind = "set_table_props"
)

// ColumnSpec describes a column to add or the target state of an altered column.
// IDs may be supplied by the client (canonical ULIDs not already in use) so that a
// preview and its apply describe identical objects; otherwise the server assigns one.
type ColumnSpec struct {
	ID             string      `json:"id,omitempty"`
	DisplayName    string      `json:"display_name"`
	PhysicalName   string      `json:"physical_name,omitempty"` // derived from DisplayName when empty
	Type           FieldType   `json:"field_type"`
	Options        TypeOptions `json:"type_options,omitempty"`
	Required       bool        `json:"is_required,omitempty"`
	DefaultSQL     string      `json:"default_sql,omitempty"`
	ValidationSQL  string      `json:"validation_sql,omitempty"`
	ValidationText string      `json:"validation_text,omitempty"`
	Lookup         *LookupSpec `json:"lookup,omitempty"` // required when Type == lookup
	Description    string      `json:"description,omitempty"`
	Hidden         bool        `json:"is_hidden,omitempty"`
}

// LookupSpec configures a lookup column's relationship.
type LookupSpec struct {
	RelationshipID   string   `json:"relationship_id,omitempty"`
	ParentTableID    string   `json:"parent_table_id"`
	DisplayColumnID  string   `json:"display_column_id,omitempty"`
	OnDelete         OnDelete `json:"on_delete,omitempty"` // default RESTRICT
	Enforce          *bool    `json:"enforce,omitempty"`   // default true
	RelationshipName string   `json:"relationship_name,omitempty"`
}

// TableSpec describes a table to create.
type TableSpec struct {
	ID           string       `json:"id,omitempty"`
	DisplayName  string       `json:"display_name"`
	PhysicalName string       `json:"physical_name,omitempty"`
	Description  string       `json:"description,omitempty"`
	TrackHistory bool         `json:"track_history,omitempty"`
	IDColumnID   string       `json:"id_column_id,omitempty"` // ID for the autonumber "id" column
	Columns      []ColumnSpec `json:"columns,omitempty"`
}

// IndexSpec describes an index to create.
type IndexSpec struct {
	ID           string   `json:"id,omitempty"`
	TableID      string   `json:"table_id"`
	PhysicalName string   `json:"physical_name,omitempty"`
	Unique       bool     `json:"is_unique,omitempty"`
	ColumnIDs    []string `json:"column_ids"`
}

// RelationshipSpec describes a non-lookup relationship (child column must be integer).
type RelationshipSpec struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	ParentTableID string   `json:"parent_table_id"`
	ChildTableID  string   `json:"child_table_id"`
	ChildColumnID string   `json:"child_column_id"`
	OnDelete      OnDelete `json:"on_delete,omitempty"`
	Enforce       *bool    `json:"enforce,omitempty"`
}

// TableProps are metadata-only table settings.
type TableProps struct {
	DisplayName  *string `json:"display_name,omitempty"`
	Description  *string `json:"description,omitempty"`
	TrackHistory *bool   `json:"track_history,omitempty"`
}

// Op is one schema change. Exactly the fields relevant to Op.Kind are set.
type Op struct {
	Kind           OpKind            `json:"op"`
	TableID        string            `json:"table_id,omitempty"`
	ColumnID       string            `json:"column_id,omitempty"`
	IndexID        string            `json:"index_id,omitempty"`
	RelationshipID string            `json:"relationship_id,omitempty"`
	Table          *TableSpec        `json:"table,omitempty"`        // create_table
	Column         *ColumnSpec       `json:"column,omitempty"`       // add_column, alter_column (target state)
	DisplayName    string            `json:"display_name,omitempty"` // rename_*
	PhysicalName   string            `json:"physical_name,omitempty"`
	ColumnOrder    []string          `json:"column_order,omitempty"` // reorder_columns
	Index          *IndexSpec        `json:"index,omitempty"`
	Relationship   *RelationshipSpec `json:"relationship,omitempty"`
	Props          *TableProps       `json:"props,omitempty"`
}

// Destructive reports whether the op can lose data (needs a restore point).
func (o Op) Destructive() bool {
	switch o.Kind {
	case OpDropTable, OpDropColumn:
		return true
	case OpAlterColumn:
		return true // type/constraint changes may convert or reject data; treated conservatively
	}
	return false
}

// ---- impact report ---------------------------------------------------------

// Issue is one finding in an impact report.
type Issue struct {
	Kind    string `json:"kind"` // e.g. "query_invalid", "form_binding", "lua_reference", "fk_violation", "constraint_violation", "conversion_failure"
	Object  string `json:"object,omitempty"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message"`
}

// ConversionFailure reports rows whose values cannot be converted.
type ConversionFailure struct {
	ColumnID string   `json:"column_id"`
	Column   string   `json:"column"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Count    int64    `json:"count"`
	Samples  []Sample `json:"samples"`
}

// Sample is one failing row.
type Sample struct {
	RowID int64 `json:"row_id"`
	Value any   `json:"value"`
}

// ImpactReport is returned by preview and by blocked applies.
type ImpactReport struct {
	Breaking             []Issue             `json:"breaking"`
	Warnings             []Issue             `json:"warnings"`
	RowsAffected         int64               `json:"rows_affected"`
	ConversionFailures   []ConversionFailure `json:"conversion_failures"`
	RestorePointRequired bool                `json:"restore_point_required"`
	SchemaVersion        int64               `json:"schema_version"` // version the ops were validated against
	CreatedIDs           map[string]string   `json:"created_ids,omitempty"`
}

// MarshalJSON keeps empty slices as [] for clients.
func (r ImpactReport) MarshalJSON() ([]byte, error) {
	type alias ImpactReport
	a := alias(r)
	if a.Breaking == nil {
		a.Breaking = []Issue{}
	}
	if a.Warnings == nil {
		a.Warnings = []Issue{}
	}
	if a.ConversionFailures == nil {
		a.ConversionFailures = []ConversionFailure{}
	}
	return json.Marshal(a)
}

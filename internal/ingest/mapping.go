package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// NewColumn is a field of a table the import creates.
type NewColumn struct {
	Source    int               `json:"source" doc:"Index of the source column that feeds this field."`
	Name      string            `json:"name" doc:"Field display name."`
	Type      catalog.FieldType `json:"field_type" doc:"short_text, long_text, integer, number, currency, yes_no, date, datetime or hyperlink."`
	MaxLength int               `json:"max_length,omitempty" doc:"short_text limit (default 255)."`
	Required  bool              `json:"is_required,omitempty"`
}

// NewTable describes the table an import creates.
type NewTable struct {
	Name    string      `json:"name"`
	Columns []NewColumn `json:"columns"`
}

// ColumnMap maps one source column onto an existing field; an empty column_id skips it.
type ColumnMap struct {
	Source   int    `json:"source"`
	ColumnID string `json:"column_id,omitempty"`
	// LonSource builds a geo_point from two numeric columns: source is then
	// the latitude column and lon_source the longitude column (ADR 0010).
	LonSource *int `json:"lon_source,omitempty" doc:"geo_point fields only: the source column holding longitude. source is then the latitude column, and both must be numbers."`
}

// ImportMapping says where staged rows go.
type ImportMapping struct {
	Mode        string      `json:"mode" enum:"append,upsert,replace"`
	Transaction string      `json:"transaction,omitempty" enum:"chunked,all_or_nothing" doc:"chunked (default) commits 2,000-row chunks and skips bad rows; all_or_nothing is one transaction that rolls back on the first error."`
	TableID     string      `json:"table_id,omitempty" doc:"Existing target table. Exactly one of table_id and new_table is given."`
	NewTable    *NewTable   `json:"new_table,omitempty"`
	Columns     []ColumnMap `json:"columns,omitempty" doc:"Existing table only: source column to field. Unlisted sources are skipped."`
	KeyColumnID string      `json:"key_column_id,omitempty" doc:"upsert only: the field that identifies a row. It must have a single-field unique index."`
}

// RowError is one rejected row.
type RowError struct {
	Row     int64  `json:"row" doc:"1-based data row number (the header is not counted)."`
	Column  string `json:"column,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type mapCol struct {
	Src   int    // source column index
	Stage string // staging column name
	Col   catalog.Column
	// Stage2 is set for a point built from two columns: Stage holds the
	// latitude and Stage2 the longitude (Src2 is its source index).
	Src2   int
	Stage2 string
}

// plan is a validated mapping.
type plan struct {
	Table   *catalog.Table // nil until a new table has been created
	NewSpec *catalog.TableSpec
	Cols    []mapCol
	Key     *catalog.Column
	Mode    string
	AllOrNo bool
	// Notes say what the mapping leaves out on purpose (attachments fields).
	Notes []string
}

var newTypes = map[catalog.FieldType]bool{
	catalog.ShortText: true, catalog.LongText: true, catalog.Integer: true, catalog.Number: true, catalog.Currency: true,
	catalog.YesNo: true, catalog.Date: true, catalog.DateTime: true, catalog.Hyperlink: true,
}

// planFor validates m against the catalog and the staged columns.
func planFor(sch *catalog.Schema, m ImportMapping, ncols int) (*plan, error) {
	p := &plan{Mode: m.Mode, AllOrNo: m.Transaction == TxAllOrNothing}
	switch m.Mode {
	case ModeAppend, ModeUpsert, ModeReplace:
	default:
		return nil, fault.New(fault.BadRequest, "Choose a mode: append, upsert or replace.")
	}
	switch m.Transaction {
	case "", TxChunked, TxAllOrNothing:
	default:
		return nil, fault.New(fault.BadRequest, "transaction must be chunked or all_or_nothing.")
	}
	if (m.TableID == "") == (m.NewTable == nil) {
		return nil, fault.New(fault.BadRequest, "Give either table_id (an existing table) or new_table.")
	}
	checkSrc := func(i int) error {
		if i < 0 || i >= ncols {
			return fault.Newf(fault.BadRequest, "Source column %d does not exist.", i)
		}
		return nil
	}
	if m.NewTable != nil {
		if m.Mode != ModeAppend {
			return nil, fault.New(fault.BadRequest, "A new table is empty: use mode append.")
		}
		if m.KeyColumnID != "" || len(m.Columns) > 0 {
			return nil, fault.New(fault.BadRequest, "For a new table, list the fields in new_table.columns only.")
		}
		name := strings.TrimSpace(m.NewTable.Name)
		if name == "" || len(name) > 200 || strings.ContainsAny(name, "\x00\n\r") {
			return nil, fault.New(fault.BadRequest, "Enter a table name of one line, at most 200 characters.")
		}
		if len(m.NewTable.Columns) == 0 {
			return nil, fault.New(fault.BadRequest, "Map at least one column.")
		}
		for _, t := range sch.Tables {
			if strings.EqualFold(t.DisplayName, name) {
				return nil, fault.Newf(fault.BadRequest, "A table named %q already exists.", name)
			}
		}
		spec := &catalog.TableSpec{ID: ids.New(), DisplayName: name}
		seenSrc, seenName := map[int]bool{}, map[string]bool{}
		for _, nc := range m.NewTable.Columns {
			if err := checkSrc(nc.Source); err != nil {
				return nil, err
			}
			if seenSrc[nc.Source] {
				return nil, fault.Newf(fault.BadRequest, "Source column %d is mapped twice.", nc.Source)
			}
			seenSrc[nc.Source] = true
			cn := strings.TrimSpace(nc.Name)
			if cn == "" || len(cn) > 200 || strings.ContainsAny(cn, "\x00\n\r") {
				return nil, fault.Newf(fault.BadRequest, "Source column %d needs a field name of one line, at most 200 characters.", nc.Source)
			}
			if seenName[strings.ToLower(cn)] || strings.EqualFold(cn, "id") {
				return nil, fault.Newf(fault.BadRequest, "The field name %q is used twice or reserved.", cn)
			}
			seenName[strings.ToLower(cn)] = true
			if !newTypes[nc.Type] {
				return nil, fault.Newf(fault.BadRequest, "Field %q: type %q is not available for imports.", cn, string(nc.Type))
			}
			cs := catalog.ColumnSpec{ID: ids.New(), DisplayName: cn, Type: nc.Type, Required: nc.Required}
			if nc.Type == catalog.ShortText && nc.MaxLength > 0 {
				cs.Options.MaxLength = nc.MaxLength
			}
			spec.Columns = append(spec.Columns, cs)
			p.Cols = append(p.Cols, mapCol{Src: nc.Source, Stage: stagingCol(nc.Source), Col: catalog.Column{
				ID: cs.ID, DisplayName: cn, Type: nc.Type, Options: cs.Options, Required: nc.Required}})
		}
		p.NewSpec = spec
		return p, nil
	}

	t, ok := sch.Table(m.TableID)
	if !ok {
		return nil, fault.New(fault.NotFound, "The target table does not exist.")
	}
	p.Table = t
	seenSrc, seenCol := map[int]bool{}, map[string]bool{}
	for _, cm := range m.Columns {
		if err := checkSrc(cm.Source); err != nil {
			return nil, err
		}
		if seenSrc[cm.Source] {
			return nil, fault.Newf(fault.BadRequest, "Source column %d is listed twice.", cm.Source)
		}
		seenSrc[cm.Source] = true
		if cm.LonSource != nil {
			if err := checkSrc(*cm.LonSource); err != nil {
				return nil, err
			}
			if seenSrc[*cm.LonSource] {
				return nil, fault.Newf(fault.BadRequest, "Source column %d is listed twice.", *cm.LonSource)
			}
			seenSrc[*cm.LonSource] = true
		}
		if cm.ColumnID == "" {
			if cm.LonSource != nil {
				return nil, fault.New(fault.BadRequest, "lon_source needs a column_id (a geo_point field).")
			}
			continue
		}
		c, ok := t.Column(cm.ColumnID)
		if !ok {
			return nil, fault.Newf(fault.BadRequest, "Field %s does not exist in %q.", cm.ColumnID, t.DisplayName)
		}
		if c.Type == catalog.Autonumber || c.Type == catalog.Calculated {
			return nil, fault.Newf(fault.BadRequest, "%q is filled in by the database and cannot be imported into.", c.DisplayName)
		}
		if seenCol[c.ID] {
			return nil, fault.Newf(fault.BadRequest, "%q is mapped from two source columns.", c.DisplayName)
		}
		seenCol[c.ID] = true
		if c.Type == catalog.Attachments {
			// Files cannot arrive in a spreadsheet: the column is left out, and the
			// mapping step says so.
			p.Notes = append(p.Notes, fmt.Sprintf("%q is an attachments field. Imports cannot attach files, so source column %d is ignored.", c.DisplayName, cm.Source+1))
			continue
		}
		mc := mapCol{Src: cm.Source, Stage: stagingCol(cm.Source), Col: *c}
		if cm.LonSource != nil {
			if c.Type != catalog.GeoPoint {
				return nil, fault.Newf(fault.BadRequest, "%q is not a geo point field, so it cannot be built from a latitude and a longitude column.", c.DisplayName)
			}
			mc.Src2, mc.Stage2 = *cm.LonSource, stagingCol(*cm.LonSource)
		}
		p.Cols = append(p.Cols, mc)
	}
	if len(p.Cols) == 0 {
		return nil, fault.New(fault.BadRequest, "Map at least one source column to a field.")
	}
	for _, c := range t.Columns {
		if c.Required && c.DefaultSQL == "" && !seenCol[c.ID] && c.Type != catalog.Autonumber && c.Type != catalog.Calculated {
			return nil, fault.Newf(fault.BadRequest, "The required field %q is not mapped and has no default value.", c.DisplayName)
		}
	}
	if m.Mode == ModeUpsert {
		if m.KeyColumnID == "" {
			return nil, fault.New(fault.BadRequest, "Choose the key field for upsert.")
		}
		if !seenCol[m.KeyColumnID] {
			return nil, fault.New(fault.BadRequest, "The key field must be one of the mapped fields.")
		}
		kc, _ := t.Column(m.KeyColumnID)
		unique := false
		for _, ix := range t.Indexes {
			if ix.Unique && len(ix.ColumnIDs) == 1 && ix.ColumnIDs[0] == kc.ID {
				unique = true
			}
		}
		if !unique {
			return nil, fault.Newf(fault.BadRequest, "Upsert needs a unique index on %q. Add one in Table Design first.", kc.DisplayName)
		}
		p.Key = kc
	} else if m.KeyColumnID != "" {
		return nil, fault.New(fault.BadRequest, "key_column_id is only used with upsert.")
	}
	return p, nil
}

// ---- value conversion -----------------------------------------------------

// toInput turns a staged value (nil, string, int64, float64) into what
// records.DecodeValue accepts for column c.
func toInput(c catalog.Column, v any) (any, error) {
	if pair, ok := v.(latLon); ok {
		return pair.input(c)
	}
	if v == nil {
		return nil, nil
	}
	if b, ok := v.([]byte); ok {
		v = string(b)
	}
	switch c.Type {
	case catalog.ShortText, catalog.LongText:
		switch x := v.(type) {
		case string:
			return x, nil
		case int64:
			return strconv.FormatInt(x, 10), nil
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64), nil
		}
	case catalog.Hyperlink:
		if x, ok := v.(string); ok {
			return strings.TrimSpace(x), nil
		}
	case catalog.Integer, catalog.Lookup, catalog.Number, catalog.Currency:
		switch x := v.(type) {
		case string:
			s := strings.TrimPrefix(strings.TrimSpace(x), "+")
			if s == "" {
				return nil, nil
			}
			return s, nil
		case int64:
			return strconv.FormatInt(x, 10), nil
		case float64:
			return strconv.FormatFloat(x, 'g', -1, 64), nil
		}
	case catalog.YesNo:
		switch x := v.(type) {
		case string:
			s := strings.TrimSpace(x)
			if s == "" {
				return nil, nil
			}
			if b, ok := parseBool(s); ok {
				return b, nil
			}
		case int64:
			if x == 0 || x == 1 {
				return json.Number(strconv.FormatInt(x, 10)), nil
			}
		case float64:
			if x == 0 || x == 1 {
				return json.Number(strconv.FormatInt(int64(x), 10)), nil
			}
		}
	case catalog.GeoPoint, catalog.GeoArea:
		// Text only ("lat, lon", WKT POINT(lon lat) or GeoJSON); DecodeValue parses it.
		if x, ok := v.(string); ok {
			s := strings.TrimSpace(x)
			if s == "" {
				return nil, nil
			}
			return s, nil
		}
	case catalog.Date:
		if x, ok := v.(string); ok {
			s := strings.TrimSpace(x)
			if s == "" {
				return nil, nil
			}
			return s, nil
		}
	case catalog.DateTime:
		if x, ok := v.(string); ok {
			s := strings.TrimSpace(x)
			if s == "" {
				return nil, nil
			}
			if n, ok := normalizeDateTime(s); ok {
				return n, nil
			}
			return s, nil
		}
	}
	return nil, fault.Newf(fault.BadRequest, "%q cannot take this value.", c.DisplayName)
}

// convertRow converts one staged row (values in p.Cols order) to storage values.
// The returned RowError names the first column that failed.
func convertRow(p *plan, vals []any) ([]any, *RowError) {
	out := make([]any, len(vals))
	for i, mc := range p.Cols {
		in, err := toInput(mc.Col, vals[i])
		if err == nil {
			out[i], err = records.DecodeValue(mc.Col, in)
		}
		if err != nil {
			fe := fault.Classify(err)
			return nil, &RowError{Column: mc.Col.DisplayName, Code: string(fe.Code), Message: fe.DisplayMessage()}
		}
		if out[i] == nil && mc.Col.Required && mc.Col.DefaultSQL == "" {
			return nil, &RowError{Column: mc.Col.DisplayName, Code: string(fault.RequiredField),
				Message: fmt.Sprintf("%q is required.", mc.Col.DisplayName)}
		}
	}
	return out, nil
}

// SetMapping validates and stores a mapping.
func (s *Service) SetMapping(ctx context.Context, l *workspace.Lease, id string, m ImportMapping) (ImportMeta, error) {
	path, err := s.importPaths(l.ID(), id)
	if err != nil {
		return ImportMeta{}, err
	}
	c, err := openStaging(ctx, path)
	if err != nil {
		return ImportMeta{}, err
	}
	defer func() { _ = c.Close() }()
	meta, err := readMeta(ctx, c)
	if err != nil {
		return ImportMeta{}, errImportNotFound
	}
	if meta.Status != StatusParsed && meta.Status != StatusMapped {
		return ImportMeta{}, fault.New(fault.BadRequest, "This import can no longer be changed.")
	}
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return ImportMeta{}, err
	}
	p, err := planFor(sch, m, len(meta.Columns))
	if err != nil {
		return ImportMeta{}, err
	}
	if m.Transaction == "" {
		m.Transaction = TxChunked
	}
	meta.Notes = p.Notes
	if meta.Notes == nil {
		meta.Notes = []string{}
	}
	meta.Mapping, meta.Status = &m, StatusMapped
	if err := saveMeta(ctx, c, meta); err != nil {
		return ImportMeta{}, err
	}
	return meta, nil
}

// ValidationResult is the outcome of the validation pass.
type ValidationResult struct {
	RowsChecked int64      `json:"rows_checked"`
	ErrorCount  int64      `json:"error_count" doc:"Rows that failed conversion."`
	Errors      []RowError `json:"errors" doc:"The first 100 failing rows."`
	Warnings    []string   `json:"warnings"`
}

// Validate converts every staged row with the records codec and reports the
// first 100 failures. It does not write anything and cannot see database
// constraints (unique, check) that only a real insert would trigger.
func (s *Service) Validate(ctx context.Context, l *workspace.Lease, id string) (ValidationResult, error) {
	path, err := s.importPaths(l.ID(), id)
	if err != nil {
		return ValidationResult{}, err
	}
	c, err := openStaging(ctx, path)
	if err != nil {
		return ValidationResult{}, err
	}
	defer func() { _ = c.Close() }()
	meta, err := readMeta(ctx, c)
	if err != nil {
		return ValidationResult{}, errImportNotFound
	}
	if meta.Mapping == nil {
		return ValidationResult{}, fault.New(fault.BadRequest, "Set the column mapping first.")
	}
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return ValidationResult{}, err
	}
	p, err := planFor(sch, *meta.Mapping, len(meta.Columns))
	if err != nil {
		return ValidationResult{}, err
	}
	res := ValidationResult{Errors: []RowError{}, Warnings: append([]string{}, p.Notes...)}
	err = readChunks(ctx, c, p, ChunkRows, func(rows []stagedRow) error {
		for _, r := range rows {
			res.RowsChecked++
			if _, re := convertRow(p, r.vals); re != nil {
				res.ErrorCount++
				if len(res.Errors) < MaxRowErrors {
					re.Row = r.n
					res.Errors = append(res.Errors, *re)
				}
			}
		}
		return nil
	})
	if err != nil {
		return ValidationResult{}, err
	}
	if meta.Mapping.Transaction == TxAllOrNothing && meta.RowCount > AllOrNothingWarnRows {
		res.Warnings = append(res.Warnings, "All-or-nothing holds the workspace's writer for the whole import; other people's saves wait until it finishes.")
	}
	return res, nil
}

type stagedRow struct {
	n    int64
	vals []any
}

// readChunks reads the mapped staging columns in n order, chunk rows at a time.
func readChunks(ctx context.Context, c *sqlitex.Conn, p *plan, chunk int, fn func([]stagedRow) error) error {
	cols := make([]string, len(p.Cols))
	for i, mc := range p.Cols {
		cols[i] = catalog.Quote(mc.Stage)
	}
	// Longitude columns of two-column points follow the plan columns.
	var lonAt []int
	for i, mc := range p.Cols {
		if mc.Stage2 != "" {
			cols = append(cols, catalog.Quote(mc.Stage2))
			lonAt = append(lonAt, i)
		}
	}
	sql := `SELECT n, ` + strings.Join(cols, ", ") + ` FROM data WHERE n > ? ORDER BY n LIMIT ?`
	var last int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := c.Query(ctx, sql, last, chunk)
		if err != nil {
			return err
		}
		var batch []stagedRow
		for rows.Next() {
			v := rows.Values()
			vals := v[1 : 1+len(p.Cols)]
			for j, i := range lonAt {
				vals[i] = latLon{lat: vals[i], lon: v[1+len(p.Cols)+j]}
			}
			batch = append(batch, stagedRow{n: v[0].(int64), vals: vals})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		last = batch[len(batch)-1].n
		if err := fn(batch); err != nil {
			return err
		}
	}
}

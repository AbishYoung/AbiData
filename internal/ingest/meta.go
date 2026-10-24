package ingest

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// ParseOptions record how the upload was read.
type ParseOptions struct {
	Format    string   `json:"format" enum:"csv,xlsx,json,ndjson,geojson" doc:"Detected or requested file format."`
	Delimiter string   `json:"delimiter,omitempty" doc:"CSV field delimiter (sniffed unless given)."`
	Encoding  string   `json:"encoding,omitempty" doc:"CSV text encoding that was detected: utf-8, utf-16le, utf-16be or windows-1252."`
	HasHeader bool     `json:"has_header" doc:"Whether the first row holds column names."`
	Sheet     string   `json:"sheet,omitempty" doc:"XLSX sheet that was read."`
	Sheets    []string `json:"sheets,omitempty" doc:"All sheets of an XLSX file."`
}

// SourceColumn describes one column of the staged file.
type SourceColumn struct {
	Index    int               `json:"index" doc:"Zero-based position; mappings refer to it."`
	Name     string            `json:"name" doc:"Cleaned column name (a suggestion for the field name)."`
	Original string            `json:"original" doc:"The header as it appears in the file."`
	Type     catalog.FieldType `json:"inferred_type" doc:"Type inferred from the first 1,000 non-empty values. Currency is never inferred."`
	Samples  []string          `json:"samples" doc:"Up to five sample values."`
	NonEmpty int               `json:"non_empty_sampled" doc:"How many of the sampled values were non-empty."`
	MaxLen   int               `json:"max_length" doc:"Longest sampled value, in characters."`
}

// ImportMeta is the state of one staged import (stored in the staging database).
type ImportMeta struct {
	ID         string         `json:"id"`
	SourceName string         `json:"source_name" doc:"The uploaded file's name."`
	Format     string         `json:"format" enum:"csv,xlsx,json,ndjson,geojson"`
	Options    ParseOptions   `json:"options"`
	Columns    []SourceColumn `json:"columns"`
	RowCount   int64          `json:"row_count"`
	Mapping    *ImportMapping `json:"mapping,omitempty"`
	Status     string         `json:"status" enum:"parsed,mapped,committing,failed"`
	CreatedBy  string         `json:"created_by"`
	CreatedAt  time.Time      `json:"created_at"`
	SizeBytes  int64          `json:"size_bytes"`
	Warnings   []string       `json:"warnings" doc:"Problems found while parsing that did not stop the import (at most 20)."`
	Notes      []string       `json:"notes" doc:"What the current mapping leaves out on purpose, for example attachments fields (files cannot be imported)."`
}

func createStagingSchema(ctx context.Context, c *sqlitex.Conn, cols []string) error {
	var b strings.Builder
	b.WriteString(`CREATE TABLE data (n INTEGER PRIMARY KEY`)
	for _, name := range cols {
		b.WriteString(", " + catalog.Quote(name))
	}
	b.WriteString(")")
	if _, err := c.Exec(ctx, sqlCreateStagingSchema.SQL); err != nil {
		return err
	}
	return c.ExecScript(ctx, b.String())
}

func saveMeta(ctx context.Context, c *sqlitex.Conn, m ImportMeta) error {
	vals := map[string]any{
		"id": m.ID, "source_name": m.SourceName, "format": m.Format, "options": m.Options, "columns": m.Columns,
		"row_count": m.RowCount, "mapping": m.Mapping, "status": m.Status, "created_by": m.CreatedBy,
		"created_at": m.CreatedAt, "size_bytes": m.SizeBytes, "warnings": m.Warnings, "notes": m.Notes,
	}
	for k, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := c.Exec(ctx, sqlSaveMeta.SQL, k, string(b)); err != nil {
			return err
		}
	}
	return nil
}

func readMeta(ctx context.Context, c *sqlitex.Conn) (ImportMeta, error) {
	rows, err := c.Query(ctx, sqlReadMeta.SQL)
	if err != nil {
		return ImportMeta{}, err
	}
	defer rows.Close()
	raw := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return ImportMeta{}, err
		}
		raw[k] = json.RawMessage(v)
	}
	if err := rows.Err(); err != nil {
		return ImportMeta{}, err
	}
	var m ImportMeta
	for k, dst := range map[string]any{
		"id": &m.ID, "source_name": &m.SourceName, "format": &m.Format, "options": &m.Options, "columns": &m.Columns,
		"row_count": &m.RowCount, "mapping": &m.Mapping, "status": &m.Status, "created_by": &m.CreatedBy,
		"created_at": &m.CreatedAt, "size_bytes": &m.SizeBytes, "warnings": &m.Warnings, "notes": &m.Notes,
	} {
		if b, ok := raw[k]; ok {
			if err := json.Unmarshal(b, dst); err != nil {
				return ImportMeta{}, err
			}
		}
	}
	if m.Columns == nil {
		m.Columns = []SourceColumn{}
	}
	if m.Warnings == nil {
		m.Warnings = []string{}
	}
	if m.Notes == nil {
		m.Notes = []string{}
	}
	return m, nil
}

// loadMeta reads an import's meta and checks it belongs to workspace ws.
func (s *Service) loadMeta(ctx context.Context, ws ids.WorkspaceID, id string) (ImportMeta, error) {
	path, err := s.importPaths(ws, id)
	if err != nil {
		return ImportMeta{}, err
	}
	c, err := openStaging(ctx, path)
	if err != nil {
		return ImportMeta{}, err
	}
	defer func() { _ = c.Close() }()
	m, err := readMeta(ctx, c)
	if err != nil {
		return ImportMeta{}, errImportNotFound
	}
	return m, nil
}

// transition atomically moves the import from one of the statuses in `from` to
// `to`; it reports false when the import was in another status.
func transition(ctx context.Context, c *sqlitex.Conn, from []string, to string) (bool, error) {
	cond := make([]string, len(from))
	args := []any{`"` + to + `"`}
	for i, f := range from {
		cond[i] = "?"
		args = append(args, `"`+f+`"`)
	}
	res, err := c.Exec(ctx, `UPDATE meta SET value = ? WHERE key = 'status' AND value IN (`+strings.Join(cond, ",")+`)`, args...)
	if err != nil {
		return false, err
	}
	return res.Changes == 1, nil
}

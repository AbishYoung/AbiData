package catalog

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"abidata/internal/sqlgen"
)

// Type conversions on PostgreSQL (ADR 0012 Phase 3). SQLite converts in the
// rebuild's copy through Go functions registered on the connection;
// PostgreSQL has no such functions, so the same Go rules (ConvertValue) run
// here, over every row's old value. The results wait in a temporary table,
// dropped at commit, which the rebuild's copy casts into the new column.

// pgConversion is one column's converted values.
type pgConversion struct {
	table string // the temporary table holding (id, v)
	to    FieldType
}

// pgConvert converts every row's value of old to nc's type, reports the
// values that cannot convert, as conversionFailures does on SQLite, and
// stages the results for the rebuild.
func (p *planner) pgConvert(t *Table, old, nc Column) error {
	readCol := sqlgen.Expr(sqlgen.Col{Name: old.PhysicalName})
	if isGeo(old.Type) {
		readCol = sqlgen.Raw{SQL: Quote(old.PhysicalName) + "::text"}
	}
	r, err := sqlgen.Render(p.d, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}, {Expr: readCol}},
		From: sqlgen.Table{Name: t.PhysicalName}, OrderBy: []sqlgen.Order{{Expr: sqlgen.Col{Name: "id"}}}})
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	rows, err := p.tx.Query(p.ctx, r.SQL, r.Args...)
	if err != nil {
		return err
	}
	type value struct {
		id int64
		v  any
	}
	var vals []value
	for rows.Next() {
		v := rows.Values()
		id, ok := v[0].(int64)
		if !ok {
			rows.Close()
			return fmt.Errorf("catalog: %s.id is %T", t.PhysicalName, v[0])
		}
		vals = append(vals, value{id, StorageForm(old.Type, v[1])})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	cf := ConversionFailure{ColumnID: nc.ID, Column: nc.DisplayName, From: string(old.Type), To: string(nc.Type)}
	out := make([]any, len(vals))
	for i, x := range vals {
		v, ok := ConvertValue(old.Type, nc.Type, x.v)
		if !ok {
			if x.v != nil {
				cf.Count++
				if len(cf.Samples) < 20 {
					cf.Samples = append(cf.Samples, Sample{RowID: x.id, Value: x.v})
				}
			}
			v = nil
			if nc.Type == YesNo {
				v = int64(0) // yes_no is never empty: what cannot convert becomes "No"
			}
		}
		out[i] = v
	}
	if cf.Count > 0 {
		p.report.ConversionFailures = append(p.report.ConversionFailures, cf)
		becomes := "empty"
		if nc.Type == YesNo {
			becomes = "No"
		}
		p.report.Breaking = append(p.report.Breaking, Issue{Kind: "conversion_failure", Object: "column", ID: nc.ID,
			Message: fmt.Sprintf("%d values in %q cannot be converted from %s to %s; they would become %s.", cf.Count, nc.DisplayName, old.Type, nc.Type, becomes)})
	}

	p.convSeq++
	conv := &pgConversion{table: fmt.Sprintf("_abidata_conv_%d", p.convSeq), to: nc.Type}
	if err := p.exec(`CREATE TEMPORARY TABLE ` + Quote(conv.table) + ` (id bigint PRIMARY KEY, v text) ON COMMIT DROP`); err != nil {
		return err
	}
	const batch = 500
	for start := 0; start < len(vals); start += batch {
		end := min(start+batch, len(vals))
		var b strings.Builder
		args := make([]any, 0, 2*(end-start))
		b.WriteString(`INSERT INTO pg_temp.` + Quote(conv.table) + ` (id, v) VALUES `)
		for i := start; i < end; i++ {
			if i > start {
				b.WriteString(", ")
			}
			b.WriteString("(?, ?)")
			args = append(args, vals[i].id, textForPG(nc.Type, out[i]))
		}
		if err := p.exec(b.String(), args...); err != nil {
			return err
		}
	}
	if p.pgConv == nil {
		p.pgConv = map[string]*pgConversion{}
	}
	p.pgConv[nc.ID] = conv
	return nil
}

// expr is the rebuild copy's expression for the converted column of a row of src.
func (c *pgConversion) expr(src string) sqlgen.Expr {
	typ, collate, _ := strings.Cut(sqlgen.PostgresType(fieldType(c.to)), " ") // text COLLATE "C"
	cast := `CAST(c.v AS ` + typ + `)`
	if collate != "" {
		cast += " " + collate
	}
	return sqlgen.Raw{SQL: `(SELECT ` + cast + ` FROM pg_temp.` + Quote(c.table) +
		` c WHERE c.id = ` + Quote(src) + `."id")`}
}

// StorageForm turns a value as pgx reads it into the form SQLite stores, which
// ConvertValue and the records codec take: 0/1 for yes_no, text for dates and
// times, JSON text for jsonb. Values SQLite returns pass through unchanged.
func StorageForm(t FieldType, v any) any {
	switch x := v.(type) {
	case bool:
		if x {
			return int64(1)
		}
		return int64(0)
	case time.Time:
		if t == Date {
			return x.UTC().Format("2006-01-02")
		}
		return x.UTC().Format("2006-01-02T15:04:05.000Z")
	case int32:
		return int64(x)
	case float32:
		return float64(x)
	case map[string]any, []any:
		b, err := json.Marshal(x)
		if err != nil {
			return nil
		}
		return string(b)
	case []byte:
		return string(x)
	}
	return v
}

// textForPG writes a converted storage value as text that casts to t's
// PostgreSQL column type.
func textForPG(t FieldType, v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case int64:
		if t == YesNo {
			return strconv.FormatBool(x != 0)
		}
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return x
	case []byte:
		return string(x)
	}
	return fmt.Sprint(v)
}

package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/scripts"
	"abidata/internal/sqlgen"
	"abidata/internal/workspace"
)

// Report is a report definition plus its module script.
type Report struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	SourceKind    string       `json:"source_kind" enum:"table,query"`
	SourceID      string       `json:"source_id"`
	ReportLayout  ReportLayout `json:"layout"`
	PageSetup     PageSetup    `json:"page_setup"`
	Version       int64        `json:"version"`
	UpdatedAt     time.Time    `json:"updated_at"`
	UpdatedBy     string       `json:"updated_by"`
	ModuleID      string       `json:"module_id"`
	ModuleSource  string       `json:"module_source"`
	ModuleVersion int64        `json:"module_version"`
}

// ReportInput creates or replaces a report.
type ReportInput struct {
	Name         string       `json:"name" minLength:"1" maxLength:"200"`
	SourceKind   string       `json:"source_kind" enum:"table,query"`
	SourceID     string       `json:"source_id"`
	ReportLayout ReportLayout `json:"layout"`
	PageSetup    PageSetup    `json:"page_setup"`
	ModuleSource string       `json:"module_source,omitempty" maxLength:"1000000" doc:"Lua helpers usable from calculated controls."`
}

// Service manages reports.
type Service struct {
	Schemas *catalog.Cache
	Records *records.Service
	Files   *files.Service // file attachments for the files module (nil: unavailable)
	Queries *query.Service
	Scripts *scripts.Service
	Runner  *script.Runner
	Events  interface {
		Publish(key, typ string, data any)
	}
}

const cols = `id, name, source_kind, source_id, layout, page_setup, version, updated_at, updated_by`

func scanReport(rows dbx.Rows) (Report, error) {
	var r Report
	var layout, ps, at string
	if err := rows.Scan(&r.ID, &r.Name, &r.SourceKind, &r.SourceID, &layout, &ps, &r.Version, &at, &r.UpdatedBy); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(layout), &r.ReportLayout); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(ps), &r.PageSetup); err != nil {
		return r, err
	}
	r.UpdatedAt, _ = dbx.ParseTime(at)
	return r, nil
}

func load(ctx context.Context, q dbx.Querier, id string) (Report, error) {
	rows, err := q.Query(ctx, `SELECT `+cols+` FROM _sys_reports WHERE id = ?`, id)
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Report{}, err
		}
		return Report{}, fault.E(fault.NotFound)
	}
	r, err := scanReport(rows)
	if err != nil {
		return Report{}, err
	}
	rows.Close()
	mod, ok, err := scripts.ByOwner(ctx, q, r.ID)
	if err != nil {
		return Report{}, err
	}
	if ok {
		r.ModuleID, r.ModuleSource, r.ModuleVersion = mod.ID, mod.Source, mod.Version
	}
	return r, nil
}

// List returns reports ordered by name.
func (s *Service) List(ctx context.Context, l *workspace.Lease) ([]Report, error) {
	out := []Report{}
	err := l.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, `SELECT `+cols+` FROM _sys_reports ORDER BY name COLLATE `+sqlgen.CICollation(pgdb.DialectOf(q)))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanReport(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// Get returns one report.
func (s *Service) Get(ctx context.Context, l *workspace.Lease, id string) (Report, error) {
	var r Report
	err := l.Read(ctx, func(q dbx.Querier) error {
		var err error
		r, err = load(ctx, q, id)
		return err
	})
	return r, err
}

// check validates a report input against the workspace: its source, layout, page setup and module.
// It returns the source table (nil for a query source).
func (s *Service) check(ctx context.Context, l *workspace.Lease, in *ReportInput) (*catalog.Table, error) {
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return nil, err
	}
	var t *catalog.Table
	var qcols []string
	switch in.SourceKind {
	case "table":
		tt, ok := sch.Table(in.SourceID)
		if !ok {
			return nil, fault.New(fault.BadRequest, "The report's table does not exist.")
		}
		t = tt
	case "query":
		q, err := s.Queries.Get(ctx, l, in.SourceID)
		if err != nil {
			return nil, fault.New(fault.BadRequest, "The report's query does not exist.")
		}
		qcols = q.Columns
	default:
		return nil, fault.New(fault.BadRequest, "Reports need a table or query source.")
	}
	if err := Validate(&in.ReportLayout, &in.PageSetup, t, qcols); err != nil {
		return nil, err
	}
	if _, err := script.Compile(in.ModuleSource, "report module"); err != nil {
		return nil, &fault.Error{Code: fault.ScriptError, Message: "The report module has a syntax error: " + fault.Classify(err).DisplayMessage()}
	}
	return t, nil
}

// Draft validates an unsaved report input and returns it as a Report that can be run but is not
// stored (the designer's live preview, ADR 0018). Nothing is written.
func (s *Service) Draft(ctx context.Context, l *workspace.Lease, in ReportInput) (Report, error) {
	if _, err := s.check(ctx, l, &in); err != nil {
		return Report{}, err
	}
	return Report{ID: "draft", Name: strings.TrimSpace(in.Name), SourceKind: in.SourceKind, SourceID: in.SourceID,
		ReportLayout: in.ReportLayout, PageSetup: in.PageSetup, ModuleSource: in.ModuleSource, UpdatedAt: time.Now()}, nil
}

// Put creates (id "") or replaces a report.
func (s *Service) Put(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64, in ReportInput, user string) (Report, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 200 {
		return Report{}, fault.New(fault.BadRequest, "Enter a report name of at most 200 characters.")
	}
	t, err := s.check(ctx, l, &in)
	if err != nil {
		return Report{}, err
	}
	r := Report{Name: name, SourceKind: in.SourceKind, SourceID: in.SourceID, ReportLayout: in.ReportLayout, PageSetup: in.PageSetup, UpdatedAt: time.Now(), UpdatedBy: user}
	err = l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		var modID string
		var modVer int64
		if id == "" {
			r.ID, r.Version = ids.New(), 1
		} else {
			cur, err := load(ctx, tx, id)
			if err != nil {
				return err
			}
			if cur.Version != expectedVersion {
				return fault.New(fault.ConcurrentModification, "This report was changed by someone else since you opened it. Nothing was saved.").
					WithDetails(map[string]any{"current_version": cur.Version})
			}
			r.ID, r.Version, modID, modVer = id, cur.Version+1, cur.ModuleID, cur.ModuleVersion
		}
		layout, _ := json.Marshal(r.ReportLayout)
		ps, _ := json.Marshal(r.PageSetup)
		_, err := tx.Exec(ctx, `INSERT INTO _sys_reports(`+cols+`) VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name = excluded.name, source_kind = excluded.source_kind, source_id = excluded.source_id,
			layout = excluded.layout, page_setup = excluded.page_setup, version = excluded.version, updated_at = excluded.updated_at,
			updated_by = excluded.updated_by`, r.ID, r.Name, r.SourceKind, r.SourceID, string(layout), string(ps), r.Version,
			dbx.FormatTime(r.UpdatedAt), r.UpdatedBy)
		if err != nil {
			if fault.Classify(err).Code == fault.DuplicateValue {
				return fault.Newf(fault.DuplicateValue, "A report named %q already exists.", r.Name)
			}
			return err
		}
		mod, err := s.Scripts.PutTx(ctx, tx, modID, modVer, scripts.ScriptInput{Name: "report." + r.ID, Kind: "report_module", OwnerID: r.ID, Source: in.ModuleSource}, user)
		if err != nil {
			return err
		}
		r.ModuleID, r.ModuleSource, r.ModuleVersion = mod.ID, mod.Source, mod.Version
		if _, err := tx.Exec(ctx, sqlPut.SQL, r.ID); err != nil {
			return err
		}
		targets := map[string]string{in.SourceKind: in.SourceID}
		for _, b := range r.ReportLayout.AllBands() {
			for _, c := range b.Controls {
				if c.Bind != nil && c.Bind.ColumnID != "id" && t != nil {
					targets["column:"+c.Bind.ColumnID] = c.Bind.ColumnID
				}
				if c.Map != nil && c.Map.OverlayQueryID != "" {
					targets["query:"+c.Map.OverlayQueryID] = c.Map.OverlayQueryID
				}
			}
		}
		for k, v := range targets {
			kind := k
			if pre, _, ok := strings.Cut(k, ":"); ok {
				kind = pre
			}
			if _, err := tx.Exec(ctx, sqlPut2.SQL, r.ID, kind, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	if s.Events != nil {
		s.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "report", "id": r.ID, "version": r.Version})
	}
	return r, nil
}

// Delete removes a report and its module.
func (s *Service) Delete(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64) error {
	err := l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		cur, err := load(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return fault.E(fault.ConcurrentModification)
		}
		for _, q := range []string{`DELETE FROM _sys_dependencies WHERE dependent_kind = 'report' AND dependent_id = ?`,
			`DELETE FROM _sys_scripts WHERE owner_id = ?`, `DELETE FROM _sys_reports WHERE id = ?`} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && s.Events != nil {
		s.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "report", "id": id, "deleted": true})
	}
	return err
}

// ---- formatting ----------------------------------------------------------------

// format renders a value for display. Formats: "" (by field type), "currency",
// "fixed:N", "percent", "integer", "date:short|medium|long", "datetime:short|medium",
// "yesno", "text".
func format(v any, spec string, f *field, tz *time.Location) any {
	if v == nil {
		return ""
	}
	ft := catalog.FieldType("")
	if f != nil && f.col != nil {
		ft = f.col.Type
		if ft == catalog.Calculated {
			ft = records.ResultFieldType(f.col.Options.ResultType)
		}
	}
	kind, arg, _ := strings.Cut(spec, ":")
	if kind == "" {
		if text, ok := geoText(v, ft); ok {
			return text
		}
		switch ft {
		case catalog.Currency:
			kind = "currency"
		case catalog.YesNo:
			kind = "yesno"
		case catalog.Date:
			kind, arg = "date", "short"
		case catalog.DateTime:
			kind, arg = "datetime", "short"
		}
	}
	switch kind {
	case "currency":
		if d, ok := asDecimal(v); ok {
			return thousands(d.StringFixed(2))
		}
	case "pattern":
		if mk, err := parseMask(arg); err == nil {
			if d, ok := asDecimal(v); ok {
				return applyMask(mk, d)
			}
		}
	case "datepattern":
		if t, ok := asTime(v, tz); ok && arg != "" {
			return formatDatePattern(t, arg)
		}
	case "fixed":
		// Decimal places come from the report template. Parsing at 8 bits bounds
		// the value (out-of-range input saturates to ±127) before it is clamped
		// to 0..10 and narrowed to int32.
		n, _ := strconv.ParseInt(arg, 10, 8)
		places := int32(min(max(n, 0), 10))
		if d, ok := asDecimal(v); ok {
			return thousands(d.StringFixed(places))
		}
	case "integer":
		if d, ok := asDecimal(v); ok {
			return thousands(d.Round(0).String())
		}
	case "percent":
		if d, ok := asDecimal(v); ok {
			return d.Mul(decimal.NewFromInt(100)).StringFixed(1) + "%"
		}
	case "yesno":
		switch x := v.(type) {
		case bool:
			if x {
				return "Yes"
			}
			return "No"
		case int64:
			if x != 0 {
				return "Yes"
			}
			return "No"
		}
	case "date":
		if t, ok := asTime(v, tz); ok {
			switch arg {
			case "long":
				return t.Format("Monday, January 2, 2006")
			case "medium":
				return t.Format("Jan 2, 2006")
			}
			return t.Format("2006-01-02")
		}
	case "datetime":
		if t, ok := asTime(v, tz); ok {
			if arg == "medium" {
				return t.Format("Jan 2, 2006 15:04")
			}
			return t.Format("2006-01-02 15:04")
		}
	}
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return ""
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "Yes"
		}
		return "No"
	case map[string]any, []any:
		b, _ := json.Marshal(x)
		return string(b)
	}
	return fmt.Sprint(v)
}

func asDecimal(v any) (decimal.Decimal, bool) {
	switch x := v.(type) {
	case int64:
		return decimal.NewFromInt(x), true
	case float64:
		return decimal.NewFromFloat(x), true
	case string:
		d, err := decimal.NewFromString(x)
		return d, err == nil
	}
	return decimal.Decimal{}, false
}

func asTime(v any, tz *time.Location) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.In(tz), true
	}
	return time.Time{}, false
}

// thousands inserts "," separators into the integer part of a decimal string.
func thousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// SchemaValidator reports reports whose bindings a schema change would break.
func SchemaValidator(ctx context.Context, tx dbx.Tx, sch *catalog.Schema) ([]catalog.Issue, []catalog.Issue, error) {
	rows, err := tx.Query(ctx, `SELECT `+cols+` FROM _sys_reports WHERE source_kind = 'table'`)
	if err != nil {
		return nil, nil, err
	}
	var reps []Report
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		reps = append(reps, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var breaking []catalog.Issue
	for _, r := range reps {
		t, ok := sch.Table(r.SourceID)
		if !ok {
			breaking = append(breaking, catalog.Issue{Kind: "report_binding", Object: "report", ID: r.ID, Message: fmt.Sprintf("Report %q would lose its table.", r.Name)})
			continue
		}
		if err := Validate(&r.ReportLayout, &r.PageSetup, t, nil); err != nil {
			breaking = append(breaking, catalog.Issue{Kind: "report_binding", Object: "report", ID: r.ID,
				Message: fmt.Sprintf("Report %q would stop working: %s", r.Name, fault.Classify(err).DisplayMessage())})
		}
	}
	return breaking, nil, nil
}

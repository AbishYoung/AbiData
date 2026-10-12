package forms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/scripts"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Form is a form definition plus its module script.
type Form struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	SourceKind     string    `json:"source_kind" enum:"table,query,none"`
	SourceID       string    `json:"source_id,omitempty"`
	DefaultView    string    `json:"default_view" enum:"single,continuous,datasheet"`
	AllowEdits     bool      `json:"allow_edits"`
	AllowAdditions bool      `json:"allow_additions"`
	AllowDeletions bool      `json:"allow_deletions"`
	Layout         Layout    `json:"layout"`
	Version        int64     `json:"version"`
	UpdatedAt      time.Time `json:"updated_at"`
	UpdatedBy      string    `json:"updated_by"`
	ModuleID       string    `json:"module_id"`
	ModuleSource   string    `json:"module_source"`
	ModuleVersion  int64     `json:"module_version"`
}

// Input creates or replaces a form (and its module source).
type FormInput struct {
	Name           string `json:"name" minLength:"1" maxLength:"200"`
	SourceKind     string `json:"source_kind" enum:"table,query,none"`
	SourceID       string `json:"source_id,omitempty"`
	DefaultView    string `json:"default_view,omitempty" enum:"single,continuous,datasheet,"`
	AllowEdits     *bool  `json:"allow_edits,omitempty"`
	AllowAdditions *bool  `json:"allow_additions,omitempty"`
	AllowDeletions *bool  `json:"allow_deletions,omitempty"`
	Layout         Layout `json:"layout"`
	ModuleSource   string `json:"module_source" maxLength:"1000000" doc:"Lua form module: event handler functions."`
}

// Service manages forms and dispatches their events.
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
	// Enqueue starts a job run for jobs.enqueue (nil: unavailable). Forms may only
	// enqueue jobs that opt in with allow_enqueue (enforced by the jobs service).
	Enqueue func(ctx context.Context, l *workspace.Lease, jobName, by string, args map[string]any, fromForm bool) (string, error)
	// UIEventBudgetMS returns a workspace's UI-event wall-time quota in ms
	// (0 or nil = the default 1 s; clamped to 250 ms–5 s).
	UIEventBudgetMS func(ws ids.WorkspaceID) int
}

const formCols = `id, name, source_kind, source_id, default_view, allow_edits, allow_additions, allow_deletions, layout, version, updated_at, updated_by`

func scanForm(rows *sqlitex.Rows) (Form, error) {
	var f Form
	var src *string
	var layout, at string
	if err := rows.Scan(&f.ID, &f.Name, &f.SourceKind, &src, &f.DefaultView, &f.AllowEdits, &f.AllowAdditions, &f.AllowDeletions,
		&layout, &f.Version, &at, &f.UpdatedBy); err != nil {
		return f, err
	}
	if src != nil {
		f.SourceID = *src
	}
	if err := json.Unmarshal([]byte(layout), &f.Layout); err != nil {
		return f, fmt.Errorf("form %s layout: %w", f.ID, err)
	}
	f.UpdatedAt, _ = sqlitex.ParseTime(at)
	return f, nil
}

func loadForm(ctx context.Context, q sqlitex.Querier, id string) (Form, error) {
	rows, err := q.Query(ctx, `SELECT `+formCols+` FROM _sys_forms WHERE id = ?`, id)
	if err != nil {
		return Form{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Form{}, err
		}
		return Form{}, fault.E(fault.NotFound)
	}
	f, err := scanForm(rows)
	if err != nil {
		return Form{}, err
	}
	rows.Close()
	mod, ok, err := scripts.ByOwner(ctx, q, f.ID)
	if err != nil {
		return Form{}, err
	}
	if ok {
		f.ModuleID, f.ModuleSource, f.ModuleVersion = mod.ID, mod.Source, mod.Version
	}
	return f, nil
}

// List returns form summaries (no module source) ordered by name.
func (s *Service) List(ctx context.Context, l *workspace.Lease) ([]Form, error) {
	out := []Form{}
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(ctx, `SELECT `+formCols+` FROM _sys_forms ORDER BY name COLLATE NOCASE`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanForm(rows)
			if err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}

// Get returns a form with its module.
func (s *Service) Get(ctx context.Context, l *workspace.Lease, id string) (Form, error) {
	var f Form
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		var err error
		f, err = loadForm(ctx, q, id)
		return err
	})
	return f, err
}

// source resolves what a form's controls may bind to.
func (s *Service) source(ctx context.Context, l *workspace.Lease, sch *catalog.Schema, kind, id string) (Source, error) {
	switch kind {
	case "table":
		t, ok := sch.Table(id)
		if !ok {
			return Source{}, fault.New(fault.BadRequest, "The form's table does not exist.")
		}
		return Source{Table: t}, nil
	case "query":
		q, err := s.Queries.Get(ctx, l, id)
		if err != nil {
			return Source{}, fault.New(fault.BadRequest, "The form's query does not exist.")
		}
		if q.Editable {
			if t, ok := sch.Table(q.TableID); ok {
				return Source{Table: t}, nil
			}
		}
		return Source{Columns: q.Columns}, nil
	case "none":
		return Source{}, nil
	}
	return Source{}, fault.Newf(fault.BadRequest, "Unknown record source kind %q.", kind)
}

// Put creates (id "") or replaces a form and its module in one transaction.
func (s *Service) Put(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64, in FormInput, user string) (Form, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 200 {
		return Form{}, fault.New(fault.BadRequest, "Enter a form name of at most 200 characters.")
	}
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return Form{}, err
	}
	src, err := s.source(ctx, l, sch, in.SourceKind, in.SourceID)
	if err != nil {
		return Form{}, err
	}
	if in.SourceKind == "none" {
		in.SourceID = ""
	}
	view := in.DefaultView
	if view == "" {
		view = "single"
	}
	boolOr := func(p *bool) bool { return p == nil || *p }
	f := Form{Name: name, SourceKind: in.SourceKind, SourceID: in.SourceID, DefaultView: view, AllowEdits: boolOr(in.AllowEdits),
		AllowAdditions: boolOr(in.AllowAdditions), AllowDeletions: boolOr(in.AllowDeletions), Layout: in.Layout,
		UpdatedAt: time.Now(), UpdatedBy: user}
	if _, err := script.Compile(in.ModuleSource, "form module"); err != nil {
		return Form{}, &fault.Error{Code: fault.ScriptError, Message: "The form module has a syntax error: " + fault.Classify(err).DisplayMessage()}
	}
	err = l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		exists := func(fid string) bool {
			var n int
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM _sys_forms WHERE id = ?`, fid).Scan(&n)
			return n > 0 || fid == f.ID
		}
		var moduleID string
		var moduleVer int64
		if id == "" {
			f.ID, f.Version = ids.New(), 1
		} else {
			cur, err := loadForm(ctx, tx, id)
			if err != nil {
				return err
			}
			if cur.Version != expectedVersion {
				return fault.New(fault.ConcurrentModification, "This form was changed by someone else since you opened it. Nothing was saved.").
					WithDetails(map[string]any{"current_version": cur.Version})
			}
			f.ID, f.Version = id, cur.Version+1
			moduleID, moduleVer = cur.ModuleID, cur.ModuleVersion
		}
		if err := Validate(&f.Layout, src, sch, exists); err != nil {
			return err
		}
		layout, err := json.Marshal(f.Layout)
		if err != nil {
			return err
		}
		var srcID any
		if f.SourceID != "" {
			srcID = f.SourceID
		}
		_, err = tx.Exec(ctx, `INSERT INTO _sys_forms(`+formCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name = excluded.name, source_kind = excluded.source_kind, source_id = excluded.source_id,
			default_view = excluded.default_view, allow_edits = excluded.allow_edits, allow_additions = excluded.allow_additions,
			allow_deletions = excluded.allow_deletions, layout = excluded.layout, version = excluded.version,
			updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
			f.ID, f.Name, f.SourceKind, srcID, f.DefaultView, f.AllowEdits, f.AllowAdditions, f.AllowDeletions, string(layout),
			f.Version, sqlitex.FormatTime(f.UpdatedAt), f.UpdatedBy)
		if err != nil {
			if fault.Classify(err).Code == fault.DuplicateValue {
				return fault.Newf(fault.DuplicateValue, "A form named %q already exists.", f.Name)
			}
			return err
		}
		mod, err := s.Scripts.PutTx(ctx, tx, moduleID, moduleVer, scripts.ScriptInput{Name: "form." + f.ID, Kind: "form_module", OwnerID: f.ID, Source: in.ModuleSource}, user)
		if err != nil {
			return err
		}
		f.ModuleID, f.ModuleSource, f.ModuleVersion = mod.ID, mod.Source, mod.Version
		return writeDeps(ctx, tx, f)
	})
	if err != nil {
		return Form{}, err
	}
	if s.Events != nil {
		s.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "form", "id": f.ID, "version": f.Version})
	}
	return f, nil
}

func writeDeps(ctx context.Context, tx *sqlitex.Tx, f Form) error {
	if _, err := tx.Exec(ctx, `DELETE FROM _sys_dependencies WHERE dependent_kind = 'form' AND dependent_id = ?`, f.ID); err != nil {
		return err
	}
	add := func(kind, target string) error {
		_, err := tx.Exec(ctx, `INSERT OR IGNORE INTO _sys_dependencies(dependent_kind, dependent_id, target_kind, target_id, confidence) VALUES ('form', ?, ?, ?, 'exact')`, f.ID, kind, target)
		return err
	}
	if f.SourceID != "" {
		if err := add(map[string]string{"table": "table", "query": "query"}[f.SourceKind], f.SourceID); err != nil {
			return err
		}
	}
	for _, c := range f.Layout.BoundColumns() {
		if c != "" && c != "id" {
			if err := add("column", c); err != nil {
				return err
			}
		}
	}
	for _, c := range f.Layout.AllControls() {
		if c.Subform != nil {
			if err := add("form", c.Subform.FormID); err != nil {
				return err
			}
		}
		if c.Map != nil && c.Map.OverlayQueryID != "" {
			if err := add("query", c.Map.OverlayQueryID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Delete removes a form and its module unless other forms embed it.
func (s *Service) Delete(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64) error {
	err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		cur, err := loadForm(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.Version != expectedVersion {
			return fault.E(fault.ConcurrentModification)
		}
		var users int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM _sys_dependencies WHERE dependent_kind = 'form' AND target_kind = 'form' AND target_id = ?`, id).Scan(&users); err != nil {
			return err
		}
		if users > 0 {
			return fault.New(fault.LinkedRecords, "Other forms use this form as a subform. Remove it from them first. Nothing was deleted.")
		}
		for _, q := range []string{`DELETE FROM _sys_dependencies WHERE dependent_kind = 'form' AND dependent_id = ?`,
			`DELETE FROM _sys_scripts WHERE owner_id = ?`, `DELETE FROM _sys_forms WHERE id = ?`} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && s.Events != nil {
		s.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "form", "id": id, "deleted": true})
	}
	return err
}

// SchemaValidator reports forms whose bindings a schema change would break.
func SchemaValidator(ctx context.Context, tx *sqlitex.Tx, sch *catalog.Schema) ([]catalog.Issue, []catalog.Issue, error) {
	rows, err := tx.Query(ctx, `SELECT `+formCols+` FROM _sys_forms`)
	if err != nil {
		return nil, nil, err
	}
	var forms []Form
	for rows.Next() {
		f, err := scanForm(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		forms = append(forms, f)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var breaking []catalog.Issue
	for _, f := range forms {
		if f.SourceKind != "table" {
			continue
		}
		t, ok := sch.Table(f.SourceID)
		if !ok {
			breaking = append(breaking, catalog.Issue{Kind: "form_binding", Object: "form", ID: f.ID, Message: fmt.Sprintf("Form %q would lose its table.", f.Name)})
			continue
		}
		for _, c := range f.Layout.AllControls() {
			if c.Bind != nil && c.Bind.ColumnID != "id" {
				if _, ok := t.Column(c.Bind.ColumnID); !ok {
					breaking = append(breaking, catalog.Issue{Kind: "form_binding", Object: "form", ID: f.ID,
						Message: fmt.Sprintf("Form %q control %q is bound to a field that would no longer exist.", f.Name, c.Name)})
				}
			}
		}
	}
	return breaking, nil, nil
}

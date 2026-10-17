package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Saved is a saved query.
type Saved struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind" enum:"visual,sql"`
	Visual      *Model    `json:"visual_model,omitempty"`
	SQL         string    `json:"sql_text"`
	Parameters  []Param   `json:"parameters"`
	Description string    `json:"description"`
	Version     int64     `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
	Columns     []string  `json:"columns"`
	Editable    bool      `json:"editable"`
	TableID     string    `json:"table_id,omitempty"`
}

// SavedInput creates or replaces a saved query.
type SavedInput struct {
	Name        string  `json:"name" minLength:"1" maxLength:"200"`
	Kind        string  `json:"kind" enum:"visual,sql"`
	Visual      *Model  `json:"visual_model,omitempty"`
	SQL         string  `json:"sql_text,omitempty" doc:"For kind sql: a single read-only SELECT/WITH/VALUES statement."`
	Parameters  []Param `json:"parameters,omitempty"`
	Description string  `json:"description,omitempty" maxLength:"4000"`
}

// Publisher receives design.changed events.
type Publisher interface {
	Publish(key, typ string, data any)
}

// Service manages saved queries.
type Service struct {
	Schemas *catalog.Cache
	Events  Publisher
}

const maxNesting = 4

// loadAll reads every saved query (inside a read or write transaction).
func loadAll(ctx context.Context, q sqlitex.Querier) (map[string]*Saved, error) {
	rows, err := q.Query(ctx, `SELECT id, name, kind, visual_model, sql_text, parameters, description, version, updated_at, updated_by FROM _sys_queries`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*Saved{}
	for rows.Next() {
		var s Saved
		var vis *string
		var params, at string
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &vis, &s.SQL, &params, &s.Description, &s.Version, &at, &s.UpdatedBy); err != nil {
			return nil, err
		}
		if vis != nil {
			var m Model
			if err := json.Unmarshal([]byte(*vis), &m); err != nil {
				return nil, fmt.Errorf("query %s: visual model: %w", s.ID, err)
			}
			s.Visual = &m
		}
		if err := json.Unmarshal([]byte(params), &s.Parameters); err != nil {
			return nil, err
		}
		s.UpdatedAt, _ = sqlitex.ParseTime(at)
		out[s.ID] = &s
	}
	return out, rows.Err()
}

// resolver compiles saved queries used as sources, detecting cycles.
type resolver struct {
	sch   *catalog.Schema
	all   map[string]*Saved
	stack []string
}

func (r *resolver) resolve(id string) (SavedQuery, bool) {
	s, ok := r.all[id]
	if !ok || len(r.stack) >= maxNesting {
		return SavedQuery{}, false
	}
	for _, x := range r.stack {
		if x == id {
			return SavedQuery{}, false
		}
	}
	if s.Kind == "sql" {
		return SavedQuery{ID: id, SQL: s.SQL, Columns: s.Columns}, len(s.Columns) > 0
	}
	r.stack = append(r.stack, id)
	defer func() { r.stack = r.stack[:len(r.stack)-1] }()
	c, err := Compile(r.sch, *s.Visual, r.resolve)
	if err != nil {
		return SavedQuery{}, false
	}
	return SavedQuery{ID: id, SQL: c.SQL, Columns: c.Columns}, true
}

// describeSQL validates a SQL-kind query under the editor policy (so every
// saved query can be run by editors) and returns its columns.
func describeSQL(ctx context.Context, c *sqlitex.Conn, sql string) ([]string, error) {
	if strings.TrimSpace(sql) == "" || len(sql) > MaxSQLLen {
		return nil, fault.New(fault.BadRequest, "Enter a SQL statement.")
	}
	var desc sqlitex.Statement
	var denial Denial
	err := wrapAuth(&denial, c.WithAuthorizer(Policy{}.Authorizer(&denial), func() error {
		var err error
		desc, err = c.Describe(ctx, sql)
		return err
	}))
	if err != nil {
		return nil, err
	}
	if !desc.ReadOnly {
		return nil, fault.New(fault.SQLNotAllowed, "Saved queries must only read data.")
	}
	for _, p := range desc.Params {
		if strings.HasPrefix(p, "?") {
			return nil, fault.New(fault.BadRequest, "Use named parameters such as :start_date instead of ?.")
		}
	}
	return desc.Columns, nil
}

// fill computes derived fields (SQL, columns, editability) for s.
func fill(ctx context.Context, c *sqlitex.Conn, sch *catalog.Schema, all map[string]*Saved, s *Saved) (Deps, error) {
	switch s.Kind {
	case "visual":
		if s.Visual == nil {
			return Deps{}, fault.New(fault.BadRequest, "A visual query needs a model.")
		}
		r := &resolver{sch: sch, all: all, stack: []string{s.ID}}
		comp, err := Compile(sch, *s.Visual, r.resolve)
		if err != nil {
			return Deps{}, err
		}
		s.SQL, s.Columns, s.Editable, s.TableID = comp.SQL, comp.Columns, comp.Editable, comp.TableID
		s.Parameters = s.Visual.Parameters
		// The compiled SQL must also pass the editor policy.
		if _, err := describeSQL(ctx, c, s.SQL); err != nil {
			return Deps{}, err
		}
		return comp.Deps, nil
	case "sql":
		s.Visual = nil
		cols, err := describeSQL(ctx, c, s.SQL)
		if err != nil {
			return Deps{}, err
		}
		s.Columns, s.Editable, s.TableID = cols, false, ""
		return Deps{}, nil
	}
	return Deps{}, fault.Newf(fault.BadRequest, "Unknown query kind %q.", s.Kind)
}

func writeDeps(ctx context.Context, tx *sqlitex.Tx, id string, d Deps) error {
	if _, err := tx.Exec(ctx, `DELETE FROM _sys_dependencies WHERE dependent_kind = 'query' AND dependent_id = ?`, id); err != nil {
		return err
	}
	for kind, list := range map[string][]string{"table": d.Tables, "column": d.Columns, "query": d.Queries} {
		for _, target := range list {
			if _, err := tx.Exec(ctx, `INSERT OR IGNORE INTO _sys_dependencies(dependent_kind, dependent_id, target_kind, target_id, confidence)
				VALUES ('query', ?, ?, ?, 'exact')`, id, kind, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func save(ctx context.Context, tx *sqlitex.Tx, s *Saved) error {
	var vis any
	if s.Visual != nil {
		b, err := json.Marshal(s.Visual)
		if err != nil {
			return err
		}
		vis = string(b)
	}
	params, err := json.Marshal(nonNil(s.Parameters))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO _sys_queries(id, name, kind, visual_model, sql_text, parameters, description, version, updated_at, updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name = excluded.name, kind = excluded.kind,
		visual_model = excluded.visual_model, sql_text = excluded.sql_text, parameters = excluded.parameters,
		description = excluded.description, version = excluded.version, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		s.ID, s.Name, s.Kind, vis, s.SQL, string(params), s.Description, s.Version, sqlitex.FormatTime(s.UpdatedAt), s.UpdatedBy)
	if err != nil && fault.Classify(err).Code == fault.DuplicateValue {
		return fault.Newf(fault.DuplicateValue, "A query named %q already exists.", s.Name)
	}
	return err
}

func nonNil(p []Param) []Param {
	if p == nil {
		return []Param{}
	}
	return p
}

// recompileDependents refreshes visual queries that use query id as a source.
func recompileDependents(ctx context.Context, tx *sqlitex.Tx, sch *catalog.Schema, all map[string]*Saved, id string) error {
	seen := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		rows, err := tx.Query(ctx, `SELECT dependent_id FROM _sys_dependencies WHERE dependent_kind = 'query' AND target_kind = 'query' AND target_id = ?`, cur)
		if err != nil {
			return err
		}
		var deps []string
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				rows.Close()
				return err
			}
			deps = append(deps, d)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range deps {
			s, ok := all[d]
			if !ok || seen[d] || s.Kind != "visual" {
				continue
			}
			seen[d] = true
			if _, err := fill(ctx, tx.Conn, sch, all, s); err != nil {
				return fault.Newf(fault.BadRequest, "This change would break query %q: %s", s.Name, fault.Classify(err).DisplayMessage())
			}
			if err := save(ctx, tx, s); err != nil {
				return err
			}
			queue = append(queue, d)
		}
	}
	return nil
}

// List returns all saved queries ordered by name.
func (svc *Service) List(ctx context.Context, l *workspace.Lease) ([]Saved, error) {
	sch, err := svc.Schemas.Get(ctx, l)
	if err != nil {
		return nil, err
	}
	var out []Saved
	err = l.ReadConn(ctx, func(c *sqlitex.Conn) error {
		all, err := loadAll(ctx, c)
		if err != nil {
			return err
		}
		for _, s := range all {
			_, _ = fill(ctx, c, sch, all, s) // derived fields; stale ones simply show no columns
			out = append(out, *s)
		}
		return nil
	})
	sortByName(out)
	return out, err
}

func sortByName(qs []Saved) {
	for i := 1; i < len(qs); i++ {
		for j := i; j > 0 && strings.ToLower(qs[j].Name) < strings.ToLower(qs[j-1].Name); j-- {
			qs[j], qs[j-1] = qs[j-1], qs[j]
		}
	}
}

// Get returns one saved query.
func (svc *Service) Get(ctx context.Context, l *workspace.Lease, id string) (Saved, error) {
	sch, err := svc.Schemas.Get(ctx, l)
	if err != nil {
		return Saved{}, err
	}
	var out Saved
	err = l.ReadConn(ctx, func(c *sqlitex.Conn) error {
		all, err := loadAll(ctx, c)
		if err != nil {
			return err
		}
		s, ok := all[id]
		if !ok {
			return fault.E(fault.NotFound)
		}
		if _, err := fill(ctx, c, sch, all, s); err != nil {
			s.Columns = nil
		}
		out = *s
		return nil
	})
	return out, err
}

// Put creates (expectedVersion == 0, id empty) or replaces (If-Match version) a query.
func (svc *Service) Put(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64, in SavedInput, user string) (Saved, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 200 {
		return Saved{}, fault.New(fault.BadRequest, "Enter a query name of at most 200 characters.")
	}
	sch, err := svc.Schemas.Get(ctx, l)
	if err != nil {
		return Saved{}, err
	}
	var out Saved
	err = l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		all, err := loadAll(ctx, tx)
		if err != nil {
			return err
		}
		s := &Saved{Name: name, Kind: in.Kind, Visual: in.Visual, SQL: in.SQL, Parameters: in.Parameters, Description: in.Description,
			UpdatedAt: time.Now(), UpdatedBy: user}
		if id == "" {
			s.ID, s.Version = ids.New(), 1
		} else {
			cur, ok := all[id]
			if !ok {
				return fault.E(fault.NotFound)
			}
			if cur.Version != expectedVersion {
				return fault.New(fault.ConcurrentModification, "This query was changed by someone else since you opened it. Nothing was saved.").
					WithDetails(map[string]any{"current": cur})
			}
			s.ID, s.Version = id, cur.Version+1
		}
		all[s.ID] = s
		deps, err := fill(ctx, tx.Conn, sch, all, s)
		if err != nil {
			return err
		}
		if err := save(ctx, tx, s); err != nil {
			return err
		}
		if err := writeDeps(ctx, tx, s.ID, deps); err != nil {
			return err
		}
		if err := recompileDependents(ctx, tx, sch, all, s.ID); err != nil {
			return err
		}
		out = *s
		return nil
	})
	if err != nil {
		return Saved{}, err
	}
	if svc.Events != nil {
		svc.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "query", "id": out.ID, "version": out.Version})
	}
	return out, nil
}

// Delete removes a query unless other queries use it.
func (svc *Service) Delete(ctx context.Context, l *workspace.Lease, id string, expectedVersion int64) error {
	err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		var ver int64
		if err := tx.QueryRow(ctx, `SELECT version FROM _sys_queries WHERE id = ?`, id).Scan(&ver); sqlitex.IsNoRows(err) {
			return fault.E(fault.NotFound)
		} else if err != nil {
			return err
		}
		if ver != expectedVersion {
			return fault.E(fault.ConcurrentModification)
		}
		var users []string
		rows, err := tx.Query(ctx, `SELECT q.name FROM _sys_dependencies d JOIN _sys_queries q ON q.id = d.dependent_id
			WHERE d.dependent_kind = 'query' AND d.target_kind = 'query' AND d.target_id = ? ORDER BY q.name`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			users = append(users, n)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(users) > 0 {
			return fault.Newf(fault.LinkedRecords, "Queries %s use this query. Change them first. Nothing was deleted.", strings.Join(users, ", "))
		}
		for _, q := range []string{`DELETE FROM _sys_dependencies WHERE dependent_kind = 'query' AND dependent_id = ?`, `DELETE FROM _sys_queries WHERE id = ?`} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && svc.Events != nil {
		svc.Events.Publish(string(l.ID()), events.DesignChanged, map[string]any{"kind": "query", "id": id, "deleted": true})
	}
	return err
}

// CompileModel compiles an unsaved model (designer SQL tab, ad-hoc runs).
func (svc *Service) CompileModel(ctx context.Context, l *workspace.Lease, m Model) (Compiled, error) {
	sch, err := svc.Schemas.Get(ctx, l)
	if err != nil {
		return Compiled{}, err
	}
	var out Compiled
	err = l.ReadConn(ctx, func(c *sqlitex.Conn) error {
		all, err := loadAll(ctx, c)
		if err != nil {
			return err
		}
		r := &resolver{sch: sch, all: all}
		out, err = Compile(sch, m, r.resolve)
		return err
	})
	return out, err
}

// RunSaved runs a saved query with parameters under policy p.
func (svc *Service) RunSaved(ctx context.Context, l *workspace.Lease, p Policy, id string, params Params, limit int) (Result, error) {
	s, err := svc.Get(ctx, l, id)
	if err != nil {
		return Result{}, err
	}
	if err := CheckParams(s.Parameters, params); err != nil {
		return Result{}, err
	}
	return svc.runSQL(ctx, l, p, s.SQL, s.Visual, params, limit)
}

// Run executes SQL (the console) with column types resolved from the catalog.
func (svc *Service) Run(ctx context.Context, l *workspace.Lease, p Policy, sql string, params Params, limit int) (Result, error) {
	return svc.runSQL(ctx, l, p, sql, nil, params, limit)
}

// RunModel compiles and runs an unsaved visual model.
func (svc *Service) RunModel(ctx context.Context, l *workspace.Lease, p Policy, m Model, params Params, limit int) (Result, error) {
	c, err := svc.CompileModel(ctx, l, m)
	if err != nil {
		return Result{}, err
	}
	if err := CheckParams(m.Parameters, params); err != nil {
		return Result{}, err
	}
	sch, err := svc.Schemas.Get(ctx, l)
	if err != nil {
		return Result{}, err
	}
	return RunTyped(ctx, l, p, sch, c.Types, c.SQL, params, limit)
}

func (svc *Service) runSQL(ctx context.Context, l *workspace.Lease, p Policy, sql string, visual *Model, params Params, limit int) (Result, error) {
	sch, err := svc.Schemas.Get(ctx, l)
	if err != nil {
		return Result{}, err
	}
	var hints []string
	if visual != nil {
		// Run the model compiled against the current schema and compiler, not
		// the SQL stored at save time, so compiler fixes (such as scaling
		// currency parameters) reach queries saved before them.
		if c, err := svc.CompileModel(ctx, l, *visual); err == nil {
			hints = c.Types
			sql = c.SQL
		}
	}
	return RunTyped(ctx, l, p, sch, hints, sql, params, limit)
}

// Usage is one object that depends on a target.
type Usage struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Confidence string `json:"confidence" enum:"exact,heuristic"`
}

// Usages lists objects depending on (kind, id) from the _sys_dependencies cache.
func Usages(ctx context.Context, l *workspace.Lease, kind, id string) ([]Usage, error) {
	out := []Usage{}
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(ctx, `SELECT d.dependent_kind, d.dependent_id, d.confidence,
			coalesce((SELECT name FROM _sys_queries WHERE id = d.dependent_id), (SELECT name FROM _sys_forms WHERE id = d.dependent_id),
			         (SELECT name FROM _sys_reports WHERE id = d.dependent_id), (SELECT name FROM _sys_scripts WHERE id = d.dependent_id), d.dependent_id)
			FROM _sys_dependencies d WHERE d.target_kind = ? AND d.target_id = ? ORDER BY 1, 4`, kind, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u Usage
			if err := rows.Scan(&u.Kind, &u.ID, &u.Confidence, &u.Name); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}

// SchemaValidator recompiles visual queries inside a schema change (they follow
// renames by column ID) and reports queries the change would break.
func SchemaValidator(ctx context.Context, tx *sqlitex.Tx, sch *catalog.Schema) (breaking, warnings []catalog.Issue, err error) {
	all, err := loadAll(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	// Visual queries first in dependency order: repeat until stable (bounded).
	for range maxNesting + 1 {
		for _, s := range all {
			if s.Kind != "visual" {
				continue
			}
			old := s.SQL
			if _, err := fill(ctx, tx.Conn, sch, all, s); err != nil {
				continue
			}
			if s.SQL != old {
				if err := save(ctx, tx, s); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	for _, s := range all {
		if s.Kind != "visual" {
			continue
		}
		r := &resolver{sch: sch, all: all, stack: []string{s.ID}}
		if _, err := Compile(sch, *s.Visual, r.resolve); err != nil {
			breaking = append(breaking, catalog.Issue{Kind: "query_invalid", Object: "query", ID: s.ID,
				Message: fmt.Sprintf("Saved query %q would stop working: %s", s.Name, fault.Classify(err).DisplayMessage())})
		}
	}
	return breaking, nil, nil
}

// CompilerVersion changes whenever the visual query compiler starts producing
// different SQL for the same model (for example scaling currency parameters).
// 3: generated through internal/sqlgen (ADR 0013).
const CompilerVersion = "3"

// RefreshCompiled recompiles every visual query's stored SQL once per
// compiler version, so consumers of the stored SQL (reports, exports) get the
// current compiler's output for queries saved by an older server. Queries that
// no longer compile are left as they are; the designer reports them.
func RefreshCompiled(ctx context.Context, c *sqlitex.Conn, sch *catalog.Schema) error {
	var have string
	err := c.QueryRow(ctx, `SELECT value FROM _sys_meta WHERE key = 'query_compiler'`).Scan(&have)
	if err == nil && have == CompilerVersion {
		return nil
	}
	return c.Immediate(ctx, func(tx *sqlitex.Tx) error {
		all, err := loadAll(ctx, tx)
		if err != nil {
			return err
		}
		for range maxNesting + 1 { // sources before their dependents
			for _, s := range all {
				if s.Kind != "visual" {
					continue
				}
				old := s.SQL
				if _, err := fill(ctx, tx.Conn, sch, all, s); err != nil {
					continue
				}
				if s.SQL != old {
					if err := save(ctx, tx, s); err != nil {
						return err
					}
				}
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO _sys_meta(key, value) VALUES ('query_compiler', ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, CompilerVersion)
		return err
	})
}

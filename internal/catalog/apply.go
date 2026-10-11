package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Mode selects preview (always rolls back) or apply.
type Mode int

const (
	Preview Mode = iota
	Apply
)

// MaxOps bounds one schema change request.
const MaxOps = 200

// ApplyOptions modify a run.
type ApplyOptions struct {
	Force       bool   // builder acknowledged breaking issues (never overrides constraint violations)
	UserID      string // for audit and updated_by
	BaseVersion int64  // if > 0, the schema_version the client designed against (optimistic lock)
	// SkipRestorePoint is for internal callers undoing their own change (e.g. an
	// import dropping the empty table it just created). Never set from requests.
	SkipRestorePoint bool
}

// Validator inspects the changed schema inside the transaction (saved queries,
// forms, reports, Lua references) and returns breaking issues and warnings.
type Validator func(ctx context.Context, tx *sqlitex.Tx, s *Schema) (breaking, warnings []Issue, err error)

// Engine runs schema changes (D3). Hooks are optional.
type Engine struct {
	// RestorePoint takes a backup of kind restore_point before a destructive apply
	// (wired to the backup package in Phase 6).
	RestorePoint func(ctx context.Context, l *workspace.Lease) error
	Validators   []Validator
	// AfterCommit runs after a successful apply (publish schema.changed, drop caches).
	AfterCommit func(ctx context.Context, l *workspace.Lease, s *Schema)
	// fault injection for crash tests (see faultinject build tag)
	hook func(step string)
	now  func() time.Time
}

var errPreviewRollback = errors.New("catalog: preview rollback")

// Run validates and executes ops in one transaction. Preview returns the impact
// report and always rolls back; Apply commits unless blocked.
func (e *Engine) Run(ctx context.Context, l *workspace.Lease, ops []Op, mode Mode, opt ApplyOptions) (ImpactReport, error) {
	if len(ops) == 0 {
		return ImpactReport{}, fault.New(fault.BadRequest, "No changes were given.")
	}
	if len(ops) > MaxOps {
		return ImpactReport{}, fault.Newf(fault.BadRequest, "At most %d changes can be applied at once.", MaxOps)
	}
	destructive := false
	for _, op := range ops {
		destructive = destructive || op.Destructive()
	}
	if mode == Apply && destructive && e.RestorePoint != nil && !opt.SkipRestorePoint {
		if err := e.RestorePoint(ctx, l); err != nil {
			return ImpactReport{}, fmt.Errorf("restore point: %w", err)
		}
	}
	var report ImpactReport
	var final *Schema
	err := l.WriteWith(ctx, workspace.Interactive, workspace.WriteOptions{ForeignKeysOff: true, Setup: registerConvertFuncs},
		func(tx *sqlitex.Tx) error {
			report = ImpactReport{RestorePointRequired: destructive}
			s, err := Load(ctx, tx)
			if err != nil {
				return err
			}
			if opt.BaseVersion > 0 && s.Version != opt.BaseVersion {
				return fault.New(fault.ConcurrentModification, "The table design was changed by someone else since you opened it. Nothing was changed; reload and try again.").
					WithDetails(map[string]any{"schema_version": s.Version})
			}
			p := &planner{ctx: ctx, tx: tx, s: s, report: &report, now: e.clock(), user: opt.UserID,
				touched: map[string]bool{}, created: map[string]string{}}
			for i, op := range ops {
				if err := p.apply(op); err != nil {
					return opError(i, op, err)
				}
				e.step("op")
			}
			if err := p.impact(e.Validators); err != nil {
				return err
			}
			if err := p.finish(ops); err != nil {
				return err
			}
			final, err = Load(ctx, tx)
			if err != nil {
				return err
			}
			if drift, err := Reconcile(ctx, tx, final); err != nil {
				return err
			} else if len(drift) > 0 {
				return fmt.Errorf("catalog: internal drift after change: %s", strings.Join(drift, "; "))
			}
			report.SchemaVersion = final.Version
			if len(p.created) > 0 {
				report.CreatedIDs = p.created
			}
			e.step("before_commit")
			if mode == Preview {
				return errPreviewRollback
			}
			if p.violations || (len(report.Breaking) > 0 && !opt.Force) {
				rep := report
				return fault.New(fault.SchemaChangeBlocked, blockedMessage(p.violations)).WithDetails(map[string]any{"report": rep})
			}
			return nil
		})
	if errors.Is(err, errPreviewRollback) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	if e.AfterCommit != nil {
		e.AfterCommit(ctx, l, final)
	}
	return report, nil
}

func blockedMessage(violations bool) string {
	if violations {
		return "Some existing rows do not satisfy the new design. Nothing was changed. Fix the listed rows first."
	}
	return "This design change would break existing objects or data. Nothing was changed. Review the impact report and confirm to apply anyway."
}

func (e *Engine) clock() func() time.Time {
	if e.now != nil {
		return e.now
	}
	return time.Now
}

func (e *Engine) step(name string) {
	if e.hook != nil {
		e.hook(name)
	}
}

func opError(i int, op Op, err error) error {
	var fe *fault.Error
	if errors.As(err, &fe) && fe.Code == fault.BadRequest {
		d := map[string]any{"op_index": i, "op": op.Kind}
		for k, v := range fe.Details {
			d[k] = v
		}
		return &fault.Error{Code: fault.BadRequest, Message: fmt.Sprintf("Change %d (%s): %s", i+1, op.Kind, fe.DisplayMessage()), Details: d, Cause: fe.Cause}
	}
	return err
}

// ---- planner ---------------------------------------------------------------

type planner struct {
	ctx        context.Context
	tx         *sqlitex.Tx
	s          *Schema
	report     *ImpactReport
	now        func() time.Time
	user       string
	touched    map[string]bool   // table IDs whose version bumps
	created    map[string]string // client-visible map of created object IDs ("table:<name>" → id)
	violations bool
}

func badf(format string, args ...any) error { return fault.Newf(fault.BadRequest, format, args...) }

func (p *planner) exec(sql string, args ...any) error {
	_, err := p.tx.Exec(p.ctx, sql, args...)
	return err
}

func (p *planner) newID(proposed string) (string, error) {
	if proposed == "" {
		return ids.New(), nil
	}
	if !ids.Valid(proposed) {
		return "", badf("%q is not a valid identifier.", proposed)
	}
	if _, ok := p.s.Table(proposed); ok {
		return "", badf("Identifier %s is already in use.", proposed)
	}
	if _, ok := p.s.Column(proposed); ok {
		return "", badf("Identifier %s is already in use.", proposed)
	}
	if _, ok := p.s.Relationship(proposed); ok {
		return "", badf("Identifier %s is already in use.", proposed)
	}
	for _, t := range p.s.Tables {
		for _, x := range t.Indexes {
			if x.ID == proposed {
				return "", badf("Identifier %s is already in use.", proposed)
			}
		}
	}
	return proposed, nil
}

func (p *planner) table(id string) (*Table, error) {
	t, ok := p.s.Table(id)
	if !ok {
		return nil, badf("Table %s does not exist.", id)
	}
	return t, nil
}

func (p *planner) physicalNameTaken(name string) (bool, error) {
	var n int
	err := p.tx.QueryRow(p.ctx, `SELECT count(*) FROM sqlite_schema WHERE name = ? COLLATE NOCASE`, name).Scan(&n)
	return n > 0, err
}

func (p *planner) apply(op Op) error {
	switch op.Kind {
	case OpCreateTable:
		return p.createTable(op)
	case OpRenameTable:
		return p.renameTable(op)
	case OpDropTable:
		return p.dropTable(op)
	case OpAddColumn:
		return p.addColumn(op)
	case OpRenameColumn:
		return p.renameColumn(op)
	case OpAlterColumn:
		return p.alterColumn(op)
	case OpDropColumn:
		return p.dropColumn(op)
	case OpReorderColumns:
		return p.reorderColumns(op)
	case OpCreateIndex:
		return p.createIndex(op)
	case OpDropIndex:
		return p.dropIndex(op)
	case OpCreateRelationship:
		return p.createRelationship(op)
	case OpDropRelationship:
		return p.dropRelationship(op)
	case OpSetTableProps:
		return p.setTableProps(op)
	}
	return badf("Unknown change %q.", op.Kind)
}

// ---- tables ----------------------------------------------------------------

func validDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", badf("Enter a name.")
	}
	if len(name) > 200 || strings.ContainsAny(name, "\x00\n\r") {
		return "", badf("Names must be one line of at most 200 characters.")
	}
	return name, nil
}

func (p *planner) createTable(op Op) error {
	spec := op.Table
	if spec == nil {
		return badf("Missing table definition.")
	}
	display, err := validDisplayName(spec.DisplayName)
	if err != nil {
		return err
	}
	for _, t := range p.s.Tables {
		if strings.EqualFold(t.DisplayName, display) {
			return badf("A table named %q already exists.", display)
		}
	}
	id, err := p.newID(spec.ID)
	if err != nil {
		return err
	}
	phys := spec.PhysicalName
	if phys == "" {
		phys = uniqueName(DerivePhysicalName(display), func(n string) bool { taken, _ := p.physicalNameTaken(n); return taken })
	}
	if err := ValidatePhysicalName(phys); err != nil {
		return err
	}
	if taken, err := p.physicalNameTaken(phys); err != nil {
		return err
	} else if taken {
		return badf("The name %q is already used by another object.", phys)
	}
	idColID, err := p.newID(spec.IDColumnID)
	if err != nil {
		return err
	}
	t := Table{ID: id, PhysicalName: phys, DisplayName: display, Description: spec.Description, TrackHistory: spec.TrackHistory, Version: 1}
	t.Columns = append(t.Columns, Column{ID: idColID, TableID: id, PhysicalName: "id", DisplayName: "ID", Type: Autonumber, Ordinal: 0})
	p.s.Tables = append(p.s.Tables, t)
	p.s.index()
	tp, _ := p.s.Table(id)
	for i, cs := range spec.Columns {
		c, rel, err := p.buildColumn(cs, tp, i+1, nil)
		if err != nil {
			return err
		}
		tp.Columns = append(tp.Columns, c)
		if rel != nil {
			p.s.Relationships = append(p.s.Relationships, *rel)
		}
		p.s.index()
		tp, _ = p.s.Table(id)
	}
	ddl, err := createTableSQL(p.s, tp, phys)
	if err != nil {
		return err
	}
	if err := p.exec(ddl); err != nil {
		return ddlError(err)
	}
	if err := p.exec(occTriggerSQL(tp)); err != nil {
		return err
	}
	if err := p.validateTableExprs(tp); err != nil {
		return err
	}
	now := sqlitex.FormatTime(p.now())
	if err := p.exec(`INSERT INTO _sys_tables(id, physical_name, display_name, description, track_history, version, created_at, updated_at)
		VALUES (?,?,?,?,?,1,?,?)`, tp.ID, tp.PhysicalName, tp.DisplayName, tp.Description, tp.TrackHistory, now, now); err != nil {
		return err
	}
	for _, r := range p.s.Relationships {
		if r.ChildTableID == tp.ID {
			if err := p.insertRelationship(r); err != nil {
				return err
			}
		}
	}
	for _, c := range tp.Columns {
		if err := p.insertColumnMeta(c); err != nil {
			return err
		}
	}
	p.touched[tp.ID] = true
	p.created["table:"+tp.DisplayName] = tp.ID
	for _, c := range tp.Columns {
		p.created["column:"+tp.DisplayName+"."+c.DisplayName] = c.ID
	}
	return nil
}

func (p *planner) renameTable(op Op) error {
	t, err := p.table(op.TableID)
	if err != nil {
		return err
	}
	if op.DisplayName != "" {
		display, err := validDisplayName(op.DisplayName)
		if err != nil {
			return err
		}
		for _, o := range p.s.Tables {
			if o.ID != t.ID && strings.EqualFold(o.DisplayName, display) {
				return badf("A table named %q already exists.", display)
			}
		}
		t.DisplayName = display
	}
	if op.PhysicalName != "" && op.PhysicalName != t.PhysicalName {
		if err := ValidatePhysicalName(op.PhysicalName); err != nil {
			return err
		}
		if !strings.EqualFold(op.PhysicalName, t.PhysicalName) {
			if taken, err := p.physicalNameTaken(op.PhysicalName); err != nil {
				return err
			} else if taken {
				return badf("The name %q is already used by another object.", op.PhysicalName)
			}
		}
		if err := p.exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", Quote(t.PhysicalName), Quote(op.PhysicalName))); err != nil {
			return ddlError(err)
		}
		t.PhysicalName = op.PhysicalName
	}
	p.touched[t.ID] = true
	return p.exec(`UPDATE _sys_tables SET display_name = ?, physical_name = ? WHERE id = ?`, t.DisplayName, t.PhysicalName, t.ID)
}

func (p *planner) dropTable(op Op) error {
	t, err := p.table(op.TableID)
	if err != nil {
		return err
	}
	var blockers []string
	for _, r := range p.s.Relationships {
		if r.ParentTableID == t.ID && r.ChildTableID != t.ID {
			blockers = append(blockers, r.Name)
		}
	}
	if len(blockers) > 0 {
		return badf("Other tables link to this table (%s). Remove those relationships or lookup fields first.", strings.Join(blockers, ", "))
	}
	var rows int64
	if err := p.tx.QueryRow(p.ctx, "SELECT count(*) FROM "+Quote(t.PhysicalName)).Scan(&rows); err != nil {
		return err
	}
	p.report.RowsAffected += rows
	if rows > 0 {
		p.report.Warnings = append(p.report.Warnings, Issue{Kind: "data_loss", Object: "table", ID: t.ID,
			Message: fmt.Sprintf("Table %q and its %d rows will be deleted. A restore point is taken first.", t.DisplayName, rows)})
	}
	if err := p.exec("DROP TABLE " + Quote(t.PhysicalName)); err != nil {
		return ddlError(err)
	}
	// foreign_keys is OFF during schema changes, so _sys_* cascades do not fire:
	// delete dependent metadata explicitly.
	for _, q := range []string{
		`DELETE FROM _sys_columns WHERE table_id = ?`,
		`DELETE FROM _sys_indexes WHERE table_id = ?`,
		`DELETE FROM _sys_relationships WHERE child_table_id = ? OR parent_table_id = ?`,
		`DELETE FROM _sys_tables WHERE id = ?`,
	} {
		args := []any{t.ID}
		if strings.Count(q, "?") == 2 {
			args = append(args, t.ID)
		}
		if err := p.exec(q, args...); err != nil {
			return err
		}
	}
	if err := p.exec(`DELETE FROM _sys_row_history WHERE table_id = ?`, t.ID); err != nil {
		return err
	}
	// Lookup display columns elsewhere cannot point here (blocked above), so only
	// in-memory cleanup remains.
	kept := p.s.Tables[:0]
	for _, x := range p.s.Tables {
		if x.ID != t.ID {
			kept = append(kept, x)
		}
	}
	p.s.Tables = kept
	rels := p.s.Relationships[:0]
	for _, r := range p.s.Relationships {
		if r.ChildTableID != t.ID && r.ParentTableID != t.ID {
			rels = append(rels, r)
		}
	}
	p.s.Relationships = rels
	p.s.index()
	delete(p.touched, t.ID)
	return nil
}

func (p *planner) setTableProps(op Op) error {
	t, err := p.table(op.TableID)
	if err != nil {
		return err
	}
	if op.Props == nil {
		return badf("Missing table properties.")
	}
	if op.Props.DisplayName != nil {
		if err := p.renameTable(Op{TableID: t.ID, DisplayName: *op.Props.DisplayName}); err != nil {
			return err
		}
	}
	if op.Props.Description != nil {
		if len(*op.Props.Description) > 4000 {
			return badf("Descriptions are limited to 4000 characters.")
		}
		t.Description = *op.Props.Description
	}
	if op.Props.TrackHistory != nil {
		t.TrackHistory = *op.Props.TrackHistory
	}
	p.touched[t.ID] = true
	return p.exec(`UPDATE _sys_tables SET description = ?, track_history = ? WHERE id = ?`, t.Description, t.TrackHistory, t.ID)
}

// ---- columns ---------------------------------------------------------------

// buildColumn validates spec for table t and returns the column (and the
// relationship it creates, for lookups). existing is the column being altered.
func (p *planner) buildColumn(spec ColumnSpec, t *Table, ordinal int, existing *Column) (Column, *Relationship, error) {
	display, err := validDisplayName(spec.DisplayName)
	if err != nil {
		return Column{}, nil, err
	}
	c := Column{TableID: t.ID, DisplayName: display, Type: spec.Type, Options: spec.Options, Required: spec.Required,
		DefaultSQL: strings.TrimSpace(spec.DefaultSQL), ValidationSQL: strings.TrimSpace(spec.ValidationSQL),
		ValidationText: strings.TrimSpace(spec.ValidationText), Description: spec.Description, Ordinal: ordinal, Hidden: spec.Hidden}
	if existing != nil {
		c.ID = existing.ID
		c.LookupRelationshipID = existing.LookupRelationshipID
	} else if c.ID, err = p.newID(spec.ID); err != nil {
		return Column{}, nil, err
	}
	for _, o := range t.Columns {
		if o.ID != c.ID && strings.EqualFold(o.DisplayName, display) {
			return Column{}, nil, badf("A field named %q already exists in this table.", display)
		}
	}
	c.PhysicalName = spec.PhysicalName
	if c.PhysicalName == "" {
		if existing != nil {
			c.PhysicalName = existing.PhysicalName
		} else {
			c.PhysicalName = uniqueName(DerivePhysicalName(display), func(n string) bool { _, ok := t.ColumnByPhysical(n); return ok })
		}
	}
	if err := ValidateColumnName(c.PhysicalName); err != nil {
		return Column{}, nil, err
	}
	if o, ok := t.ColumnByPhysical(c.PhysicalName); ok && o.ID != c.ID {
		return Column{}, nil, badf("The field name %q is already used in this table.", c.PhysicalName)
	}
	if !c.Type.Valid() || c.Type == Autonumber {
		return Column{}, nil, badf("%q is not a field type that can be added.", c.Type)
	}
	if len(c.ValidationText) > 500 || len(c.Description) > 4000 {
		return Column{}, nil, badf("Validation text is limited to 500 characters and descriptions to 4000.")
	}
	for _, expr := range []string{c.DefaultSQL, c.ValidationSQL} {
		if expr != "" {
			if err := lexCheck(expr); err != nil {
				return Column{}, nil, err
			}
		}
	}
	switch c.Type {
	case ShortText:
		if c.Options.MaxLength < 0 || c.Options.MaxLength > 65535 {
			return Column{}, nil, badf("Field size must be between 1 and 65535.")
		}
		if c.Options.MaxLength == 0 {
			c.Options.MaxLength = DefaultMaxLength
		}
	case Calculated:
		if c.Options.Expression == "" {
			return Column{}, nil, badf("A calculated field needs an expression.")
		}
		if err := lexCheck(c.Options.Expression); err != nil {
			return Column{}, nil, err
		}
		switch c.Options.ResultType {
		case "", "text", "integer", "number", "currency", "yes_no", "date", "datetime":
		default:
			return Column{}, nil, badf("Unknown result type %q.", c.Options.ResultType)
		}
		if c.Required || c.DefaultSQL != "" || c.ValidationSQL != "" {
			return Column{}, nil, badf("Calculated fields cannot be required or have defaults or validation rules.")
		}
	}
	if c.Type != ShortText {
		c.Options.MaxLength = 0
	}
	if c.Type != Calculated {
		c.Options.Expression, c.Options.ResultType = "", ""
	}
	var rel *Relationship
	if c.Type == Lookup {
		if spec.Lookup == nil {
			return Column{}, nil, badf("A lookup field needs a related table.")
		}
		parent, err := p.table(spec.Lookup.ParentTableID)
		if err != nil {
			return Column{}, nil, err
		}
		if parent.ID == t.ID && existing == nil && len(t.Columns) == 0 {
			return Column{}, nil, badf("Invalid lookup.")
		}
		disp := spec.Lookup.DisplayColumnID
		if disp == "" {
			disp = defaultDisplayColumn(parent)
		} else if _, ok := parent.Column(disp); !ok {
			return Column{}, nil, badf("The display field is not in table %q.", parent.DisplayName)
		}
		c.LookupDisplayColumnID = disp
		onDelete := spec.Lookup.OnDelete
		if onDelete == "" {
			onDelete = Restrict
		}
		if onDelete != Restrict && onDelete != Cascade && onDelete != SetNull {
			return Column{}, nil, badf("Unknown delete rule %q.", onDelete)
		}
		if onDelete == SetNull && c.Required {
			return Column{}, nil, badf("A required lookup cannot use the 'set null' delete rule.")
		}
		enforce := spec.Lookup.Enforce == nil || *spec.Lookup.Enforce
		if existing != nil && existing.Type == Lookup && existing.LookupRelationshipID != "" {
			r, _ := p.s.Relationship(existing.LookupRelationshipID)
			if r != nil && r.ParentTableID == parent.ID {
				rr := *r
				rr.OnDelete, rr.Enforce = onDelete, enforce
				rel = &rr
				return c, rel, nil
			}
		}
		relID, err := p.newID(spec.Lookup.RelationshipID)
		if err != nil {
			return Column{}, nil, err
		}
		name := spec.Lookup.RelationshipName
		if name == "" {
			name = parent.DisplayName + " → " + t.DisplayName + "." + display
		}
		for _, r := range p.s.Relationships {
			if strings.EqualFold(r.Name, name) {
				name += " (" + relID[len(relID)-4:] + ")"
				break
			}
		}
		rel = &Relationship{ID: relID, Name: name, ParentTableID: parent.ID, ChildTableID: t.ID, ChildColumnID: c.ID, OnDelete: onDelete, Enforce: enforce}
		c.LookupRelationshipID = relID
	} else {
		c.LookupRelationshipID, c.LookupDisplayColumnID = "", ""
	}
	return c, rel, nil
}

func defaultDisplayColumn(t *Table) string {
	for _, c := range t.Columns {
		if c.Type == ShortText {
			return c.ID
		}
	}
	return t.Columns[0].ID
}

// validateTableExprs runs the semantic expression checks against the live table.
func (p *planner) validateTableExprs(t *Table) error {
	for _, c := range t.Columns {
		checks := []struct {
			kind ExprKind
			expr string
		}{{ExprValidation, c.ValidationSQL}, {ExprDefault, c.DefaultSQL}, {ExprCalculated, c.Options.Expression}}
		for _, ch := range checks {
			if ch.expr == "" {
				continue
			}
			if err := ValidateExpr(p.ctx, p.tx.Conn, ch.kind, t.PhysicalName, ch.expr); err != nil {
				return badf("Field %q: %s", c.DisplayName, fault.Classify(err).DisplayMessage())
			}
		}
	}
	return nil
}

func (p *planner) addColumn(op Op) error {
	t, err := p.table(op.TableID)
	if err != nil {
		return err
	}
	if op.Column == nil {
		return badf("Missing field definition.")
	}
	before := cloneTable(t)
	c, rel, err := p.buildColumn(*op.Column, t, nextOrdinal(t), nil)
	if err != nil {
		return err
	}
	t.Columns = append(t.Columns, c)
	if rel != nil {
		p.s.Relationships = append(p.s.Relationships, *rel)
	}
	p.s.index()
	t, _ = p.s.Table(t.ID)
	native := c.ValidationSQL == "" && (!c.Required || c.DefaultSQL != "") && (c.Type != Lookup || rel == nil || !rel.Enforce)
	if native {
		if err := p.exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", Quote(t.PhysicalName), columnDef(c))); err != nil {
			return ddlError(err)
		}
	} else if err := p.rebuild(before, t, nil); err != nil {
		return err
	}
	if err := p.validateTableExprs(t); err != nil {
		return err
	}
	if rel != nil {
		if err := p.insertRelationship(*rel); err != nil {
			return err
		}
	}
	if err := p.insertColumnMeta(c); err != nil {
		return err
	}
	p.touched[t.ID] = true
	p.created["column:"+t.DisplayName+"."+c.DisplayName] = c.ID
	return nil
}

func nextOrdinal(t *Table) int {
	n := 0
	for _, c := range t.Columns {
		n = max(n, c.Ordinal+1)
	}
	return n
}

func (p *planner) column(tableID, colID string) (*Table, *Column, error) {
	t, err := p.table(tableID)
	if err != nil {
		return nil, nil, err
	}
	c, ok := t.Column(colID)
	if !ok {
		return nil, nil, badf("Field %s does not exist in table %q.", colID, t.DisplayName)
	}
	return t, c, nil
}

func (p *planner) renameColumn(op Op) error {
	t, c, err := p.column(op.TableID, op.ColumnID)
	if err != nil {
		return err
	}
	if c.Type == Autonumber && op.PhysicalName != "" && op.PhysicalName != "id" {
		return badf("The ID field's name cannot be changed.")
	}
	if op.DisplayName != "" {
		display, err := validDisplayName(op.DisplayName)
		if err != nil {
			return err
		}
		for _, o := range t.Columns {
			if o.ID != c.ID && strings.EqualFold(o.DisplayName, display) {
				return badf("A field named %q already exists in this table.", display)
			}
		}
		c.DisplayName = display
	}
	if op.PhysicalName != "" && op.PhysicalName != c.PhysicalName {
		if err := ValidateColumnName(op.PhysicalName); err != nil {
			return err
		}
		if o, ok := t.ColumnByPhysical(op.PhysicalName); ok && o.ID != c.ID {
			return badf("The field name %q is already used in this table.", op.PhysicalName)
		}
		old := c.PhysicalName
		if err := p.exec(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", Quote(t.PhysicalName), Quote(old), Quote(op.PhysicalName))); err != nil {
			return ddlError(err)
		}
		c.PhysicalName = op.PhysicalName
		// SQLite rewrote references inside the table's DDL; keep stored expressions in step.
		for i := range t.Columns {
			x := &t.Columns[i]
			nv, nd, ne := renameIdent(x.ValidationSQL, old, c.PhysicalName), renameIdent(x.DefaultSQL, old, c.PhysicalName), renameIdent(x.Options.Expression, old, c.PhysicalName)
			if nv != x.ValidationSQL || nd != x.DefaultSQL || ne != x.Options.Expression {
				x.ValidationSQL, x.DefaultSQL, x.Options.Expression = nv, nd, ne
				if err := p.updateColumnMeta(*x); err != nil {
					return err
				}
			}
		}
	}
	p.touched[t.ID] = true
	return p.updateColumnMeta(*c)
}

// metadataOnly reports whether altering a to b changes nothing physical.
func metadataOnly(a, b Column) bool {
	return a.PhysicalName == b.PhysicalName && a.Type == b.Type && a.Required == b.Required &&
		a.DefaultSQL == b.DefaultSQL && a.ValidationSQL == b.ValidationSQL && a.Options.MaxLength == b.Options.MaxLength &&
		a.Options.Expression == b.Options.Expression && a.Options.ResultType == b.Options.ResultType &&
		a.LookupRelationshipID == b.LookupRelationshipID
}

func (p *planner) alterColumn(op Op) error {
	t, c, err := p.column(op.TableID, op.ColumnID)
	if err != nil {
		return err
	}
	if op.Column == nil {
		return badf("Missing field definition.")
	}
	if c.Type == Autonumber {
		if op.Column.DisplayName != "" && op.Column.DisplayName != c.DisplayName {
			return p.renameColumn(Op{TableID: t.ID, ColumnID: c.ID, DisplayName: op.Column.DisplayName})
		}
		return badf("The ID field cannot be changed.")
	}
	// Physical rename first (native), then the rest.
	if op.Column.PhysicalName != "" && op.Column.PhysicalName != c.PhysicalName {
		if err := p.renameColumn(Op{TableID: t.ID, ColumnID: c.ID, PhysicalName: op.Column.PhysicalName}); err != nil {
			return err
		}
		t, c, _ = p.column(t.ID, c.ID)
	}
	before := cloneTable(t)
	oldCol := *c
	nc, rel, err := p.buildColumn(*op.Column, t, c.Ordinal, c)
	if err != nil {
		return err
	}
	if oldCol.Type == Lookup && nc.Type != Lookup && oldCol.LookupRelationshipID != "" {
		// The lookup's relationship goes away with it.
		p.removeRelationship(oldCol.LookupRelationshipID)
		if err := p.exec(`DELETE FROM _sys_relationships WHERE id = ?`, oldCol.LookupRelationshipID); err != nil {
			return err
		}
	}
	*c = nc
	if rel != nil {
		p.upsertRelationship(*rel)
	}
	p.s.index()
	t, _ = p.s.Table(t.ID)
	if !metadataOnly(oldCol, nc) || (rel != nil && !relEqual(p.s, before, rel)) {
		var conv map[string]convSpec
		if oldCol.Type != nc.Type && nc.Type != Calculated && oldCol.Type != Calculated {
			conv = map[string]convSpec{nc.ID: {from: oldCol.Type, to: nc.Type}}
			if err := p.conversionFailures(before, oldCol, nc); err != nil {
				return err
			}
		}
		if err := p.rebuild(before, t, conv); err != nil {
			return err
		}
		if err := p.validateTableExprs(t); err != nil {
			return err
		}
	}
	if rel != nil {
		if err := p.insertOrUpdateRelationship(*rel); err != nil {
			return err
		}
	}
	p.touched[t.ID] = true
	return p.updateColumnMeta(nc)
}

func relEqual(s *Schema, before *Table, rel *Relationship) bool {
	_ = before
	r, ok := s.Relationship(rel.ID)
	return ok && *r == *rel
}

func (p *planner) dropColumn(op Op) error {
	t, c, err := p.column(op.TableID, op.ColumnID)
	if err != nil {
		return err
	}
	if c.Type == Autonumber {
		return badf("The ID field cannot be deleted.")
	}
	for _, o := range t.Columns {
		if o.ID == c.ID {
			continue
		}
		for _, expr := range []string{o.ValidationSQL, o.DefaultSQL, o.Options.Expression} {
			if expr != "" && referencesIdent(expr, c.PhysicalName) {
				return badf("Field %q is used by field %q. Change that field first.", c.DisplayName, o.DisplayName)
			}
		}
	}
	for _, r := range p.s.Relationships {
		if r.ParentTableID == t.ID {
			continue
		}
		for _, x := range p.s.Tables {
			for _, xc := range x.Columns {
				if xc.LookupDisplayColumnID == c.ID {
					p.report.Warnings = append(p.report.Warnings, Issue{Kind: "lookup_display", Object: "column", ID: xc.ID,
						Message: fmt.Sprintf("Lookup %q.%q showed this field; it will show the ID instead.", x.DisplayName, xc.DisplayName)})
				}
			}
		}
		break
	}
	before := cloneTable(t)
	var nonNull int64
	if err := p.tx.QueryRow(p.ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE %s IS NOT NULL", Quote(t.PhysicalName), Quote(c.PhysicalName))).Scan(&nonNull); err != nil {
		return err
	}
	if nonNull > 0 {
		p.report.Warnings = append(p.report.Warnings, Issue{Kind: "data_loss", Object: "column", ID: c.ID,
			Message: fmt.Sprintf("Field %q holds data in %d rows; it will be deleted. A restore point is taken first.", c.DisplayName, nonNull)})
	}
	if c.LookupRelationshipID != "" {
		p.removeRelationship(c.LookupRelationshipID)
	}
	for _, r := range p.s.Relationships {
		if r.ChildColumnID == c.ID {
			p.removeRelationship(r.ID)
		}
	}
	// Indexes over the column go with it.
	keptIdx := t.Indexes[:0]
	for _, x := range t.Indexes {
		uses := false
		for _, id := range x.ColumnIDs {
			uses = uses || id == c.ID
		}
		if uses {
			if err := p.exec(`DELETE FROM _sys_indexes WHERE id = ?`, x.ID); err != nil {
				return err
			}
			continue
		}
		keptIdx = append(keptIdx, x)
	}
	t.Indexes = keptIdx
	cols := t.Columns[:0]
	for _, x := range t.Columns {
		if x.ID != c.ID {
			cols = append(cols, x)
		}
	}
	t.Columns = cols
	p.s.index()
	t, _ = p.s.Table(t.ID)
	if err := p.rebuild(before, t, nil); err != nil {
		return err
	}
	for _, x := range p.s.Tables {
		for _, xc := range x.Columns {
			if xc.LookupDisplayColumnID == op.ColumnID {
				if err := p.exec(`UPDATE _sys_columns SET lookup_display_column_id = NULL WHERE id = ?`, xc.ID); err != nil {
					return err
				}
			}
		}
	}
	if err := p.exec(`DELETE FROM _sys_relationships WHERE child_column_id = ?`, op.ColumnID); err != nil {
		return err
	}
	p.touched[t.ID] = true
	return p.exec(`DELETE FROM _sys_columns WHERE id = ?`, op.ColumnID)
}

func (p *planner) reorderColumns(op Op) error {
	t, err := p.table(op.TableID)
	if err != nil {
		return err
	}
	if len(op.ColumnOrder) != len(t.Columns) {
		return badf("The new order must list every field exactly once.")
	}
	seen := map[string]bool{}
	for i, id := range op.ColumnOrder {
		c, ok := t.Column(id)
		if !ok || seen[id] {
			return badf("The new order must list every field exactly once.")
		}
		seen[id] = true
		c.Ordinal = i
		if err := p.exec(`UPDATE _sys_columns SET ordinal = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	p.touched[t.ID] = true
	return nil
}

// ---- indexes & relationships ----------------------------------------------

func (p *planner) createIndex(op Op) error {
	spec := op.Index
	if spec == nil {
		return badf("Missing index definition.")
	}
	t, err := p.table(spec.TableID)
	if err != nil {
		return err
	}
	if len(spec.ColumnIDs) == 0 || len(spec.ColumnIDs) > 16 {
		return badf("An index needs between 1 and 16 fields.")
	}
	var names []string
	seen := map[string]bool{}
	for _, id := range spec.ColumnIDs {
		c, ok := t.Column(id)
		if !ok || seen[id] {
			return badf("Index fields must be distinct fields of this table.")
		}
		seen[id] = true
		names = append(names, c.PhysicalName)
	}
	id, err := p.newID(spec.ID)
	if err != nil {
		return err
	}
	phys := spec.PhysicalName
	if phys == "" {
		base := "ix_" + t.PhysicalName + "_" + strings.Join(names, "_")
		if len(base) > 63 {
			base = base[:63]
		}
		phys = uniqueName(strings.TrimRight(base, "_"), func(n string) bool { taken, _ := p.physicalNameTaken(n); return taken })
	}
	if err := ValidatePhysicalName(phys); err != nil {
		return err
	}
	if taken, err := p.physicalNameTaken(phys); err != nil {
		return err
	} else if taken {
		return badf("The name %q is already used by another object.", phys)
	}
	x := Index{ID: id, TableID: t.ID, PhysicalName: phys, Unique: spec.Unique, ColumnIDs: spec.ColumnIDs}
	ddl, err := createIndexSQL(t, x)
	if err != nil {
		return err
	}
	if err := p.exec(ddl); err != nil {
		var se *sqlite3.Error
		if errors.As(err, &se) && se.Code() == sqlite3.CONSTRAINT {
			p.violations = true
			p.report.Breaking = append(p.report.Breaking, Issue{Kind: "constraint_violation", Object: "table", ID: t.ID,
				Message: fmt.Sprintf("Table %q has duplicate values in %s, so a unique index cannot be created.", t.DisplayName, strings.Join(names, ", "))})
			return nil
		}
		return ddlError(err)
	}
	t.Indexes = append(t.Indexes, x)
	cols, _ := json.Marshal(x.ColumnIDs)
	p.touched[t.ID] = true
	p.created["index:"+x.PhysicalName] = x.ID
	return p.exec(`INSERT INTO _sys_indexes(id, table_id, physical_name, is_unique, column_ids) VALUES (?,?,?,?,?)`,
		x.ID, x.TableID, x.PhysicalName, x.Unique, string(cols))
}

func (p *planner) dropIndex(op Op) error {
	for ti := range p.s.Tables {
		t := &p.s.Tables[ti]
		for i, x := range t.Indexes {
			if x.ID != op.IndexID {
				continue
			}
			if err := p.exec("DROP INDEX " + Quote(x.PhysicalName)); err != nil {
				return ddlError(err)
			}
			t.Indexes = append(t.Indexes[:i:i], t.Indexes[i+1:]...)
			p.touched[t.ID] = true
			return p.exec(`DELETE FROM _sys_indexes WHERE id = ?`, x.ID)
		}
	}
	return badf("Index %s does not exist.", op.IndexID)
}

func (p *planner) createRelationship(op Op) error {
	spec := op.Relationship
	if spec == nil {
		return badf("Missing relationship definition.")
	}
	if _, err := p.table(spec.ParentTableID); err != nil {
		return err
	}
	child, col, err := p.column(spec.ChildTableID, spec.ChildColumnID)
	if err != nil {
		return err
	}
	if col.Type != Integer {
		return badf("The linking field must be a Number (integer) field; use a lookup field to link with a picker.")
	}
	for _, r := range p.s.Relationships {
		if r.ChildColumnID == col.ID {
			return badf("Field %q already links to another table.", col.DisplayName)
		}
	}
	name, err := validDisplayName(spec.Name)
	if err != nil {
		return err
	}
	for _, r := range p.s.Relationships {
		if strings.EqualFold(r.Name, name) {
			return badf("A relationship named %q already exists.", name)
		}
	}
	id, err := p.newID(spec.ID)
	if err != nil {
		return err
	}
	onDelete := spec.OnDelete
	if onDelete == "" {
		onDelete = Restrict
	}
	if onDelete != Restrict && onDelete != Cascade && onDelete != SetNull {
		return badf("Unknown delete rule %q.", onDelete)
	}
	if onDelete == SetNull && col.Required {
		return badf("A required field cannot use the 'set null' delete rule.")
	}
	r := Relationship{ID: id, Name: name, ParentTableID: spec.ParentTableID, ChildTableID: child.ID, ChildColumnID: col.ID,
		OnDelete: onDelete, Enforce: spec.Enforce == nil || *spec.Enforce}
	before := cloneTable(child)
	p.s.Relationships = append(p.s.Relationships, r)
	p.s.index()
	if r.Enforce {
		child, _ = p.s.Table(child.ID)
		if err := p.rebuild(before, child, nil); err != nil {
			return err
		}
	}
	p.touched[child.ID] = true
	p.created["relationship:"+name] = id
	return p.insertRelationship(r)
}

func (p *planner) dropRelationship(op Op) error {
	r, ok := p.s.Relationship(op.RelationshipID)
	if !ok {
		return badf("Relationship %s does not exist.", op.RelationshipID)
	}
	child, err := p.table(r.ChildTableID)
	if err != nil {
		return err
	}
	if c, ok := child.Column(r.ChildColumnID); ok && c.Type == Lookup {
		return badf("Relationship %q belongs to lookup field %q. Change or delete that field instead.", r.Name, c.DisplayName)
	}
	enforced := r.Enforce
	before := cloneTable(child)
	p.removeRelationship(r.ID)
	if enforced {
		child, _ = p.s.Table(before.ID)
		if err := p.rebuild(before, child, nil); err != nil {
			return err
		}
	}
	p.touched[child.ID] = true
	return p.exec(`DELETE FROM _sys_relationships WHERE id = ?`, op.RelationshipID)
}

func (p *planner) removeRelationship(id string) {
	kept := p.s.Relationships[:0]
	for _, r := range p.s.Relationships {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	p.s.Relationships = kept
	p.s.index()
}

func (p *planner) upsertRelationship(r Relationship) {
	for i := range p.s.Relationships {
		if p.s.Relationships[i].ID == r.ID {
			p.s.Relationships[i] = r
			p.s.index()
			return
		}
	}
	p.s.Relationships = append(p.s.Relationships, r)
	p.s.index()
}

// ---- rebuild (sqlite.org/lang_altertable.html#otheralter) ------------------

type convSpec struct{ from, to FieldType }

const rebuildName = "_abidata_rebuild"

// rebuild recreates table `after` (already updated in p.s) from its previous
// shape `before`, copying data column-by-ID with optional type conversions.
// Rows the new constraints reject are counted as violations (blocking).
func (p *planner) rebuild(before, after *Table, conv map[string]convSpec) error {
	ddl, err := createTableSQL(p.s, after, rebuildName)
	if err != nil {
		return err
	}
	if err := p.exec(ddl); err != nil {
		return ddlError(err)
	}
	dst := []string{`"id"`, `"_version"`}
	src := []string{`"id"`, `"_version"`}
	for _, c := range after.Columns {
		if c.Type == Autonumber || c.Type == Calculated {
			continue
		}
		old, ok := before.Column(c.ID)
		if !ok || old.Type == Calculated {
			continue // new column: DEFAULT applies
		}
		dst = append(dst, Quote(c.PhysicalName))
		if cv, ok := conv[c.ID]; ok {
			src = append(src, fmt.Sprintf("abidata_convert('%s', '%s', %s)", cv.from, cv.to, Quote(old.PhysicalName)))
		} else {
			src = append(src, Quote(old.PhysicalName))
		}
	}
	var total int64
	if err := p.tx.QueryRow(p.ctx, "SELECT count(*) FROM "+Quote(before.PhysicalName)).Scan(&total); err != nil {
		return err
	}
	res, err := p.tx.Exec(p.ctx, fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) SELECT %s FROM %s",
		Quote(rebuildName), strings.Join(dst, ", "), strings.Join(src, ", "), Quote(before.PhysicalName)))
	if err != nil {
		return ddlError(err)
	}
	p.report.RowsAffected += res.Changes
	if res.Changes < total {
		p.violations = true
		var samples []Sample
		rows, err := p.tx.Query(p.ctx, fmt.Sprintf(`SELECT "id" FROM %s WHERE "id" NOT IN (SELECT "id" FROM %s) ORDER BY "id" LIMIT 20`,
			Quote(before.PhysicalName), Quote(rebuildName)))
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			samples = append(samples, Sample{RowID: id})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		ids := make([]string, 0, len(samples))
		for _, s := range samples {
			ids = append(ids, strconv.FormatInt(s.RowID, 10))
		}
		p.report.Breaking = append(p.report.Breaking, Issue{Kind: "constraint_violation", Object: "table", ID: after.ID,
			Message: fmt.Sprintf("%d rows in %q break a required, size, validation or uniqueness rule (row IDs: %s).",
				total-res.Changes, after.DisplayName, strings.Join(ids, ", "))})
	}
	var seq *int64
	if err := p.tx.QueryRow(p.ctx, `SELECT seq FROM sqlite_sequence WHERE name = ?`, before.PhysicalName).Scan(&seq); err != nil && !sqlitex.IsNoRows(err) {
		return err
	}
	if err := p.exec("DROP TABLE " + Quote(before.PhysicalName)); err != nil {
		return ddlError(err)
	}
	if err := p.exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", Quote(rebuildName), Quote(after.PhysicalName))); err != nil {
		return ddlError(err)
	}
	if seq != nil {
		// Keep AUTOINCREMENT from reusing IDs of rows deleted before the rebuild.
		if err := p.exec(`UPDATE sqlite_sequence SET seq = max(seq, ?) WHERE name = ?`, *seq, after.PhysicalName); err != nil {
			return err
		}
		if err := p.exec(`INSERT INTO sqlite_sequence(name, seq) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = ?)`,
			after.PhysicalName, *seq, after.PhysicalName); err != nil {
			return err
		}
	}
	for _, x := range after.Indexes {
		ddl, err := createIndexSQL(after, x)
		if err != nil {
			return err
		}
		if err := p.exec(ddl); err != nil {
			var se *sqlite3.Error
			if errors.As(err, &se) && se.Code() == sqlite3.CONSTRAINT {
				p.violations = true
				p.report.Breaking = append(p.report.Breaking, Issue{Kind: "constraint_violation", Object: "table", ID: after.ID,
					Message: fmt.Sprintf("Unique index %q no longer holds after this change.", x.PhysicalName)})
				continue
			}
			return ddlError(err)
		}
	}
	return p.exec(occTriggerSQL(after))
}

// conversionFailures reports rows whose values cannot convert from old to nc.
func (p *planner) conversionFailures(t *Table, old, nc Column) error {
	q := Quote(old.PhysicalName)
	where := fmt.Sprintf("%s IS NOT NULL AND NOT abidata_convert_ok('%s', '%s', %s)", q, old.Type, nc.Type, q)
	var n int64
	if err := p.tx.QueryRow(p.ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", Quote(t.PhysicalName), where)).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	cf := ConversionFailure{ColumnID: nc.ID, Column: nc.DisplayName, From: string(old.Type), To: string(nc.Type), Count: n}
	rows, err := p.tx.Query(p.ctx, fmt.Sprintf(`SELECT "id", %s FROM %s WHERE %s ORDER BY "id" LIMIT 20`, q, Quote(t.PhysicalName), where))
	if err != nil {
		return err
	}
	for rows.Next() {
		var s Sample
		if err := rows.Scan(&s.RowID, &s.Value); err != nil {
			rows.Close()
			return err
		}
		cf.Samples = append(cf.Samples, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	p.report.ConversionFailures = append(p.report.ConversionFailures, cf)
	becomes := "empty"
	if nc.Type == YesNo {
		becomes = "No"
	}
	p.report.Breaking = append(p.report.Breaking, Issue{Kind: "conversion_failure", Object: "column", ID: nc.ID,
		Message: fmt.Sprintf("%d values in %q cannot be converted from %s to %s; they would become %s.", n, nc.DisplayName, old.Type, nc.Type, becomes)})
	return nil
}

// registerConvertFuncs installs DIRECTONLY helpers used by rebuild copies. They
// cannot appear in schema objects (triggers, CHECKs, generated columns), so
// workspace files stay portable (D7).
func registerConvertFuncs(c *sqlitex.Conn) (func(), error) {
	raw := c.Raw()
	const flags = sqlite3.DETERMINISTIC | sqlite3.DIRECTONLY
	argVal := func(v sqlite3.Value) any {
		switch v.Type() {
		case sqlite3.INTEGER:
			return v.Int64()
		case sqlite3.FLOAT:
			return v.Float()
		case sqlite3.TEXT:
			return v.Text()
		case sqlite3.BLOB:
			return v.Blob(nil)
		}
		return nil
	}
	result := func(ctx sqlite3.Context, v any) {
		switch x := v.(type) {
		case nil:
			ctx.ResultNull()
		case int64:
			ctx.ResultInt64(x)
		case float64:
			ctx.ResultFloat(x)
		case string:
			ctx.ResultText(x)
		case []byte:
			ctx.ResultBlob(x)
		default:
			ctx.ResultNull()
		}
	}
	err := raw.CreateFunction("abidata_convert", 3, flags, func(ctx sqlite3.Context, args ...sqlite3.Value) {
		to := FieldType(args[1].Text())
		out, ok := ConvertValue(FieldType(args[0].Text()), to, argVal(args[2]))
		if !ok {
			if to == YesNo {
				ctx.ResultInt64(0) // yes_no is never empty: what cannot convert becomes "No"
			} else {
				ctx.ResultNull()
			}
			return
		}
		result(ctx, out)
	})
	if err != nil {
		return nil, err
	}
	err = raw.CreateFunction("abidata_convert_ok", 3, flags, func(ctx sqlite3.Context, args ...sqlite3.Value) {
		_, ok := ConvertValue(FieldType(args[0].Text()), FieldType(args[1].Text()), argVal(args[2]))
		ctx.ResultBool(ok)
	})
	if err != nil {
		return nil, err
	}
	return func() {
		_ = raw.CreateFunction("abidata_convert", 3, 0, nil)
		_ = raw.CreateFunction("abidata_convert_ok", 3, 0, nil)
	}, nil
}

// ---- impact & finish -------------------------------------------------------

func (p *planner) impact(validators []Validator) error {
	rows, err := p.tx.Query(p.ctx, `SELECT "table", parent, count(*) FROM pragma_foreign_key_check GROUP BY 1, 2`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var child, parent string
		var n int64
		if err := rows.Scan(&child, &parent, &n); err != nil {
			rows.Close()
			return err
		}
		if strings.HasPrefix(child, "_sys_") {
			return fmt.Errorf("catalog: internal metadata inconsistency: %d rows in %s refer to missing %s rows", n, child, parent)
		}
		cd, pd := child, parent
		if t, ok := p.s.TableByPhysical(child); ok {
			cd = t.DisplayName
		}
		if t, ok := p.s.TableByPhysical(parent); ok {
			pd = t.DisplayName
		}
		p.violations = true
		p.report.Breaking = append(p.report.Breaking, Issue{Kind: "fk_violation", Object: "table",
			Message: fmt.Sprintf("%d rows in %q refer to %q rows that do not exist.", n, cd, pd)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Validators run first: they may recompile derived SQL (visual queries) in
	// this transaction before every saved query is re-prepared.
	for _, v := range validators {
		b, w, err := v(p.ctx, p.tx, p.s)
		if err != nil {
			return err
		}
		p.report.Breaking = append(p.report.Breaking, b...)
		p.report.Warnings = append(p.report.Warnings, w...)
	}
	return p.checkSavedQueries()
}

func (p *planner) checkSavedQueries() error {
	rows, err := p.tx.Query(p.ctx, `SELECT id, name, sql_text FROM _sys_queries ORDER BY name`)
	if err != nil {
		return err
	}
	type q struct{ id, name, sql string }
	var qs []q
	for rows.Next() {
		var x q
		if err := rows.Scan(&x.id, &x.name, &x.sql); err != nil {
			rows.Close()
			return err
		}
		qs = append(qs, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	flagged := map[string]bool{}
	for _, b := range p.report.Breaking {
		if b.Object == "query" {
			flagged[b.ID] = true
		}
	}
	for _, x := range qs {
		if flagged[x.id] {
			continue
		}
		if _, err := p.tx.Prepare(p.ctx, x.sql); err != nil {
			p.report.Breaking = append(p.report.Breaking, Issue{Kind: "query_invalid", Object: "query", ID: x.id,
				Message: fmt.Sprintf("Saved query %q would stop working: %s", x.name, sqliteMessage(err))})
		}
	}
	return nil
}

func (p *planner) finish(ops []Op) error {
	now := sqlitex.FormatTime(p.now())
	for id := range p.touched {
		if err := p.exec(`UPDATE _sys_tables SET version = version + 1, updated_at = ? WHERE id = ?`, now, id); err != nil {
			return err
		}
	}
	if err := p.exec(`UPDATE _sys_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'schema_version'`); err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"ops": ops})
	if err != nil {
		return err
	}
	var user any
	if p.user != "" {
		user = p.user
	}
	return p.exec(`INSERT INTO _sys_audit(at, user_id, action, object_kind, object_id, details) VALUES (?, ?, 'schema.apply', 'schema', NULL, ?)`,
		now, user, string(details))
}

// ---- metadata rows ---------------------------------------------------------

func (p *planner) insertColumnMeta(c Column) error {
	opts, err := json.Marshal(c.Options)
	if err != nil {
		return err
	}
	return p.exec(`INSERT INTO _sys_columns(id, table_id, physical_name, display_name, field_type, type_options, is_required,
		default_sql, validation_sql, validation_text, lookup_relationship_id, lookup_display_column_id, description, ordinal, is_hidden)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.TableID, c.PhysicalName, c.DisplayName, string(c.Type), string(opts), c.Required,
		nullable(c.DefaultSQL), nullable(c.ValidationSQL), nullable(c.ValidationText), nullable(c.LookupRelationshipID),
		nullable(c.LookupDisplayColumnID), c.Description, c.Ordinal, c.Hidden)
}

func (p *planner) updateColumnMeta(c Column) error {
	opts, err := json.Marshal(c.Options)
	if err != nil {
		return err
	}
	return p.exec(`UPDATE _sys_columns SET physical_name = ?, display_name = ?, field_type = ?, type_options = ?, is_required = ?,
		default_sql = ?, validation_sql = ?, validation_text = ?, lookup_relationship_id = ?, lookup_display_column_id = ?,
		description = ?, ordinal = ?, is_hidden = ? WHERE id = ?`,
		c.PhysicalName, c.DisplayName, string(c.Type), string(opts), c.Required, nullable(c.DefaultSQL), nullable(c.ValidationSQL),
		nullable(c.ValidationText), nullable(c.LookupRelationshipID), nullable(c.LookupDisplayColumnID), c.Description, c.Ordinal, c.Hidden, c.ID)
}

func (p *planner) insertRelationship(r Relationship) error {
	return p.exec(`INSERT INTO _sys_relationships(id, name, parent_table_id, child_table_id, child_column_id, on_delete, enforce)
		VALUES (?,?,?,?,?,?,?)`, r.ID, r.Name, r.ParentTableID, r.ChildTableID, r.ChildColumnID, string(r.OnDelete), r.Enforce)
}

func (p *planner) insertOrUpdateRelationship(r Relationship) error {
	return p.exec(`INSERT INTO _sys_relationships(id, name, parent_table_id, child_table_id, child_column_id, on_delete, enforce)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET on_delete = excluded.on_delete, enforce = excluded.enforce`,
		r.ID, r.Name, r.ParentTableID, r.ChildTableID, r.ChildColumnID, string(r.OnDelete), r.Enforce)
}

func cloneTable(t *Table) *Table {
	c := *t
	c.Columns = append([]Column(nil), t.Columns...)
	c.Indexes = append([]Index(nil), t.Indexes...)
	return &c
}

// ddlError converts SQLite errors from DDL on builder input into BAD_REQUEST
// with SQLite's message (builders see technical detail).
func ddlError(err error) error {
	var se *sqlite3.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.ERROR, sqlite3.CONSTRAINT:
			return &fault.Error{Code: fault.BadRequest, Message: "The database rejected this design: " + sqliteMessage(err), Cause: err}
		}
	}
	return err
}

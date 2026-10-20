// Package demo builds the two sample workspaces. "Northwind Traders" is a
// commercial order-processing and inventory sample with realistic rows, saved
// queries, a form, a report, a job script and a scheduled job. "Biosecurity
// Watch" is a regional biosecurity and invasive species monitoring sample that
// shows geographic fields and file attachments: suppliers and farms with a map
// location, species, pest sightings with GPS positions and photos, quarantine
// zones (polygons, one with a hole), spatial queries, a sighting form with a map,
// and a report with maps and images. Both drive the same services the HTTP
// handlers use, so validation, audit and row history behave exactly as they do
// for a user.
//
// Content is deterministic (fixed random seed); dates are relative to the
// install time so the data always looks current. No script in the demo uses the
// network.
package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/files"
	"abidata/internal/forms"
	"abidata/internal/ids"
	"abidata/internal/jobs"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/reports"
	"abidata/internal/script"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
)

// Kind is one of the sample workspaces.
type Kind string

const (
	// Traders is the order-processing sample: customers, orders, products.
	Traders Kind = "traders"
	// Invasives is the biosecurity sample: maps, areas and photos.
	Invasives Kind = "invasives"
)

// Kinds lists every sample workspace in the order they are created.
var Kinds = []Kind{Traders, Invasives}

// Workspace names. Create does nothing when a workspace with the kind's name
// already exists, and existing workspaces are never modified.
const (
	WorkspaceName = "Northwind Traders"
	InvasivesName = "Biosecurity Watch"
)

// ParseKind accepts "traders" or "invasives" (any case).
func ParseKind(s string) (Kind, error) {
	k := Kind(strings.ToLower(strings.TrimSpace(s)))
	if k != Traders && k != Invasives {
		return "", fmt.Errorf("unknown demo %q: use traders or invasives", s)
	}
	return k, nil
}

// Name is the workspace name of the kind's shared sample.
func (k Kind) Name() string {
	if k == Invasives {
		return InvasivesName
	}
	return WorkspaceName
}

// Builder holds the services the demo is built with. Every field is required.
type Builder struct {
	Admin      *admin.Store
	Workspaces *workspace.Manager
	Schemas    *catalog.Cache
	Catalog    *catalog.Engine
	Records    *records.Service
	Queries    *query.Service
	Scripts    *scripts.Service
	Forms      *forms.Service
	Reports    *reports.Service
	Jobs       *jobs.Service
	// Files stores the sample photos (ADR 0011). When nil the biosecurity
	// sightings are created without photos.
	Files *files.Service
	Log   *slog.Logger
	// Now overrides the clock (tests). Zero means time.Now.
	Now func() time.Time
}

// NewBuilder wires a Builder around the server's admin store and workspace
// manager. It keeps its own schema cache and services (a cache miss simply
// reloads from the workspace file, so sharing the server's cache is not
// needed). sched may be nil (the `abidata demo` command): the job index is then
// rebuilt when the server next opens the workspace.
func NewBuilder(adm *admin.Store, wm *workspace.Manager, sched *jobs.Scheduler, fs *files.Service, log *slog.Logger) *Builder {
	schemas := catalog.NewCache()
	rec := &records.Service{Schemas: schemas}
	qs := &query.Service{Schemas: schemas}
	ss := &scripts.Service{}
	runner := script.NewRunner(2, 2)
	return &Builder{
		Admin: adm, Workspaces: wm, Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Files: fs, Log: log,
		Catalog: &catalog.Engine{
			Validators:  []catalog.Validator{query.SchemaValidator, forms.SchemaValidator, reports.SchemaValidator},
			AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) { schemas.Put(l.ID(), s) },
		},
		Forms:   &forms.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Reports: &reports.Service{Schemas: schemas, Records: rec, Queries: qs, Scripts: ss, Runner: runner},
		Jobs:    &jobs.Service{Scripts: ss, Scheduler: sched},
	}
}

// Result describes what Create did.
type Result struct {
	WorkspaceID ids.WorkspaceID
	Created     bool // false: a workspace with the demo's name already existed
}

// Create builds the kind's sample workspace owned by ownerID. When a workspace
// with the kind's name already exists it returns that workspace and Created is
// false. If building fails part-way the half-built workspace is retired
// (marked deleted and renamed) so a retry starts clean.
func (b *Builder) Create(ctx context.Context, kind Kind, ownerID string) (Result, error) {
	if _, err := ParseKind(string(kind)); err != nil {
		return Result{}, err
	}
	existing, err := b.Admin.ListWorkspaces(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, w := range existing {
		if strings.EqualFold(w.Name, kind.Name()) {
			return Result{WorkspaceID: w.ID}, nil
		}
	}
	return b.createNamed(ctx, kind, ownerID, kind.Name())
}

// createNamed builds a sample workspace of the kind called name, owned by ownerID.
func (b *Builder) createNamed(ctx context.Context, kind Kind, ownerID, name string) (Result, error) {
	now := time.Now()
	if b.Now != nil {
		now = b.Now()
	}
	ctx = context.WithoutCancel(ctx)
	w := admin.Workspace{ID: ids.NewWorkspaceID(), Name: name, Status: admin.StatusActive, CreatedAt: now, CreatedBy: ownerID}
	if err := b.Workspaces.Create(ctx, w.ID); err != nil {
		return Result{}, err
	}
	if err := b.Admin.CreateWorkspace(ctx, w, ownerID); err != nil {
		if p, perr := b.Workspaces.PathsFor(w.ID); perr == nil {
			removeFiles(p.DB, p.WAL, p.SHM)
		}
		return Result{}, err
	}
	if err := b.build(ctx, kind, w.ID, ownerID, now); err != nil {
		b.retire(ctx, w.ID, name)
		return Result{}, fmt.Errorf("build demo workspace: %w", err)
	}
	if err := b.Admin.Audit(ctx, admin.AuditEntry{At: time.Now(), UserID: ownerID, Action: "workspace.create",
		ObjectKind: "workspace", ObjectID: string(w.ID), Details: map[string]any{"name": name, "owner": ownerID, "demo": string(kind)}}); err != nil && b.Log != nil {
		b.Log.Warn("audit write failed", "err", err)
	}
	return Result{WorkspaceID: w.ID, Created: true}, nil
}

// retire hides a half-built workspace and frees its name.
func (b *Builder) retire(ctx context.Context, id ids.WorkspaceID, wsName string) {
	if b.Files != nil {
		if err := b.Files.RemoveBucket(id); err != nil && b.Log != nil {
			b.Log.Warn("remove sample photos of failed demo workspace", "workspace_id", id, "err", err)
		}
	}
	st := admin.StatusDeleted
	name := fmt.Sprintf("%s (failed %s)", wsName, id)
	if err := b.Admin.UpdateWorkspace(ctx, id, admin.WorkspacePatch{Status: &st, Name: &name}); err != nil && b.Log != nil {
		b.Log.Error("retire failed demo workspace", "workspace_id", id, "err", err)
	}
}

// tbl gives access to a created table's column IDs and physical names by
// display name.
type tbl struct{ t *catalog.Table }

func (t tbl) id() string { return t.t.ID }
func (t tbl) col(display string) string {
	for _, c := range t.t.Columns {
		if c.DisplayName == display {
			return c.ID
		}
	}
	panic("demo: no column " + display + " in " + t.t.DisplayName)
}
func (t tbl) phys(display string) string {
	for _, c := range t.t.Columns {
		if c.DisplayName == display {
			return c.PhysicalName
		}
	}
	panic("demo: no column " + display + " in " + t.t.DisplayName)
}

// q quotes an identifier for SQL.
func q(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func num(n int64) json.Number { return json.Number(strconv.FormatInt(n, 10)) }

// money formats cents as a decimal currency string.
func money(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

// Column and table spec helpers shared by the Northwind and biosecurity parts.

func create(name, desc string, cols ...catalog.ColumnSpec) catalog.Op {
	return catalog.Op{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: name, Description: desc, TrackHistory: true, Columns: cols}}
}

func text(name string) catalog.ColumnSpec {
	return catalog.ColumnSpec{DisplayName: name, Type: catalog.ShortText}
}

func typed(name string, t catalog.FieldType) catalog.ColumnSpec {
	return catalog.ColumnSpec{DisplayName: name, Type: t}
}

func lookup(name string, parent tbl, display string) catalog.ColumnSpec {
	return catalog.ColumnSpec{DisplayName: name, Type: catalog.Lookup,
		Lookup: &catalog.LookupSpec{ParentTableID: parent.id(), DisplayColumnID: parent.col(display)}}
}

func required(c catalog.ColumnSpec) catalog.ColumnSpec { c.Required = true; return c }

func (b *Builder) build(ctx context.Context, kind Kind, id ids.WorkspaceID, owner string, now time.Time) error {
	l, err := b.Workspaces.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer l.Release()

	apply := func(ops ...catalog.Op) (*catalog.Schema, error) {
		if _, err := b.Catalog.Run(ctx, l, ops, catalog.Apply, catalog.ApplyOptions{UserID: owner}); err != nil {
			return nil, err
		}
		return b.Schemas.Get(ctx, l)
	}
	table := func(sch *catalog.Schema, display string) tbl {
		for i := range sch.Tables {
			if sch.Tables[i].DisplayName == display {
				return tbl{&sch.Tables[i]}
			}
		}
		panic("demo: no table " + display)
	}

	actor := records.Actor{UserID: owner}
	insert := func(t tbl, rows []map[string]any) ([]int64, error) {
		var out []int64
		for len(rows) > 0 {
			n := min(len(rows), records.MaxBatch)
			ops := make([]records.BatchOp, n)
			for i := range ops {
				ops[i] = records.BatchOp{Op: "insert", Fields: rows[i]}
			}
			res, err := b.Records.Batch(ctx, l, t.id(), ops, actor, nil)
			if err != nil {
				return nil, fmt.Errorf("insert into %s: %w", t.t.DisplayName, err)
			}
			for _, r := range res {
				out = append(out, r.ID)
			}
			rows = rows[n:]
		}
		return out, nil
	}

	switch kind {
	case Traders:
		kit := &tradersKit{b: b, ctx: ctx, l: l, owner: owner, now: now, apply: apply, table: table, insert: insert}
		return kit.run()
	case Invasives:
		kit := &bioKit{b: b, ctx: ctx, l: l, owner: owner, now: now, apply: apply, table: table, insert: insert}
		if err := kit.run(); err != nil {
			return fmt.Errorf("invasive species: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown demo kind: %s", kind)
	}
}

func removeFiles(paths ...string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

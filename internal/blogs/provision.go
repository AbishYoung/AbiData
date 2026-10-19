package blogs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/workspace"
)

// Provisioner gives every user a blog: a system workspace built from the
// template and an _adm_blogs row (ADR 0017). EnsureBlog is idempotent.
type Provisioner struct {
	Admin      *admin.Store
	Workspaces *workspace.Manager
	Catalog    *catalog.Engine
	Schemas    *catalog.Cache
	Log        *slog.Logger
	// Now overrides the clock (tests).
	Now func() time.Time

	// mu serialises provisioning, so a login and the boot backfill cannot
	// both create a blog for the same user.
	mu sync.Mutex
}

func (p *Provisioner) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// EnsureBlog returns the user's blog, creating it (workspace, template and
// row) when it is missing, and filling in the template of a blog workspace
// that lacks it. A blog whose tables are damaged is left as it is and logged:
// it is not served until repaired.
func (p *Provisioner) EnsureBlog(ctx context.Context, userID string) (admin.Blog, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	b, err := p.Admin.GetBlogByUser(ctx, userID)
	if err == nil {
		return b, p.repair(ctx, b)
	}
	if !errors.Is(err, admin.ErrNotFound) {
		return admin.Blog{}, err
	}
	u, err := p.Admin.GetUser(ctx, userID)
	if err != nil {
		return admin.Blog{}, err
	}
	if u.Username == "" {
		return admin.Blog{}, fmt.Errorf("blogs: user %s has no username yet", u.ID)
	}
	return p.create(ctx, u)
}

func (p *Provisioner) create(ctx context.Context, u admin.User) (admin.Blog, error) {
	now := p.now()
	w := admin.Workspace{ID: ids.NewWorkspaceID(), Name: "Blog: " + u.Username, Status: admin.StatusActive,
		CreatedAt: now, CreatedBy: u.ID, SystemKind: admin.KindBlog}
	if err := p.Workspaces.Create(ctx, w.ID); err != nil {
		return admin.Blog{}, fmt.Errorf("blogs: create workspace: %w", err)
	}
	err := p.Admin.CreateWorkspace(ctx, w, u.ID)
	if errors.Is(err, admin.ErrConflict) {
		// The user already has an ordinary workspace with that name.
		w.Name = fmt.Sprintf("Blog: %s (%s)", u.Username, string(w.ID)[:6])
		err = p.Admin.CreateWorkspace(ctx, w, u.ID)
	}
	if err != nil {
		if paths, perr := p.Workspaces.PathsFor(w.ID); perr == nil {
			for _, f := range []string{paths.DB, paths.WAL, paths.SHM} {
				_ = os.Remove(f)
			}
		}
		return admin.Blog{}, fmt.Errorf("blogs: create workspace row: %w", err)
	}
	if err := p.applyTemplate(ctx, w.ID, u.ID); err != nil {
		p.retire(ctx, w)
		return admin.Blog{}, err
	}
	b := admin.Blog{ID: ids.New(), UserID: u.ID, Username: u.Username, WorkspaceID: w.ID, Title: u.DisplayName,
		CreatedAt: now, UpdatedAt: now}
	if err := p.Admin.CreateBlog(ctx, b); err != nil {
		p.retire(ctx, w)
		return admin.Blog{}, fmt.Errorf("blogs: create blog row: %w", err)
	}
	if err := p.Admin.Audit(ctx, admin.AuditEntry{At: now, UserID: u.ID, Action: "blog.provision", ObjectKind: "blog", ObjectID: b.ID,
		Details: map[string]any{"workspace_id": string(w.ID), "username": u.Username}}); err != nil && p.Log != nil {
		p.Log.Warn("audit write failed", "err", err)
	}
	return b, nil
}

// retire hides a half-built blog workspace, as the demo builder does.
func (p *Provisioner) retire(ctx context.Context, w admin.Workspace) {
	st := admin.StatusDeleted
	name := fmt.Sprintf("%s (failed %s)", w.Name, w.ID)
	if err := p.Admin.UpdateWorkspace(ctx, w.ID, admin.WorkspacePatch{Status: &st, Name: &name}); err != nil && p.Log != nil {
		p.Log.Error("retire failed blog workspace", "workspace_id", w.ID, "err", err)
	}
}

// applyTemplate creates the blog's tables and pins them.
func (p *Provisioner) applyTemplate(ctx context.Context, id ids.WorkspaceID, owner string) error {
	l, err := p.Workspaces.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer l.Release()
	ops, pins := templateOps()
	if _, err := p.Catalog.Run(ctx, l, ops, catalog.Apply, catalog.ApplyOptions{UserID: owner}); err != nil {
		return fmt.Errorf("blogs: template: %w", err)
	}
	return l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error { return writePins(ctx, tx, pins) })
}

// repair fills in the template of an active blog workspace that has none
// (for example one restored from a file without it).
func (p *Provisioner) repair(ctx context.Context, b admin.Blog) error {
	w, err := p.Admin.GetWorkspace(ctx, b.WorkspaceID)
	if err != nil || w.Status != admin.StatusActive {
		return err
	}
	l, err := p.Workspaces.Acquire(ctx, w.ID)
	if err != nil {
		return err
	}
	var pins Pins
	var ok bool
	err = l.Read(ctx, func(q dbx.Querier) error {
		var err error
		pins, ok, err = ReadPins(ctx, q)
		return err
	})
	if err == nil && ok {
		var s *catalog.Schema
		if s, err = p.Schemas.Get(ctx, l); err == nil {
			if uerr := p.upgrade(ctx, l, s, pins, b.UserID); uerr != nil && p.Log != nil {
				p.Log.Warn("blog workspace could not get the template's new columns", "workspace_id", w.ID, "err", uerr)
			}
			if missing := pins.Missing(s); len(missing) > 0 && p.Log != nil {
				p.Log.Warn("blog workspace is missing pinned parts; it is not served until repaired",
					"workspace_id", w.ID, "missing", missing)
			}
		}
	}
	l.Release()
	if err != nil || ok {
		return err
	}
	return p.applyTemplate(ctx, w.ID, b.UserID)
}

// upgrade adds the posts columns that joined the template after this blog
// was made (addedPostColumns), and pins them.
func (p *Provisioner) upgrade(ctx context.Context, l *workspace.Lease, s *catalog.Schema, pins Pins, owner string) error {
	if _, ok := s.Table(pins.Posts); !ok {
		return nil
	}
	var ops []catalog.Op
	added := map[string]string{}
	for _, c := range postColumns {
		if _, pinned := pins.Columns[c.PhysicalName]; pinned || !addedPostColumns[c.PhysicalName] {
			continue
		}
		c.ID = ids.New()
		added[c.PhysicalName] = c.ID
		ops = append(ops, catalog.Op{Kind: catalog.OpAddColumn, TableID: pins.Posts, Column: &c})
	}
	if len(ops) == 0 {
		return nil
	}
	if _, err := p.Catalog.Run(ctx, l, ops, catalog.Apply, catalog.ApplyOptions{UserID: owner}); err != nil {
		return err
	}
	next := pins
	next.Columns = make(map[string]string, len(pins.Columns)+len(added))
	for k, v := range pins.Columns {
		next.Columns[k] = v
	}
	for k, v := range added {
		next.Columns[k] = v
	}
	return l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error { return writePins(ctx, tx, next) })
}

// EnsureAll provisions a blog for every user without one, in batches, and
// returns how many it created. It runs at boot; failures are logged and the
// user is retried at their next login.
func (p *Provisioner) EnsureAll(ctx context.Context) (int, error) {
	const batch = 50
	created, failed := 0, map[string]bool{}
	for {
		users, err := p.Admin.ListUsersWithoutBlog(ctx, batch+len(failed))
		if err != nil {
			return created, err
		}
		progress := false
		for _, id := range users {
			if failed[id] {
				continue
			}
			if ctx.Err() != nil {
				return created, ctx.Err()
			}
			if _, err := p.EnsureBlog(ctx, id); err != nil {
				failed[id] = true
				if p.Log != nil {
					p.Log.Error("provision blog", "user_id", id, "err", err)
				}
				continue
			}
			created++
			progress = true
		}
		if !progress {
			return created, nil
		}
	}
}

package blogs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/workspace"
)

type rig struct {
	ctx context.Context
	adm *admin.Store
	wm  *workspace.Manager
	p   *Provisioner
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	adm, err := admin.Open(ctx, filepath.Join(dir, "system", "admin.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	wm, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = wm.Shutdown(ctx)
		_ = adm.Close()
	})
	schemas := catalog.NewCache()
	engine := &catalog.Engine{AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) { schemas.Put(l.ID(), s) }}
	p := &Provisioner{Admin: adm, Workspaces: wm, Catalog: engine, Schemas: schemas}
	engine.Guard = p.Guard
	return &rig{ctx: ctx, adm: adm, wm: wm, p: p}
}

func (r *rig) user(t *testing.T, email, username string) string {
	t.Helper()
	id := ids.New()
	if err := r.adm.CreateUser(r.ctx, admin.User{ID: id, Email: email, Username: username, DisplayName: "Name " + username,
		PasswordHash: "h", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (r *rig) schema(t *testing.T, ws ids.WorkspaceID) (*catalog.Schema, Pins) {
	t.Helper()
	l, err := r.wm.Acquire(r.ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	s, err := r.p.Schemas.Get(r.ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	var pins Pins
	var ok bool
	if err := l.Read(r.ctx, func(q dbx.Querier) error {
		var err error
		pins, ok, err = ReadPins(r.ctx, q)
		return err
	}); err != nil || !ok {
		t.Fatalf("pins: %v %v", ok, err)
	}
	return s, pins
}

func TestEnsureBlogProvisionsOnce(t *testing.T) {
	r := newRig(t)
	u := r.user(t, "ann@example.com", "ann")
	b, err := r.p.EnsureBlog(r.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if b.Username != "ann" || b.Title != "Name ann" {
		t.Fatalf("%+v", b)
	}
	w, err := r.adm.GetWorkspace(r.ctx, b.WorkspaceID)
	if err != nil || w.SystemKind != admin.KindBlog || w.Name != "Blog: ann" || w.Status != admin.StatusActive {
		t.Fatalf("%+v %v", w, err)
	}
	if role, err := r.adm.GetRole(r.ctx, w.ID, u); err != nil || role != admin.RoleOwner {
		t.Fatalf("role %v %v", role, err)
	}
	s, pins := r.schema(t, w.ID)
	if m := pins.Missing(s); len(m) != 0 {
		t.Fatalf("missing %v", m)
	}
	posts, _ := s.Table(pins.Posts)
	if posts.PhysicalName != "posts" || !posts.TrackHistory {
		t.Fatalf("%+v", posts)
	}

	again, err := r.p.EnsureBlog(r.ctx, u)
	if err != nil || again.ID != b.ID {
		t.Fatalf("second ensure: %+v %v", again, err)
	}
	acc, _ := r.adm.ListWorkspacesForUser(r.ctx, u)
	if len(acc) != 1 {
		t.Fatalf("%d workspaces after two ensures", len(acc))
	}
	audit, _ := r.adm.ListAudit(r.ctx, 10)
	if len(audit) != 1 || audit[0].Action != "blog.provision" {
		t.Fatalf("audit %+v", audit)
	}
}

func TestEnsureAllAndNameClash(t *testing.T) {
	r := newRig(t)
	a := r.user(t, "a@example.com", "aaa")
	r.user(t, "b@example.com", "bbb")
	noName := r.user(t, "c@example.com", "")
	// An ordinary workspace already has the blog's name.
	clash := admin.Workspace{ID: ids.NewWorkspaceID(), Name: "Blog: aaa", Status: admin.StatusActive, CreatedAt: time.Now(), CreatedBy: a}
	if err := r.wm.Create(r.ctx, clash.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.adm.CreateWorkspace(r.ctx, clash, a); err != nil {
		t.Fatal(err)
	}
	n, err := r.p.EnsureAll(r.ctx)
	if err != nil || n != 2 {
		t.Fatalf("created %d, %v", n, err)
	}
	b, err := r.adm.GetBlogByUser(r.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := r.adm.GetWorkspace(r.ctx, b.WorkspaceID); w.Name == "Blog: aaa" || w.SystemKind != admin.KindBlog {
		t.Fatalf("clash name %q", w.Name)
	}
	if _, err := r.adm.GetBlogByUser(r.ctx, noName); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("user without a username got a blog: %v", err)
	}
	if n, err := r.p.EnsureAll(r.ctx); err != nil || n != 0 {
		t.Fatalf("second run %d %v", n, err)
	}
}

// TestRepairAddsNewTemplateColumns: a blog made before the images column
// existed is still served, and repair adds and pins the column.
func TestRepairAddsNewTemplateColumns(t *testing.T) {
	r := newRig(t)
	u := r.user(t, "ann@example.com", "ann")
	b, err := r.p.EnsureBlog(r.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	// Make it an old blog: unpin the images column, then drop it.
	_, pins := r.schema(t, b.WorkspaceID)
	imagesID := pins.Columns["images"]
	delete(pins.Columns, "images")
	l, err := r.wm.Acquire(r.ctx, b.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error { return writePins(r.ctx, tx, pins) }); err != nil {
		l.Release()
		t.Fatal(err)
	}
	_, err = r.p.Catalog.Run(r.ctx, l, []catalog.Op{{Kind: catalog.OpDropColumn, TableID: pins.Posts, ColumnID: imagesID}}, catalog.Apply, catalog.ApplyOptions{UserID: u, Force: true})
	l.Release()
	if err != nil {
		t.Fatal(err)
	}
	s, pins := r.schema(t, b.WorkspaceID)
	if m := pins.Missing(s); len(m) != 0 {
		t.Fatalf("an old blog counts as damaged: %v", m)
	}
	if _, err := r.p.EnsureBlog(r.ctx, u); err != nil {
		t.Fatal(err)
	}
	s, pins = r.schema(t, b.WorkspaceID)
	posts, _ := s.Table(pins.Posts)
	c, ok := posts.Column(pins.Columns["images"])
	if !ok || c.Type != catalog.Attachments || c.PhysicalName != "images" || len(pins.Missing(s)) != 0 {
		t.Fatalf("images after repair: %+v %v %v", c, ok, pins.Missing(s))
	}
}

func TestGuardKeepsPinnedColumns(t *testing.T) {
	r := newRig(t)
	u := r.user(t, "ann@example.com", "ann")
	b, err := r.p.EnsureBlog(r.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	s, pins := r.schema(t, b.WorkspaceID)
	posts, _ := s.Table(pins.Posts)
	title, _ := posts.Column(pins.Columns["title"])
	run := func(op catalog.Op) error {
		l, err := r.wm.Acquire(r.ctx, b.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Release()
		_, err = r.p.Catalog.Run(r.ctx, l, []catalog.Op{op}, catalog.Apply, catalog.ApplyOptions{UserID: u, Force: true})
		return err
	}
	retyped := catalog.ColumnSpec{DisplayName: title.DisplayName, Type: catalog.LongText, Required: true}
	for name, op := range map[string]catalog.Op{
		"drop table":     {Kind: catalog.OpDropTable, TableID: pins.Posts},
		"drop redirects": {Kind: catalog.OpDropTable, TableID: pins.Redirects},
		"drop column":    {Kind: catalog.OpDropColumn, TableID: pins.Posts, ColumnID: pins.Columns["body_md"]},
		"rename column":  {Kind: catalog.OpRenameColumn, TableID: pins.Posts, ColumnID: pins.Columns["slug"], DisplayName: "Address"},
		"retype column":  {Kind: catalog.OpAlterColumn, TableID: pins.Posts, ColumnID: title.ID, Column: &retyped},
		"drop index":     {Kind: catalog.OpDropIndex, TableID: pins.Posts, IndexID: pins.SlugIndex},
	} {
		if err := run(op); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A new column and a new table name are fine.
	if err := run(catalog.Op{Kind: catalog.OpAddColumn, TableID: pins.Posts, Column: &catalog.ColumnSpec{DisplayName: "Mood", Type: catalog.ShortText}}); err != nil {
		t.Fatalf("add column: %v", err)
	}
	if err := run(catalog.Op{Kind: catalog.OpRenameTable, TableID: pins.Posts, DisplayName: "Articles"}); err != nil {
		t.Fatalf("rename table: %v", err)
	}
	s, pins = r.schema(t, b.WorkspaceID)
	if m := pins.Missing(s); len(m) != 0 {
		t.Fatalf("missing after allowed changes: %v", m)
	}
}

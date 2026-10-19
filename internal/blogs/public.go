package blogs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/sqlgen"
	"abidata/internal/workspace"
)

// HomePosts is how many posts the blog home lists.
const HomePosts = 50

// ErrHidden is every reason a blog or post is not served (unknown, unpublished,
// disabled, taken down, switched off, damaged). Callers answer the same
// NOT_FOUND for all of them, so the URL space reveals nothing (ADR 0017).
var ErrHidden = errors.New("blogs: not served")

// Moved is a permanent redirect: an old username or an old post slug.
type Moved struct {
	Username string
	Slug     string // "" for the blog home
}

func (m *Moved) Error() string { return fmt.Sprintf("blogs: moved to %s/%s", m.Username, m.Slug) }

// Visibility is everything outside a post that decides whether a blog is
// served. The renderer checks it on every request; no row is trusted alone.
type Visibility struct {
	InstallOn       bool // the install switch
	UserActive      bool // the owner is not disabled
	WorkspaceActive bool // the blog workspace is active (not suspended, restoring or deleted)
	BlogPublished   bool // the owner's switch
	BlogTakenDown   bool // an administrator's take-down of the whole blog
}

// Serves reports whether the blog home and its posts may be served.
func (v Visibility) Serves() bool {
	return v.InstallOn && v.UserActive && v.WorkspaceActive && v.BlogPublished && !v.BlogTakenDown
}

// PostServed reports whether a post may be served at its URL.
func PostServed(v Visibility, p Post, takenDown bool) bool {
	return v.Serves() && p.Public() && !takenDown
}

// PostListed reports whether a post appears on the home (and later the feed):
// served and published, not unlisted.
func PostListed(v Visibility, p Post, takenDown bool) bool {
	return PostServed(v, p, takenDown) && p.Status == StatusPublished
}

// Reader reads published blogs for the public pages. It opens only the
// pinned columns, and its queries put the status condition in the WHERE, so
// drafts never leave the workspace.
type Reader struct {
	Admin      *admin.Store
	Workspaces *workspace.Manager
	Schemas    *catalog.Cache
	Switch     InstallSwitch
	// TakenDown reports an active take-down of the blog (slug "") or one post (phase 5). Nil: none.
	TakenDown func(ctx context.Context, blogID, slug string) (bool, error)
	Now       func() time.Time
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reader) takenDown(ctx context.Context, blogID, slug string) (bool, error) {
	if r.TakenDown == nil {
		return false, nil
	}
	return r.TakenDown(ctx, blogID, slug)
}

// Blog resolves a username to a blog that is served. An old username still on
// hold answers *Moved; every other failure is ErrHidden (or an internal error).
func (r *Reader) Blog(ctx context.Context, username string) (admin.Blog, error) {
	var v Visibility
	v.InstallOn = r.Switch.Enabled(ctx)
	if !v.InstallOn || !validUsernameShape(username) {
		return admin.Blog{}, ErrHidden
	}
	b, err := r.Admin.GetBlogByUsername(ctx, username)
	if errors.Is(err, admin.ErrNotFound) {
		if next, err := r.Admin.ResolveOldUsername(ctx, strings.ToLower(username), r.now()); err == nil {
			return admin.Blog{}, &Moved{Username: next}
		} else if !errors.Is(err, admin.ErrNotFound) {
			return admin.Blog{}, err
		}
		return admin.Blog{}, ErrHidden
	} else if err != nil {
		return admin.Blog{}, err
	}
	u, err := r.Admin.GetUser(ctx, b.UserID)
	if err != nil {
		return admin.Blog{}, hiddenIfMissing(err)
	}
	w, err := r.Admin.GetWorkspace(ctx, b.WorkspaceID)
	if err != nil {
		return admin.Blog{}, hiddenIfMissing(err)
	}
	v.UserActive, v.WorkspaceActive, v.BlogPublished = !u.Disabled, w.Status == admin.StatusActive, b.Published
	if v.BlogTakenDown, err = r.takenDown(ctx, b.ID, ""); err != nil {
		return admin.Blog{}, err
	}
	if !v.Serves() {
		return admin.Blog{}, ErrHidden
	}
	// A request in another case answers with the canonical (stored) name.
	if username != b.Username {
		return admin.Blog{}, &Moved{Username: b.Username}
	}
	return b, nil
}

// validUsernameShape rejects what can never be a username before any query.
func validUsernameShape(s string) bool {
	if len(s) < 3 || len(s) > 30 {
		return false
	}
	return strings.IndexFunc(s, func(c rune) bool {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
		return !ok
	}) < 0
}

func hiddenIfMissing(err error) error {
	if errors.Is(err, admin.ErrNotFound) {
		return ErrHidden
	}
	return err
}

// open leases the blog workspace and loads its schema and pins. A damaged
// blog (missing pins) is hidden until the owner or EnsureBlog repairs it.
func (r *Reader) open(ctx context.Context, b admin.Blog) (*workspace.Lease, *catalog.Schema, Pins, error) {
	l, err := r.Workspaces.Acquire(ctx, b.WorkspaceID)
	if err != nil {
		return nil, nil, Pins{}, err
	}
	var pins Pins
	var ok bool
	if err := l.Read(ctx, func(q dbx.Querier) error {
		var err error
		pins, ok, err = ReadPins(ctx, q)
		return err
	}); err != nil {
		l.Release()
		return nil, nil, Pins{}, err
	}
	sch, err := r.Schemas.Get(ctx, l)
	if err != nil {
		l.Release()
		return nil, nil, Pins{}, err
	}
	if !ok || len(pins.Missing(sch)) > 0 {
		l.Release()
		return nil, nil, Pins{}, ErrHidden
	}
	return l, sch, pins, nil
}

func statusIs(col func(string) sqlgen.Col, statuses ...string) sqlgen.Expr {
	list := make([]sqlgen.Expr, len(statuses))
	for i, s := range statuses {
		list[i] = sqlgen.Arg{Value: s}
	}
	return sqlgen.In{X: col("status"), List: list}
}

// Home returns the newest published posts of a served blog, without bodies.
func (r *Reader) Home(ctx context.Context, b admin.Blog, limit int) ([]Post, error) {
	l, sch, pins, err := r.open(ctx, b)
	if err != nil {
		return nil, err
	}
	defer l.Release()
	var posts []Post
	err = l.Read(ctx, func(db dbx.Querier) error {
		posts, err = readPosts(ctx, db, sch, pins, postQuery{
			where: func(col func(string) sqlgen.Col) sqlgen.Expr { return statusIs(col, StatusPublished) },
			order: []sqlgen.Order{
				{Expr: sqlgen.Col{Name: physical(sch, pins, "published_at")}, Desc: true, Nulls: sqlgen.NullsLast},
				{Expr: sqlgen.Col{Name: "id"}, Desc: true},
			},
			// Rows the renderer refuses are dropped below, so read a little more than shown.
			limit: limit + 10,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	v := Visibility{InstallOn: true, UserActive: true, WorkspaceActive: true, BlogPublished: true}
	out := make([]Post, 0, len(posts))
	for _, p := range posts {
		down, err := r.takenDown(ctx, b.ID, p.Slug)
		if err != nil {
			return nil, err
		}
		if PostListed(v, p, down) && len(out) < limit {
			out = append(out, p)
		}
	}
	return out, nil
}

// Post returns one served post with its body. An old slug of a served post
// answers *Moved; everything else that is not served is ErrHidden.
func (r *Reader) Post(ctx context.Context, b admin.Blog, slug string) (Post, error) {
	if !ValidSlug(slug) {
		return Post{}, ErrHidden
	}
	l, sch, pins, err := r.open(ctx, b)
	if err != nil {
		return Post{}, err
	}
	defer l.Release()
	public := func(where func(func(string) sqlgen.Col) sqlgen.Expr) (Post, bool, error) {
		var posts []Post
		err := l.Read(ctx, func(db dbx.Querier) error {
			var err error
			posts, err = readPosts(ctx, db, sch, pins, postQuery{withBody: true, limit: 1, where: func(col func(string) sqlgen.Col) sqlgen.Expr {
				return sqlgen.And{where(col), statusIs(col, StatusPublished, StatusUnlisted)}
			}})
			return err
		})
		if err != nil || len(posts) == 0 {
			return Post{}, false, err
		}
		p := posts[0]
		down, err := r.takenDown(ctx, b.ID, p.Slug)
		if err != nil {
			return Post{}, false, err
		}
		v := Visibility{InstallOn: true, UserActive: true, WorkspaceActive: true, BlogPublished: true}
		return p, PostServed(v, p, down), nil
	}
	p, ok, err := public(func(col func(string) sqlgen.Col) sqlgen.Expr {
		return sqlgen.Binary{Op: sqlgen.OpEq, L: col("slug"), R: sqlgen.Arg{Value: slug}}
	})
	if err != nil || ok {
		return p, err
	}
	// An old slug of a post that is still served redirects to its current address.
	target, found, err := redirectTarget(ctx, l, sch, pins, slug)
	if err != nil || !found {
		return Post{}, firstErr(err, ErrHidden)
	}
	p, ok, err = public(func(func(string) sqlgen.Col) sqlgen.Expr {
		return sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: "id"}, R: sqlgen.Arg{Value: target}}
	})
	if err != nil {
		return Post{}, err
	}
	if !ok || p.Slug == slug {
		return Post{}, ErrHidden
	}
	return Post{}, &Moved{Username: b.Username, Slug: p.Slug}
}

// FileServed reports whether a workspace file may be served publicly: only
// when a served post's body refers to it (attachment:ID). Other files of the
// blog workspace, and images of drafts, stay private (ErrHidden).
func (r *Reader) FileServed(ctx context.Context, b admin.Blog, fileID string) error {
	if !ids.Valid(fileID) {
		return ErrHidden
	}
	l, sch, pins, err := r.open(ctx, b)
	if err != nil {
		return err
	}
	defer l.Release()
	var posts []Post
	err = l.Read(ctx, func(db dbx.Querier) error {
		var err error
		posts, err = readPosts(ctx, db, sch, pins, postQuery{limit: 50, where: func(col func(string) sqlgen.Col) sqlgen.Expr {
			return sqlgen.And{
				statusIs(col, StatusPublished, StatusUnlisted),
				sqlgen.Match{Kind: sqlgen.MatchContains, X: col("body_md"), Pattern: sqlgen.Arg{Value: FileScheme + fileID}},
			}
		}})
		return err
	})
	if err != nil {
		return err
	}
	v := Visibility{InstallOn: true, UserActive: true, WorkspaceActive: true, BlogPublished: true}
	for _, p := range posts {
		down, err := r.takenDown(ctx, b.ID, p.Slug)
		if err != nil {
			return err
		}
		if PostServed(v, p, down) {
			return nil
		}
	}
	return ErrHidden
}

func firstErr(err, otherwise error) error {
	if err != nil {
		return err
	}
	return otherwise
}

// physical is the physical name of a pinned posts column.
func physical(sch *catalog.Schema, pins Pins, role string) string {
	if t, ok := sch.Table(pins.Posts); ok {
		if c, ok := t.Column(pins.Columns[role]); ok {
			return c.PhysicalName
		}
	}
	return role
}

// redirectTarget looks an old slug up in the redirects table.
func redirectTarget(ctx context.Context, l *workspace.Lease, sch *catalog.Schema, pins Pins, slug string) (int64, bool, error) {
	t, ok := sch.Table(pins.Redirects)
	if !ok {
		return 0, false, nil
	}
	oldC, ok1 := t.Column(pins.RedirectCols["old_slug"])
	postC, ok2 := t.Column(pins.RedirectCols["post_id"])
	if !ok1 || !ok2 {
		return 0, false, nil
	}
	var id *int64
	err := l.Read(ctx, func(db dbx.Querier) error {
		stmt, err := sqlgen.Render(pgdb.DialectOf(db), &sqlgen.Select{
			Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: postC.PhysicalName}}},
			From:    sqlgen.Table{Name: t.PhysicalName},
			Where:   sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: oldC.PhysicalName}, R: sqlgen.Arg{Value: slug}},
			Limit:   sqlgen.Lit{Value: 1},
		})
		if err != nil {
			return err
		}
		return db.QueryRow(ctx, stmt.SQL, stmt.Args...).Scan(&id)
	})
	if dbx.IsNoRows(err) || (err == nil && id == nil) {
		return 0, false, nil
	} else if err != nil {
		return 0, false, err
	}
	return *id, true, nil
}

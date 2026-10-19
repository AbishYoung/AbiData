// Package blogs holds user blogs (ADR 0017): the blog workspace's template and
// its pinned columns, provisioning, and (later phases) the post rules, the
// Markdown pipeline and the public renderer.
package blogs

import (
	"context"
	"encoding/json"
	"fmt"

	"abidata/internal/admin"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// Post statuses.
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusUnlisted  = "unlisted"
)

// Limits (ADR 0017 "Quotas").
const (
	MaxTitle    = 200
	MaxSlug     = 80
	MaxSummary  = 300
	MaxBodySize = 200 << 10
)

// metaKey is the _sys_meta key that flags the blog's tables. The tables are
// found by id, not by name, so renaming them cannot unpin them.
const metaKey = "blog.pins"

// Pins records which tables and columns of a blog workspace the server owns.
// Columns maps a column's role (its template physical name) to its id.
type Pins struct {
	Posts        string            `json:"posts"`
	Columns      map[string]string `json:"columns"`
	SlugIndex    string            `json:"slug_index"`
	Redirects    string            `json:"redirects"`
	RedirectCols map[string]string `json:"redirect_columns"`
}

// postColumns are the posts table's pinned columns, in template order.
var postColumns = []catalog.ColumnSpec{
	{DisplayName: "Title", PhysicalName: "title", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: MaxTitle}},
	{DisplayName: "Slug", PhysicalName: "slug", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: MaxSlug},
		Description: "The post's address: /b/{username}/{slug}."},
	{DisplayName: "Body", PhysicalName: "body_md", Type: catalog.LongText, Description: "Markdown."},
	{DisplayName: "Summary", PhysicalName: "summary", Type: catalog.ShortText, Options: catalog.TypeOptions{MaxLength: MaxSummary},
		Description: "Shown in the post list, the feed and link previews."},
	{DisplayName: "Status", PhysicalName: "status", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: 20},
		DefaultSQL: "'draft'", ValidationSQL: "status IN ('draft', 'published', 'unlisted')", ValidationText: "Status is draft, published or unlisted."},
	{DisplayName: "Published at", PhysicalName: "published_at", Type: catalog.DateTime, Description: "Set on first publish; can be changed to back-date a post."},
}

// redirectColumns are the old-slug redirects' pinned columns.
var redirectColumns = []catalog.ColumnSpec{
	{DisplayName: "Old slug", PhysicalName: "old_slug", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: MaxSlug}},
	{DisplayName: "Post", PhysicalName: "post_id", Type: catalog.Integer, Required: true, Description: "The id of the post the old slug now redirects to."},
}

// templateOps builds the blog workspace's tables with fresh ids and returns the pins for them.
func templateOps() ([]catalog.Op, Pins) {
	pins := Pins{Posts: ids.New(), Columns: map[string]string{}, SlugIndex: ids.New(), Redirects: ids.New(), RedirectCols: map[string]string{}}
	posts := &catalog.TableSpec{ID: pins.Posts, DisplayName: "Posts", PhysicalName: "posts", TrackHistory: true,
		Description: "Your blog's posts. Write them in the blog editor; the pinned columns cannot be removed or retyped."}
	for _, c := range postColumns {
		c.ID = ids.New()
		pins.Columns[c.PhysicalName] = c.ID
		posts.Columns = append(posts.Columns, c)
	}
	redirects := &catalog.TableSpec{ID: pins.Redirects, DisplayName: "Post slugs", PhysicalName: "post_slugs",
		Description: "Old post addresses that redirect to their post. Maintained by the blog."}
	for _, c := range redirectColumns {
		c.ID = ids.New()
		pins.RedirectCols[c.PhysicalName] = c.ID
		redirects.Columns = append(redirects.Columns, c)
	}
	return []catalog.Op{
		{Kind: catalog.OpCreateTable, Table: posts},
		{Kind: catalog.OpCreateIndex, Index: &catalog.IndexSpec{ID: pins.SlugIndex, TableID: pins.Posts, Unique: true,
			ColumnIDs: []string{pins.Columns["slug"]}}},
		{Kind: catalog.OpCreateTable, Table: redirects},
		{Kind: catalog.OpCreateIndex, Index: &catalog.IndexSpec{TableID: pins.Redirects, Unique: true,
			ColumnIDs: []string{pins.RedirectCols["old_slug"]}}},
	}, pins
}

// ReadPins returns the workspace's pins, or ok=false when it is not (yet) a blog workspace.
func ReadPins(ctx context.Context, q dbx.Querier) (Pins, bool, error) {
	var raw string
	err := q.QueryRow(ctx, sqlReadPins.SQL, metaKey).Scan(&raw)
	if dbx.IsNoRows(err) {
		return Pins{}, false, nil
	}
	if err != nil {
		return Pins{}, false, err
	}
	var p Pins
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return Pins{}, false, fmt.Errorf("blogs: bad %s: %w", metaKey, err)
	}
	return p, true, nil
}

func writePins(ctx context.Context, tx dbx.Tx, p Pins) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, sqlWritePins.SQL, metaKey, string(b))
	return err
}

// Missing lists what the schema lacks of the pins: the tables, columns (with
// the expected type) or the slug index. Empty means the blog can be served.
func (p Pins) Missing(s *catalog.Schema) []string {
	var out []string
	check := func(tableID string, cols map[string]string, specs []catalog.ColumnSpec, name string) {
		t, ok := s.Table(tableID)
		if !ok {
			out = append(out, "table "+name)
			return
		}
		for _, c := range specs {
			col, ok := t.Column(cols[c.PhysicalName])
			if !ok || col.Type != c.Type || col.PhysicalName != c.PhysicalName {
				out = append(out, name+"."+c.PhysicalName)
			}
		}
	}
	check(p.Posts, p.Columns, postColumns, "posts")
	check(p.Redirects, p.RedirectCols, redirectColumns, "post_slugs")
	if t, ok := s.Table(p.Posts); ok {
		found := false
		for _, ix := range t.Indexes {
			found = found || ix.ID == p.SlugIndex
		}
		if !found {
			out = append(out, "posts slug index")
		}
	}
	return out
}

// Guard refuses schema changes that would drop, rename or retype a pinned
// table, column or index of a blog workspace (ADR 0017). Other changes,
// including new columns and a new display name for a table, are allowed. A
// copy restored as a new workspace keeps the pins in its file but is an
// ordinary workspace, so only system_kind 'blog' workspaces are guarded.
// Wire it as catalog.Engine.Guard.
func (p *Provisioner) Guard(ctx context.Context, ws ids.WorkspaceID, tx dbx.Tx, s *catalog.Schema, op catalog.Op) error {
	pins, ok, err := ReadPins(ctx, tx)
	if err != nil || !ok {
		return err
	}
	w, err := p.Admin.GetWorkspace(ctx, ws)
	if err != nil {
		return err
	}
	if w.SystemKind != admin.KindBlog {
		return nil
	}
	return guardOp(pins, s, op)
}

func guardOp(pins Pins, s *catalog.Schema, op catalog.Op) error {
	pinnedTable := op.TableID == pins.Posts || op.TableID == pins.Redirects
	pinnedCol := func(id string) bool {
		for _, m := range []map[string]string{pins.Columns, pins.RedirectCols} {
			for _, c := range m {
				if c == id {
					return true
				}
			}
		}
		return false
	}
	refuse := func(what string) error {
		return fault.Newf(fault.BadRequest, "%s is part of your blog and cannot be removed, renamed or retyped. You can add columns of your own.", what)
	}
	switch op.Kind {
	case catalog.OpDropTable:
		if pinnedTable {
			return refuse("This table")
		}
	case catalog.OpRenameTable:
		if pinnedTable && op.PhysicalName != "" {
			if t, ok := s.Table(op.TableID); ok && op.PhysicalName != t.PhysicalName {
				return refuse("This table's physical name")
			}
		}
	case catalog.OpDropColumn, catalog.OpRenameColumn:
		if pinnedCol(op.ColumnID) {
			return refuse("This column")
		}
	case catalog.OpAlterColumn:
		if pinnedCol(op.ColumnID) && op.Column != nil {
			if c, ok := s.Column(op.ColumnID); ok && (op.Column.Type != c.Type ||
				(op.Column.PhysicalName != "" && op.Column.PhysicalName != c.PhysicalName) ||
				(op.Column.DisplayName != "" && op.Column.DisplayName != c.DisplayName)) {
				return refuse("This column")
			}
		}
	case catalog.OpDropIndex:
		if op.IndexID == pins.SlugIndex {
			return refuse("The unique slug index")
		}
	}
	return nil
}

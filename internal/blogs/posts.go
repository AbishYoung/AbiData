package blogs

import (
	"context"
	"fmt"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/pgdb"
	"abidata/internal/records"
	"abidata/internal/sqlgen"
)

// Post is one row of a blog's posts table, read through the pinned columns
// only (ADR 0017): never through the generic records API, so columns a user
// adds never reach a page.
type Post struct {
	ID          int64
	Version     int64
	Title       string
	Slug        string
	Summary     string
	Body        string // empty unless read with the body
	BodyBytes   int64
	Status      string
	PublishedAt *time.Time
	Cover       string // a file id, or ""
	CoverAlt    string
}

// Problem says why the renderer would refuse the post ("" when it would not):
// rows written from the generic tools can break rules the blog API keeps.
func (p Post) Problem() string {
	switch {
	case !ValidSlug(p.Slug):
		return "The slug is not valid: use lower-case letters, digits and single hyphens."
	case p.Title == "":
		return "The title is empty."
	case p.BodyBytes > MaxBodySize:
		return fmt.Sprintf("The body is larger than %d KB.", MaxBodySize>>10)
	case p.Status != StatusDraft && p.Status != StatusPublished && p.Status != StatusUnlisted:
		return "The status is not draft, published or unlisted."
	}
	return ""
}

// Public reports whether the post's own state lets it be served.
func (p Post) Public() bool {
	return (p.Status == StatusPublished || p.Status == StatusUnlisted) && p.Problem() == ""
}

// postQuery selects posts by the pinned columns. where may be nil.
type postQuery struct {
	withBody bool
	where    func(col func(role string) sqlgen.Col) sqlgen.Expr
	order    []sqlgen.Order
	limit    int
}

// readPosts runs q against the blog's posts table.
func readPosts(ctx context.Context, db dbx.Querier, s *catalog.Schema, pins Pins, q postQuery) ([]Post, error) {
	t, ok := s.Table(pins.Posts)
	if !ok {
		return nil, errDamaged
	}
	col := func(role string) sqlgen.Col {
		c, ok := t.Column(pins.Columns[role])
		if !ok {
			return sqlgen.Col{Name: role}
		}
		if c.Type == catalog.Attachments {
			return sqlgen.Col{Name: c.PhysicalName, Type: sqlgen.Attachments}
		}
		return sqlgen.Col{Name: c.PhysicalName}
	}
	if m := pins.Missing(s); len(m) > 0 {
		return nil, errDamaged
	}
	_, hasCover := pins.Columns["cover"]
	_, hasAlt := pins.Columns["cover_alt"]
	hasCover = hasCover && hasAlt
	cols := []sqlgen.Column{
		{Expr: sqlgen.Col{Name: "id"}}, {Expr: sqlgen.Col{Name: "_version"}},
		{Expr: col("title")}, {Expr: col("slug")}, {Expr: col("summary")}, {Expr: col("status")}, {Expr: col("published_at")},
		{Expr: sqlgen.Func{Name: "octet_length", Args: []sqlgen.Expr{col("body_md")}}},
	}
	if q.withBody {
		cols = append(cols, sqlgen.Column{Expr: col("body_md")})
	}
	if hasCover {
		cols = append(cols, sqlgen.Column{Expr: col("cover")}, sqlgen.Column{Expr: col("cover_alt")})
	}
	sel := &sqlgen.Select{Columns: cols, From: sqlgen.Table{Name: t.PhysicalName}, OrderBy: q.order}
	if q.where != nil {
		sel.Where = q.where(col)
	}
	if q.limit > 0 {
		sel.Limit = sqlgen.Lit{Value: q.limit}
	}
	stmt, err := sqlgen.Render(pgdb.DialectOf(db), sel)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, stmt.SQL, stmt.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var p Post
		var title, slug, summary, status, body *string
		var published any // RFC 3339 text on SQLite, a time on PostgreSQL
		var size *int64
		dest := []any{&p.ID, &p.Version, &title, &slug, &summary, &status, &published, &size}
		if q.withBody {
			dest = append(dest, &body)
		}
		var cover any
		var coverAlt *string
		if hasCover {
			dest = append(dest, &cover, &coverAlt)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if files := records.AttachmentIDs(textOf(catalog.StorageForm(catalog.Attachments, cover))); len(files) > 0 {
			p.Cover, p.CoverAlt = files[0], deref(coverAlt)
		}
		p.Title, p.Slug, p.Summary, p.Status, p.Body = deref(title), deref(slug), deref(summary), deref(status), deref(body)
		if size != nil {
			p.BodyBytes = *size
		}
		switch v := published.(type) {
		case time.Time:
			t := v.UTC()
			p.PublishedAt = &t
		case string:
			if t, err := dbx.ParseTime(v); err == nil {
				p.PublishedAt = &t
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// textOf turns a stored text value as a driver returns it into a string.
func textOf(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// errDamaged is returned for a blog workspace whose pinned tables are missing or changed.
var errDamaged = fmt.Errorf("blogs: the blog's posts table is missing or damaged")

func byID(id int64) func(func(string) sqlgen.Col) sqlgen.Expr {
	return func(func(string) sqlgen.Col) sqlgen.Expr {
		return sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: "id"}, R: sqlgen.Arg{Value: id}}
	}
}

// lastEdited maps post ids to the time of their latest change (row history).
func lastEdited(ctx context.Context, db dbx.Querier, tableID string) (map[int64]time.Time, error) {
	rows, err := db.Query(ctx, sqlLastEdited.SQL, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var id int64
		var at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		if t, err := dbx.ParseTime(at); err == nil {
			out[id] = t
		}
	}
	return out, rows.Err()
}

package admin

import (
	"context"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/sqlgen"
)

// Blog is a user's blog home (ADR 0017). Its posts live in the blog workspace.
type Blog struct {
	ID          string
	UserID      string
	Username    string // the owner's current username (joined)
	WorkspaceID ids.WorkspaceID
	Slug        string
	Title       string
	Intro       string
	Published   bool
	PublishedAt *time.Time // first time the blog was switched on
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func scanBlog(r scanner) (Blog, error) {
	var b Blog
	var ws, created, updated string
	var published int64
	var publishedAt, username *string
	if err := r.Scan(&b.ID, &b.UserID, &ws, &b.Slug, &b.Title, &b.Intro, &published, &publishedAt, &created, &updated, &username); err != nil {
		return Blog{}, err
	}
	b.WorkspaceID, b.Published = ids.WorkspaceID(ws), published == 1
	if username != nil {
		b.Username = *username
	}
	var err error
	if b.CreatedAt, err = parseTime(created); err != nil {
		return Blog{}, err
	}
	if b.UpdatedAt, err = parseTime(updated); err != nil {
		return Blog{}, err
	}
	if publishedAt != nil {
		t, err := parseTime(*publishedAt)
		if err != nil {
			return Blog{}, err
		}
		b.PublishedAt = &t
	}
	return b, nil
}

// CreateBlog inserts a blog row. ErrConflict if the user or workspace already has one.
func (s *Store) CreateBlog(ctx context.Context, b Blog) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlCreateBlog.SQL, b.ID, b.UserID, string(b.WorkspaceID), b.Title, b.CreatedAt, b.UpdatedAt)
		return wrapWrite(err, "create blog")
	})
}

func (s *Store) getBlog(ctx context.Context, st *sqlgen.Static, arg string) (Blog, error) {
	var b Blog
	err := s.Read(ctx, func(c dbx.Querier) error {
		var err error
		b, err = scanBlog(c.QueryRow(ctx, st.SQL, arg))
		return err
	})
	if err != nil {
		return Blog{}, notFoundIfNoRows(err, "blog")
	}
	return b, nil
}

// GetBlog finds a blog by id.
func (s *Store) GetBlog(ctx context.Context, id string) (Blog, error) {
	return s.getBlog(ctx, sqlBlogByID, id)
}

// GetBlogByUser finds a user's blog.
func (s *Store) GetBlogByUser(ctx context.Context, userID string) (Blog, error) {
	return s.getBlog(ctx, sqlBlogByUser, userID)
}

// GetBlogByWorkspace finds the blog a workspace holds.
func (s *Store) GetBlogByWorkspace(ctx context.Context, ws ids.WorkspaceID) (Blog, error) {
	return s.getBlog(ctx, sqlBlogByWorkspace, string(ws))
}

// GetBlogByUsername finds the blog of the user with this current username (case-insensitive).
func (s *Store) GetBlogByUsername(ctx context.Context, username string) (Blog, error) {
	return s.getBlog(ctx, sqlBlogByUsername, username)
}

// UpdateBlog sets the blog home's title and intro.
func (s *Store) UpdateBlog(ctx context.Context, id, title, intro string, at time.Time) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlUpdateBlog.SQL, title, intro, at, id)
		if err != nil {
			return err
		}
		return requireChanged(res, "update blog")
	})
}

// SetBlogPublished switches the blog on or off; the first switch-on sets published_at.
func (s *Store) SetBlogPublished(ctx context.Context, id string, published bool, at time.Time) error {
	st := sqlUnpublishBlog
	if published {
		st = sqlPublishBlog
	}
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, st.SQL, at, id)
		if err != nil {
			return err
		}
		return requireChanged(res, "publish blog")
	})
}

// ListUsersWithoutBlog returns up to limit user ids that have no blog yet, oldest first.
func (s *Store) ListUsersWithoutBlog(ctx context.Context, limit int) ([]string, error) {
	var out []string
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, sqlUsersWithoutBlog.SQL, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

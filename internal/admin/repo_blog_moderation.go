package admin

import (
	"context"
	"strings"
	"time"

	"abidata/internal/dbx"
)

// Take-down reasons of blogs and reader reports (ADR 0017).
var BlogReasons = []string{"spam", "phishing", "malware", "illegal", "harassment", "copyright", "other"}

// BlogTakedown is one take-down of a blog (PostSlug "") or of one post. A
// restore keeps the row, so the table is the history.
type BlogTakedown struct {
	ID          string
	BlogID      string
	PostSlug    string
	ReasonCode  string
	Note        string
	TakenDownBy string
	TakenDownAt time.Time
	RestoredBy  string
	RestoredAt  *time.Time
}

// BlogReport is a reader's report of a blog or post.
type BlogReport struct {
	ID         string
	BlogID     string
	PostSlug   string
	ReasonCode string
	Message    string
	CreatedAt  time.Time
	ResolvedBy string
	ResolvedAt *time.Time
}

// BlogSummary is a blog with its open moderation counts, for the admin list.
type BlogSummary struct {
	Blog
	ActiveTakedowns int
	OpenReports     int
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optTime(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := parseTime(*s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TakeDown records an active take-down. ErrConflict if one is already active
// for the same blog and post.
func (s *Store) TakeDown(ctx context.Context, td BlogTakedown) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlTakeDown.SQL, td.ID, td.BlogID, nullable(td.PostSlug), td.ReasonCode, td.Note, td.TakenDownBy, td.TakenDownAt)
		return wrapWrite(err, "take down")
	})
}

// Restore ends the active take-down of a blog (slug "") or a post. ErrNotFound if none is active.
func (s *Store) Restore(ctx context.Context, blogID, slug, by string, at time.Time) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		var res dbx.Result
		var err error
		if slug == "" {
			res, err = tx.Exec(ctx, sqlRestoreBlog.SQL, by, at, blogID)
		} else {
			res, err = tx.Exec(ctx, sqlRestorePost.SQL, by, at, blogID, slug)
		}
		if err != nil {
			return err
		}
		return requireChanged(res, "restore")
	})
}

// TakenDown reports an active take-down of the blog (slug "") or of one post.
func (s *Store) TakenDown(ctx context.Context, blogID, slug string) (bool, error) {
	var n int64
	err := s.Read(ctx, func(c dbx.Querier) error {
		if slug == "" {
			return c.QueryRow(ctx, sqlBlogTakenDown.SQL, blogID).Scan(&n)
		}
		return c.QueryRow(ctx, sqlPostTakenDown.SQL, blogID, slug).Scan(&n)
	})
	return n > 0, err
}

// Takedowns returns a blog's take-downs, newest first, active and restored.
func (s *Store) Takedowns(ctx context.Context, blogID string) ([]BlogTakedown, error) {
	out := []BlogTakedown{}
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, sqlTakedowns.SQL, blogID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var td BlogTakedown
			var slug, restoredBy, restoredAt *string
			var at string
			if err := rows.Scan(&td.ID, &td.BlogID, &slug, &td.ReasonCode, &td.Note, &td.TakenDownBy, &at, &restoredBy, &restoredAt); err != nil {
				return err
			}
			if slug != nil {
				td.PostSlug = *slug
			}
			if restoredBy != nil {
				td.RestoredBy = *restoredBy
			}
			if td.TakenDownAt, err = parseTime(at); err != nil {
				return err
			}
			if td.RestoredAt, err = optTime(restoredAt); err != nil {
				return err
			}
			out = append(out, td)
		}
		return rows.Err()
	})
	return out, err
}

// CreateBlogReport stores a reader's report.
func (s *Store) CreateBlogReport(ctx context.Context, r BlogReport) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlCreateBlogReport.SQL, r.ID, r.BlogID, nullable(r.PostSlug), r.ReasonCode, r.Message, r.CreatedAt)
		return wrapWrite(err, "report")
	})
}

// BlogReports returns a blog's reports, newest first.
func (s *Store) BlogReports(ctx context.Context, blogID string) ([]BlogReport, error) {
	out := []BlogReport{}
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, sqlBlogReports.SQL, blogID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r BlogReport
			var slug, resolvedBy, resolvedAt *string
			var at string
			if err := rows.Scan(&r.ID, &r.BlogID, &slug, &r.ReasonCode, &r.Message, &at, &resolvedBy, &resolvedAt); err != nil {
				return err
			}
			if slug != nil {
				r.PostSlug = *slug
			}
			if resolvedBy != nil {
				r.ResolvedBy = *resolvedBy
			}
			if r.CreatedAt, err = parseTime(at); err != nil {
				return err
			}
			if r.ResolvedAt, err = optTime(resolvedAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// ResolveBlogReports marks a blog's open reports as handled.
func (s *Store) ResolveBlogReports(ctx context.Context, blogID, by string, at time.Time) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlResolveBlogReports.SQL, by, at, blogID)
		return err
	})
}

// ListBlogs lists blogs by username (a substring; "" for all), optionally
// only those with an active take-down or an open report.
func (s *Store) ListBlogs(ctx context.Context, q string, takenDown, reported bool, limit int) ([]BlogSummary, error) {
	pattern := "%" + strings.NewReplacer(`\`, "", "%", "", "_", "").Replace(strings.ToLower(q)) + "%"
	flag := func(b bool) int64 {
		if b {
			return 1
		}
		return 0
	}
	stmt := sqlListBlogs
	if s.pg != nil {
		stmt = sqlListBlogsPG
	}
	out := []BlogSummary{}
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, stmt.SQL, pattern, flag(takenDown), flag(reported), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var bs BlogSummary
			var td, rp int64
			b, err := scanBlog(scanFunc(func(dest ...any) error { return rows.Scan(append(dest, &td, &rp)...) }))
			if err != nil {
				return err
			}
			bs.Blog, bs.ActiveTakedowns, bs.OpenReports = b, int(td), int(rp)
			out = append(out, bs)
		}
		return rows.Err()
	})
	return out, err
}

type scanFunc func(dest ...any) error

func (f scanFunc) Scan(dest ...any) error { return f(dest...) }

package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"abidata/internal/ids"
)

func TestBlogModeration(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	mkUser(t, s, "u2", "bob@example.com")
	_ = s.InitUsername(ctx, "u1", "ann")
	_ = s.InitUsername(ctx, "u2", "bob")
	for i, u := range []string{"u1", "u2"} {
		w := Workspace{ID: ids.NewWorkspaceID(), Name: "Blog " + u, Status: StatusActive, CreatedAt: t0, CreatedBy: u, SystemKind: KindBlog}
		if err := s.CreateWorkspace(ctx, w, u); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateBlog(ctx, Blog{ID: []string{"b1", "b2"}[i], UserID: u, WorkspaceID: w.ID, CreatedAt: t0, UpdatedAt: t0}); err != nil {
			t.Fatal(err)
		}
	}

	down := func(slug string) error {
		return s.TakeDown(ctx, BlogTakedown{ID: ids.New(), BlogID: "b1", PostSlug: slug, ReasonCode: "spam", Note: "Ads.", TakenDownBy: "root", TakenDownAt: t0})
	}
	if err := down("hello"); err != nil {
		t.Fatal(err)
	}
	if err := down("hello"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second active take-down: %v", err)
	}
	if err := down(""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		slug string
		want bool
	}{{"", true}, {"hello", true}, {"other", false}} {
		if got, err := s.TakenDown(ctx, "b1", c.slug); err != nil || got != c.want {
			t.Errorf("taken down %q: %v %v", c.slug, got, err)
		}
	}

	// Restore keeps the history and allows a new take-down.
	t1 := t0.Add(time.Hour)
	if err := s.Restore(ctx, "b1", "hello", "root", t1); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(ctx, "b1", "hello", "root", t1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restore twice: %v", err)
	}
	if got, _ := s.TakenDown(ctx, "b1", "hello"); got {
		t.Fatal("restored post still down")
	}
	if err := down("hello"); err != nil {
		t.Fatalf("take down again: %v", err)
	}
	hist, err := s.Takedowns(ctx, "b1")
	if err != nil || len(hist) != 3 {
		t.Fatalf("history %d %v", len(hist), err)
	}
	restored := 0
	for _, h := range hist {
		if h.RestoredAt != nil && h.RestoredBy == "root" {
			restored++
		}
	}
	if restored != 1 {
		t.Fatalf("restored rows %d", restored)
	}

	// Reports.
	if err := s.CreateBlogReport(ctx, BlogReport{ID: ids.New(), BlogID: "b2", PostSlug: "x", ReasonCode: "phishing", Message: "Fake bank.", CreatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	reps, err := s.BlogReports(ctx, "b2")
	if err != nil || len(reps) != 1 || reps[0].Message != "Fake bank." || reps[0].ResolvedAt != nil {
		t.Fatalf("reports %+v %v", reps, err)
	}

	// The admin list, with filters.
	list := func(q string, td, rp bool) []string {
		t.Helper()
		got, err := s.ListBlogs(ctx, q, td, rp, 100)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, b := range got {
			out = append(out, b.Username)
		}
		return out
	}
	if got := list("", false, false); len(got) != 2 {
		t.Fatalf("all %v", got)
	}
	if got := list("AN", false, false); len(got) != 1 || got[0] != "ann" {
		t.Fatalf("search %v", got)
	}
	if got := list("", true, false); len(got) != 1 || got[0] != "ann" {
		t.Fatalf("taken down %v", got)
	}
	if got := list("", false, true); len(got) != 1 || got[0] != "bob" {
		t.Fatalf("reported %v", got)
	}
	all, _ := s.ListBlogs(ctx, "", false, false, 100)
	if all[0].ActiveTakedowns != 2 || all[1].OpenReports != 1 {
		t.Fatalf("counts %+v", all)
	}
	if err := s.ResolveBlogReports(ctx, "b2", "root", t1); err != nil {
		t.Fatal(err)
	}
	if got := list("", false, true); len(got) != 0 {
		t.Fatalf("resolved still listed %v", got)
	}
}

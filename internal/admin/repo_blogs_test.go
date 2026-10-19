package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"abidata/internal/ids"
)

func TestBlogRows(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	mkUser(t, s, "u2", "bob@example.com")
	if err := s.InitUsername(ctx, "u1", "ann"); err != nil {
		t.Fatal(err)
	}
	w := Workspace{ID: ids.NewWorkspaceID(), Name: "Blog: ann", Status: StatusActive, CreatedAt: t0, CreatedBy: "u1", SystemKind: KindBlog}
	if err := s.CreateWorkspace(ctx, w, "u1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetWorkspace(ctx, w.ID); got.SystemKind != KindBlog {
		t.Fatalf("system kind %q", got.SystemKind)
	}
	plain := mkWS(t, s, "Plain", "u1")
	if got, _ := s.GetWorkspace(ctx, plain.ID); got.SystemKind != "" {
		t.Fatalf("plain workspace kind %q", got.SystemKind)
	}

	b := Blog{ID: "b1", UserID: "u1", WorkspaceID: w.ID, Title: "Ann's blog", CreatedAt: t0, UpdatedAt: t0}
	if err := s.CreateBlog(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateBlog(ctx, Blog{ID: "b2", UserID: "u1", WorkspaceID: plain.ID, CreatedAt: t0, UpdatedAt: t0}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second blog for a user: %v", err)
	}
	for _, get := range []func() (Blog, error){
		func() (Blog, error) { return s.GetBlog(ctx, "b1") },
		func() (Blog, error) { return s.GetBlogByUser(ctx, "u1") },
		func() (Blog, error) { return s.GetBlogByWorkspace(ctx, w.ID) },
		func() (Blog, error) { return s.GetBlogByUsername(ctx, "ANN") },
	} {
		got, err := get()
		if err != nil || got.ID != "b1" || got.Username != "ann" || got.Title != "Ann's blog" || got.Published {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if _, err := s.GetBlogByUser(ctx, "u2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no blog: %v", err)
	}

	t1 := t0.Add(time.Hour)
	if err := s.UpdateBlog(ctx, "b1", "Notes", "Hello.", t1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBlogPublished(ctx, "b1", true, t1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBlogPublished(ctx, "b1", false, t1.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBlogPublished(ctx, "b1", true, t1.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBlog(ctx, "b1")
	if !got.Published || got.Title != "Notes" || got.Intro != "Hello." || got.PublishedAt == nil || !got.PublishedAt.Equal(t1) {
		t.Fatalf("after updates: %+v", got)
	}

	without, err := s.ListUsersWithoutBlog(ctx, 10)
	if err != nil || len(without) != 1 || without[0] != "u2" {
		t.Fatalf("without blog: %v %v", without, err)
	}
}

func TestSystemWorkspaceMembershipPinned(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	mkUser(t, s, "u2", "bob@example.com")
	w := Workspace{ID: ids.NewWorkspaceID(), Name: "Blog: ann", Status: StatusActive, CreatedAt: t0, CreatedBy: "u1", SystemKind: KindBlog}
	if err := s.CreateWorkspace(ctx, w, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembership(ctx, Membership{WorkspaceID: w.ID, UserID: "u2", Role: RoleEditor}); !errors.Is(err, ErrProtected) {
		t.Fatalf("add member: %v", err)
	}
	viewer := RoleViewer
	if err := s.ChangeMembership(ctx, w.ID, "u1", &viewer); !errors.Is(err, ErrProtected) {
		t.Fatalf("change role: %v", err)
	}
	if err := s.ChangeMembership(ctx, w.ID, "u1", nil); !errors.Is(err, ErrProtected) {
		t.Fatalf("remove owner: %v", err)
	}
	if err := s.DeleteMembership(ctx, w.ID, "u1"); !errors.Is(err, ErrProtected) {
		t.Fatalf("delete membership: %v", err)
	}
	if r, err := s.GetRole(ctx, w.ID, "u1"); err != nil || r != RoleOwner {
		t.Fatalf("owner after refusals: %v %v", r, err)
	}
	// Ordinary workspaces are unaffected.
	plain := mkWS(t, s, "Plain", "u1")
	if err := s.SetMembership(ctx, Membership{WorkspaceID: plain.ID, UserID: "u2", Role: RoleEditor}); err != nil {
		t.Fatal(err)
	}
}

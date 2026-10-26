package blogs

import (
	"context"
	"errors"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/records"
)

// TestVisibility covers every combination of the conditions outside a post,
// and the post's own state: only all-clear is served.
func TestVisibility(t *testing.T) {
	for mask := 0; mask < 32; mask++ {
		v := Visibility{InstallOn: mask&1 != 0, UserActive: mask&2 != 0, WorkspaceActive: mask&4 != 0, BlogPublished: mask&8 != 0, BlogTakenDown: mask&16 != 0}
		if want := mask == 15; v.Serves() != want {
			t.Errorf("%+v: Serves %v", v, v.Serves())
		}
	}
	on := Visibility{InstallOn: true, UserActive: true, WorkspaceActive: true, BlogPublished: true}
	good := Post{Title: "T", Slug: "t", Status: StatusPublished}
	for _, c := range []struct {
		name           string
		v              Visibility
		p              Post
		down           bool
		served, listed bool
	}{
		{"published", on, good, false, true, true},
		{"unlisted", on, Post{Title: "T", Slug: "t", Status: StatusUnlisted}, false, true, false},
		{"draft", on, Post{Title: "T", Slug: "t", Status: StatusDraft}, false, false, false},
		{"odd status", on, Post{Title: "T", Slug: "t", Status: "secret"}, false, false, false},
		{"post taken down", on, good, true, false, false},
		{"bad slug", on, Post{Title: "T", Slug: "Bad Slug", Status: StatusPublished}, false, false, false},
		{"no title", on, Post{Slug: "t", Status: StatusPublished}, false, false, false},
		{"oversize body", on, Post{Title: "T", Slug: "t", Status: StatusPublished, BodyBytes: MaxBodySize + 1}, false, false, false},
		{"blog off", Visibility{InstallOn: true, UserActive: true, WorkspaceActive: true}, good, false, false, false},
	} {
		if got := PostServed(c.v, c.p, c.down); got != c.served {
			t.Errorf("%s: served %v", c.name, got)
		}
		if got := PostListed(c.v, c.p, c.down); got != c.listed {
			t.Errorf("%s: listed %v", c.name, got)
		}
	}
}

func TestInstallSwitch(t *testing.T) {
	r := newRig(t)
	w := InstallSwitch{Admin: r.adm, Allowed: true}
	if on, forced := w.On(r.ctx); on || forced {
		t.Fatalf("default: %v %v", on, forced)
	}
	if err := r.adm.PutSetting(r.ctx, SettingPublicBlogs, "true"); err != nil {
		t.Fatal(err)
	}
	if !w.Enabled(r.ctx) {
		t.Fatal("setting on: off")
	}
	w.Allowed = false
	if on, forced := w.On(r.ctx); on || !forced {
		t.Fatalf("forced off: %v %v", on, forced)
	}
}

func TestReader(t *testing.T) {
	r := newRig(t)
	u := r.user(t, "ann@example.com", "ann")
	b, err := r.p.EnsureBlog(r.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	rec := &records.Service{Schemas: r.p.Schemas}
	rec.Hook = (&Rules{Records: rec, Now: func() time.Time { return now }}).Hook
	_, pins := r.schema(t, b.WorkspaceID)
	c := pins.Columns
	l, err := r.wm.Acquire(r.ctx, b.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			l.Release()
		}
	}()
	actor := records.Actor{UserID: u}
	add := func(title, status string) records.Row {
		t.Helper()
		row, err := rec.Insert(r.ctx, l, pins.Posts, map[string]any{c["title"]: title, c["body_md"]: "Body of " + title}, actor, nil)
		if err != nil {
			t.Fatal(err)
		}
		if status != StatusDraft {
			now = now.Add(time.Hour)
			if row, err = rec.Update(r.ctx, l, pins.Posts, row.ID, row.Version, map[string]any{c["status"]: status}, actor, nil); err != nil {
				t.Fatal(err)
			}
		}
		return row
	}
	first := add("First", StatusPublished)
	add("Second", StatusPublished)
	add("Hidden", StatusUnlisted)
	add("Secret", StatusDraft)
	// The first post is renamed after publishing: its old slug redirects.
	if _, err := rec.Update(r.ctx, l, pins.Posts, first.ID, first.Version, map[string]any{c["slug"]: "first-renamed"}, actor, nil); err != nil {
		t.Fatal(err)
	}
	l.Release()
	held = false

	sw := InstallSwitch{Admin: r.adm, Allowed: true}
	rd := &Reader{Admin: r.adm, Workspaces: r.wm, Schemas: r.p.Schemas, Switch: sw, Now: func() time.Time { return now }}
	hidden := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrHidden) {
			t.Fatalf("%s: %v, want hidden", name, err)
		}
	}

	// Install switch off, then blog off: hidden.
	_, err = rd.Blog(r.ctx, "ann")
	hidden("switch off", err)
	if err := r.adm.PutSetting(r.ctx, SettingPublicBlogs, "true"); err != nil {
		t.Fatal(err)
	}
	_, err = rd.Blog(r.ctx, "ann")
	hidden("blog off", err)
	if err := r.adm.SetBlogPublished(r.ctx, b.ID, true, now); err != nil {
		t.Fatal(err)
	}
	blog, err := rd.Blog(r.ctx, "ann")
	if err != nil {
		t.Fatal(err)
	}
	_, err = rd.Blog(r.ctx, "nobody")
	hidden("unknown", err)
	_, err = rd.Blog(r.ctx, "../etc")
	hidden("malformed", err)
	var mv *Moved
	if _, err := rd.Blog(r.ctx, "Ann"); !errors.As(err, &mv) || mv.Username != "ann" {
		t.Fatalf("case: %v", err)
	}

	// The home lists published posts only, newest first.
	posts, err := rd.Home(r.ctx, blog, HomePosts)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || posts[0].Title != "Second" || posts[1].Slug != "first-renamed" {
		t.Fatalf("home %+v", posts)
	}

	// Posts: published and unlisted are served, drafts are not; old slugs move.
	if p, err := rd.Post(r.ctx, blog, "hidden"); err != nil || p.Body != "Body of Hidden" {
		t.Fatalf("unlisted %+v %v", p, err)
	}
	_, err = rd.Post(r.ctx, blog, "secret")
	hidden("draft", err)
	_, err = rd.Post(r.ctx, blog, "Not A Slug")
	hidden("bad slug", err)
	if _, err := rd.Post(r.ctx, blog, "first"); !errors.As(err, &mv) || mv.Slug != "first-renamed" {
		t.Fatalf("old slug: %v", err)
	}

	// A post-level take-down hides the post; a blog-level one the whole blog.
	rd.TakenDown = func(_ context.Context, _, slug string) (bool, error) { return slug == "second", nil }
	_, err = rd.Post(r.ctx, blog, "second")
	hidden("post taken down", err)
	if posts, _ := rd.Home(r.ctx, blog, HomePosts); len(posts) != 1 {
		t.Fatalf("home after take-down %+v", posts)
	}
	// A take-down follows the post when its slug changes: the taken-down
	// address neither serves nor redirects, and the new slug is hidden too.
	rd.TakenDown = func(_ context.Context, _, slug string) (bool, error) { return slug == "first", nil }
	_, err = rd.Post(r.ctx, blog, "first")
	hidden("taken-down old slug", err)
	_, err = rd.Post(r.ctx, blog, "first-renamed")
	hidden("renamed after take-down", err)
	if posts, _ := rd.Home(r.ctx, blog, HomePosts); len(posts) != 1 || posts[0].Title != "Second" {
		t.Fatalf("home after renamed take-down %+v", posts)
	}
	rd.TakenDown = func(_ context.Context, _, slug string) (bool, error) { return slug == "", nil }
	_, err = rd.Blog(r.ctx, "ann")
	hidden("blog taken down", err)
	rd.TakenDown = nil

	// A disabled owner or a suspended workspace hides the blog.
	yes := true
	if err := r.adm.UpdateUser(r.ctx, u, admin.UserPatch{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	_, err = rd.Blog(r.ctx, "ann")
	hidden("disabled", err)
	no := false
	_ = r.adm.UpdateUser(r.ctx, u, admin.UserPatch{Disabled: &no})
	st := admin.StatusSuspended
	if err := r.adm.UpdateWorkspace(r.ctx, b.WorkspaceID, admin.WorkspacePatch{Status: &st}); err != nil {
		t.Fatal(err)
	}
	_, err = rd.Blog(r.ctx, "ann")
	hidden("suspended", err)
	act := admin.StatusActive
	_ = r.adm.UpdateWorkspace(r.ctx, b.WorkspaceID, admin.WorkspacePatch{Status: &act})

	// An old username redirects while it is held.
	if err := r.adm.SetUsername(r.ctx, u, "anna", now, now.Add(90*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := rd.Blog(r.ctx, "ann"); !errors.As(err, &mv) || mv.Username != "anna" {
		t.Fatalf("old username: %v", err)
	}
	if _, err := rd.Blog(r.ctx, "anna"); err != nil {
		t.Fatalf("new username: %v", err)
	}
}

package blogs

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"abidata/internal/fault"
	"abidata/internal/records"
)

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Hello, World!":             "hello-world",
		"  Crème brûlée à la mode":  "creme-brulee-a-la-mode",
		"Straße & Søren":            "strasse-and-soren",
		"--a--b--":                  "a-b",
		"日本語":                       "",
		"ABC 123":                   "abc-123",
		strings.Repeat("word ", 40): strings.TrimSuffix(strings.Repeat("word-", 16), "-"),
	} {
		if got := Slugify(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	for _, s := range []string{"ok", "a-1", "x"} {
		if !ValidSlug(s) {
			t.Errorf("%q invalid", s)
		}
	}
	for _, s := range []string{"", "Upper", "a--b", "-a", "feed.xml", "_files", "rss", "new", strings.Repeat("a", 81)} {
		if ValidSlug(s) {
			t.Errorf("%q valid", s)
		}
	}
}

func TestPostRules(t *testing.T) {
	r := newRig(t)
	u := r.user(t, "ann@example.com", "ann")
	b, err := r.p.EnsureBlog(r.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	rec := &records.Service{Schemas: r.p.Schemas}
	rules := &Rules{Records: rec, Now: func() time.Time { return now }}
	rec.Hook = rules.Hook
	_, pins := r.schema(t, b.WorkspaceID)
	c := pins.Columns
	l, err := r.wm.Acquire(r.ctx, b.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	actor := records.Actor{UserID: u}
	insert := func(f map[string]any) records.Row {
		t.Helper()
		row, err := rec.Insert(r.ctx, l, pins.Posts, f, actor, nil)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}

	// The slug comes from the title, and is made unique.
	p1 := insert(map[string]any{c["title"]: "Hello, World"})
	if p1.Fields[c["slug"]] != "hello-world" || p1.Fields[c["status"]] != "draft" || p1.Fields[c["published_at"]] != nil {
		t.Fatalf("p1 %v", p1.Fields)
	}
	p2 := insert(map[string]any{c["title"]: "Hello world!"})
	if p2.Fields[c["slug"]] != "hello-world-2" {
		t.Fatalf("p2 slug %v", p2.Fields[c["slug"]])
	}
	// A typed slug is normalised; a reserved one is suffixed.
	p3 := insert(map[string]any{c["title"]: "X", c["slug"]: "  My Post "})
	if p3.Fields[c["slug"]] != "my-post" {
		t.Fatalf("p3 slug %v", p3.Fields[c["slug"]])
	}
	p4 := insert(map[string]any{c["title"]: "New", c["slug"]: "new"})
	if p4.Fields[c["slug"]] != "new-2" {
		t.Fatalf("p4 slug %v", p4.Fields[c["slug"]])
	}

	// Publishing sets published_at once.
	p1, err = rec.Update(r.ctx, l, pins.Posts, p1.ID, p1.Version, map[string]any{c["status"]: "published"}, actor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Fields[c["published_at"]] != "2026-05-01T09:00:00.000Z" {
		t.Fatalf("published_at %v", p1.Fields[c["published_at"]])
	}
	now = now.Add(time.Hour)
	p1, _ = rec.Update(r.ctx, l, pins.Posts, p1.ID, p1.Version, map[string]any{c["status"]: "unlisted"}, actor, nil)
	if p1.Fields[c["published_at"]] != "2026-05-01T09:00:00.000Z" {
		t.Fatalf("published_at moved: %v", p1.Fields[c["published_at"]])
	}

	// Renaming a public post's slug keeps a redirect; a draft's does not.
	p1, err = rec.Update(r.ctx, l, pins.Posts, p1.ID, p1.Version, map[string]any{c["slug"]: "greetings"}, actor, nil)
	if err != nil || p1.Fields[c["slug"]] != "greetings" {
		t.Fatalf("rename %v %v", p1.Fields, err)
	}
	if _, err := rec.Update(r.ctx, l, pins.Posts, p2.ID, p2.Version, map[string]any{c["slug"]: "draft-two"}, actor, nil); err != nil {
		t.Fatal(err)
	}
	redirects := func() map[string]int64 {
		rows, _, err := rec.List(r.ctx, l, pins.Redirects, records.ListQuery{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, row := range rows {
			out[row.Fields[pins.RedirectCols["old_slug"]].(string)], _ = strconv.ParseInt(fmt.Sprint(row.Fields[pins.RedirectCols["post_id"]]), 10, 64)
		}
		return out
	}
	if got := redirects(); len(got) != 1 || got["hello-world"] != p1.ID {
		t.Fatalf("redirects %v", got)
	}
	// Taking the old slug back removes its redirect.
	if _, err := rec.Update(r.ctx, l, pins.Posts, p1.ID, p1.Version, map[string]any{c["slug"]: "hello-world"}, actor, nil); err != nil {
		t.Fatal(err)
	}
	if got := redirects(); got["hello-world"] != 0 || got["greetings"] != p1.ID {
		t.Fatalf("redirects after taking back %v", got)
	}

	// The body size limit holds for every write path.
	if _, err := rec.Insert(r.ctx, l, pins.Posts, map[string]any{c["title"]: "Big", c["body_md"]: strings.Repeat("x", MaxBodySize+1)}, actor, nil); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("oversize body: %v", err)
	}
	// The status rule is the column's validation.
	if _, err := rec.Insert(r.ctx, l, pins.Posts, map[string]any{c["title"]: "Odd", c["status"]: "secret"}, actor, nil); fault.Classify(err).Code != fault.ValidationFailed {
		t.Fatalf("bad status: %v", err)
	}
}

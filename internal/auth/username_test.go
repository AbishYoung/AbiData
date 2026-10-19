package auth

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

func TestNormalizeUsername(t *testing.T) {
	for in, want := range map[string]string{"ada": "ada", "  Ada-Lovelace ": "ada-lovelace", "a1b2": "a1b2", "abc-1-x": "abc-1-x"} {
		got, err := NormalizeUsername(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "ab", "1abc", "-abc", "abc-", "ab--c", "ab_c", "ab.c", "ábc", "abcdefghijklmnopqrstuvwxyz12345",
		"admin", "API", "blog", "xn--abc", "w", "setup"} {
		if _, err := NormalizeUsername(in); code(err) != fault.BadRequest {
			t.Errorf("%q: accepted (%v)", in, err)
		}
	}
}

func TestDeriveUsername(t *testing.T) {
	taken := map[string]bool{"ada": true, "ada-2": true}
	free := func(n string) (bool, error) { return taken[n], nil }
	for email, want := range map[string]string{
		"ada@example.com":          "ada-3",
		"Grace.Hopper@example.com": "grace-hopper",
		"x@example.com":            "x00",
		"42@example.com":           "user",
		"__@example.com":           "user",
		"admin@example.com":        "admin-1",
		"o'brien+tag@example.com":  "o-brien-tag",
		"a-very-long-local-part-that-goes-on@example.com": "a-very-long-local-part-tha",
	} {
		got, err := DeriveUsername(email, free)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", email, got, err, want)
		}
		if _, err := NormalizeUsername(got); err != nil {
			t.Errorf("%s: derived invalid %q: %v", email, got, err)
		}
	}
}

// TestReservedCoversRouterSegments keeps every first path segment of the SPA
// router reserved, so no username can shadow an app route.
func TestReservedCoversRouterSegments(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "app", "router.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	ms := regexp.MustCompile(`path:\s*"/([^/"$]+)`).FindAllStringSubmatch(string(src), -1)
	if len(ms) == 0 {
		t.Fatal("no router paths found")
	}
	for _, m := range ms {
		if !IsReservedUsername(m[1]) {
			t.Errorf("router segment %q is not a reserved username", m[1])
		}
	}
}

func pw() string { return ids.Token(16) }

func TestCreateUserUsernames(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newSvc(t)
	u, err := s.CreateUser(ctx, "Ada@Example.com", "Ada", pw(), false)
	if err != nil || u.Username != "ada" {
		t.Fatalf("derived: %+v %v", u, err)
	}
	u2, err := s.CreateUser(ctx, "ada@other.example", "Ada 2", pw(), false)
	if err != nil || u2.Username != "ada-2" {
		t.Fatalf("suffixed: %+v %v", u2, err)
	}
	if _, err := s.CreateNamedUser(ctx, "b@example.com", "ADA", "B", pw(), false); code(err) != fault.DuplicateValue {
		t.Fatalf("taken name: %v", err)
	}
	if _, err := s.CreateNamedUser(ctx, "b@example.com", "bad_name", "B", pw(), false); code(err) != fault.BadRequest {
		t.Fatalf("invalid name: %v", err)
	}
	if _, err := s.CreateNamedUser(ctx, "ada@example.com", "fresh", "B", pw(), false); code(err) != fault.DuplicateValue {
		t.Fatalf("duplicate email: %v", err)
	}
	b, err := s.CreateNamedUser(ctx, "b@example.com", "Bee", "B", pw(), false)
	if err != nil || b.Username != "bee" {
		t.Fatalf("chosen: %+v %v", b, err)
	}
	got, err := st.GetUserByUsername(ctx, "BEE")
	if err != nil || got.ID != b.ID {
		t.Fatalf("lookup: %+v %v", got, err)
	}
}

func TestChangeUsernameRules(t *testing.T) {
	ctx := context.Background()
	s, st, c := newSvc(t)
	a, _ := s.CreateUser(ctx, "ann@example.com", "Ann", pw(), false)
	b, _ := s.CreateUser(ctx, "bob@example.com", "Bob", pw(), false)

	if _, err := s.ChangeUsername(ctx, a.ID, "bob", false); code(err) != fault.DuplicateValue {
		t.Fatalf("taken: %v", err)
	}
	if u, err := s.ChangeUsername(ctx, a.ID, "ann", false); err != nil || u.UsernameChangedAt != nil {
		t.Fatalf("same name is a no-op: %+v %v", u, err)
	}
	if _, err := s.ChangeUsername(ctx, a.ID, "annie", false); err != nil {
		t.Fatal(err)
	}
	// The old name is held for its user and redirects.
	if cur, err := st.ResolveOldUsername(ctx, "ann", c.now()); err != nil || cur != "annie" {
		t.Fatalf("redirect: %q %v", cur, err)
	}
	if _, err := s.ChangeUsername(ctx, b.ID, "ann", true); code(err) != fault.DuplicateValue {
		t.Fatalf("held name taken by another: %v", err)
	}
	if _, err := s.CreateNamedUser(ctx, "c@example.com", "ann", "C", pw(), false); code(err) != fault.DuplicateValue {
		t.Fatalf("held name on create: %v", err)
	}
	// Once per 30 days for the user; an admin is not limited.
	c.t = c.t.Add(29 * 24 * time.Hour)
	if _, err := s.ChangeUsername(ctx, a.ID, "anna", false); code(err) != fault.BadRequest {
		t.Fatalf("too soon: %v", err)
	}
	if _, err := s.ChangeUsername(ctx, a.ID, "ann", true); err != nil {
		t.Fatalf("admin takes back the old name: %v", err)
	}
	if _, err := st.ResolveOldUsername(ctx, "ann", c.now()); err == nil {
		t.Fatal("a name in use again still redirects")
	}
	// After the hold the name is released.
	c.t = c.t.Add(UsernameHold + time.Hour)
	if _, err := s.ChangeUsername(ctx, b.ID, "annie", false); err != nil {
		t.Fatalf("released name: %v", err)
	}
}

func TestBackfillUsernames(t *testing.T) {
	ctx := context.Background()
	s, st, _ := newSvc(t)
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, email := range []string{"sam@example.com", "sam@other.example", "admin@example.com"} {
		if err := st.CreateUser(ctx, admin.User{ID: ids.New(), Email: email, DisplayName: "S", PasswordHash: "h",
			CreatedAt: t0.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.BackfillUsernames(ctx)
	if err != nil || n != 3 {
		t.Fatalf("backfill: %d %v", n, err)
	}
	us, _ := st.ListUsers(ctx)
	got := map[string]string{}
	for _, u := range us {
		got[u.Email] = u.Username
	}
	want := map[string]string{"sam@example.com": "sam", "sam@other.example": "sam-2", "admin@example.com": "admin-1"}
	for e, w := range want {
		if got[e] != w {
			t.Errorf("%s: %q, want %q", e, got[e], w)
		}
	}
	if n, err := s.BackfillUsernames(ctx); err != nil || n != 0 {
		t.Fatalf("second run: %d %v", n, err)
	}
}

package admin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/pgtest"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// storeOpener returns a function that opens the same store again each time:
// on SQLite a file, on PostgreSQL (ABIDATA_TEST_DB=postgres, ADR 0012 §10) a
// throwaway database's admin schema.
func storeOpener(t *testing.T) func() (*Store, error) {
	t.Helper()
	if os.Getenv("ABIDATA_TEST_DB") == "postgres" {
		e := pgtest.New(t)
		n := pgdb.Names{Prefix: e.Prefix}
		return func() (*Store, error) { return OpenPostgres(context.Background(), e.App.Config(), n) }
	}
	path := filepath.Join(t.TempDir(), "admin.db")
	return func() (*Store, error) { return Open(context.Background(), path, "NORMAL") }
}

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := storeOpener(t)()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mkUser(t *testing.T, s *Store, id, email string) User {
	t.Helper()
	u := User{ID: id, Email: email, DisplayName: "N " + id, PasswordHash: "h", CreatedAt: t0}
	if err := s.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func mkWS(t *testing.T, s *Store, name, owner string) Workspace {
	t.Helper()
	w := Workspace{ID: ids.NewWorkspaceID(), Name: name, Status: StatusActive, CreatedAt: t0, CreatedBy: owner}
	if err := s.CreateWorkspace(context.Background(), w, owner); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	u := mkUser(t, s, "u1", "Ann@Example.com")
	mkUser(t, s, "u2", "bob@example.com")

	if err := s.CreateUser(ctx, User{ID: "u3", Email: "ANN@example.com", CreatedAt: t0}); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup email: %v", err)
	}
	if err := s.CreateUser(ctx, User{ID: "u1", Email: "other@example.com", CreatedAt: t0}); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup id: %v", err)
	}
	got, err := s.GetUser(ctx, "u1")
	if err != nil || !reflect.DeepEqual(got, u) {
		t.Fatalf("GetUser = %+v, %v; want %+v", got, err, u)
	}
	got, err = s.GetUserByEmail(ctx, "ann@EXAMPLE.com")
	if err != nil || got.ID != "u1" {
		t.Fatalf("by email: %+v %v", got, err)
	}
	if _, err := s.GetUser(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.GetUserByEmail(ctx, "nope@x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	list, err := s.ListUsers(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "u1" || list[1].ID != "u2" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if n, err := s.CountUsers(ctx); err != nil || n != 2 {
		t.Fatalf("count %d %v", n, err)
	}

	name, yes := "Annie", true
	if err := s.UpdateUser(ctx, "u1", UserPatch{DisplayName: &name, IsPlatformAdmin: &yes}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetUser(ctx, "u1")
	if got.DisplayName != "Annie" || !got.IsPlatformAdmin || got.Disabled {
		t.Fatalf("after patch: %+v", got)
	}
	if err := s.UpdateUser(ctx, "u1", UserPatch{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetUser(ctx, "u1")
	if got.DisplayName != "Annie" || !got.IsPlatformAdmin || !got.Disabled {
		t.Fatalf("patch must keep other fields: %+v", got)
	}
	if err := s.UpdateUser(ctx, "nope", UserPatch{Disabled: &yes}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	if err := s.SetPasswordHash(ctx, "u1", "newhash"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetUser(ctx, "u1")
	if got.PasswordHash != "newhash" {
		t.Fatal("hash not updated")
	}
	if err := s.SetPasswordHash(ctx, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	at := t0.Add(time.Hour)
	if err := s.TouchLogin(ctx, "u1", at); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetUser(ctx, "u1")
	if got.LastLoginAt == nil || !got.LastLoginAt.Equal(at) {
		t.Fatalf("last login %v", got.LastLoginAt)
	}
	if err := s.TouchLogin(ctx, "nope", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "a@x.com")
	mkUser(t, s, "u2", "b@x.com")

	mk := func(hash, user string, seen, exp time.Time) Session {
		se := Session{IDHash: hash, UserID: user, CSRFToken: "csrf-" + hash, CreatedAt: t0, LastSeenAt: seen, ExpiresAt: exp, IP: "1.2.3.4", UserAgent: "UA"}
		if err := s.CreateSession(ctx, se); err != nil {
			t.Fatal(err)
		}
		return se
	}
	now := t0.Add(10 * time.Hour)
	fresh := mk("fresh", "u1", now.Add(-time.Minute), now.Add(time.Hour))
	mk("idle", "u1", now.Add(-3*time.Hour), now.Add(time.Hour))
	mk("expired", "u2", now.Add(-time.Minute), now)
	mk("other", "u2", now.Add(-time.Minute), now.Add(time.Hour))

	if err := s.CreateSession(ctx, fresh); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup session: %v", err)
	}
	if err := s.CreateSession(ctx, Session{IDHash: "orphan", UserID: "ghost", CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0}); err == nil {
		t.Fatal("expected FK failure")
	}
	got, err := s.GetSession(ctx, "fresh")
	if err != nil || !reflect.DeepEqual(got, fresh) {
		t.Fatalf("GetSession = %+v, %v", got, err)
	}
	if _, err := s.GetSession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	later := now.Add(time.Second)
	if err := s.TouchSession(ctx, "fresh", later); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSession(ctx, "fresh")
	if !got.LastSeenAt.Equal(later) {
		t.Fatalf("last seen %v", got.LastSeenAt)
	}
	if err := s.TouchSession(ctx, "nope", later); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	// idle=2h: "idle" (3h) purged by idleness, "expired" by absolute expiry.
	n, err := s.PurgeSessions(ctx, now, 2*time.Hour)
	if err != nil || n != 2 {
		t.Fatalf("purge = %d, %v", n, err)
	}
	for _, h := range []string{"idle", "expired"} {
		if _, err := s.GetSession(ctx, h); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s should be purged: %v", h, err)
		}
	}
	for _, h := range []string{"fresh", "other"} {
		if _, err := s.GetSession(ctx, h); err != nil {
			t.Fatalf("%s should remain: %v", h, err)
		}
	}
	if n, _ := s.PurgeSessions(ctx, now, 2*time.Hour); n != 0 {
		t.Fatalf("second purge = %d", n)
	}

	if err := s.DeleteSession(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if err := s.DeleteUserSessions(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, "fresh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if err := s.DeleteUserSessions(ctx, "u1"); err != nil {
		t.Fatalf("deleting none must succeed: %v", err)
	}
}

func TestLoginAttempts(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	const win = 15 * time.Minute

	la, err := s.GetLoginAttempts(ctx, "k")
	if err != nil || la != (LoginAttempts{}) {
		t.Fatalf("absent = %+v, %v", la, err)
	}
	la, err = s.RecordLoginFailure(ctx, "k", t0, win)
	if err != nil || la.Count != 1 || !la.WindowStart.Equal(t0) || la.Key != "k" {
		t.Fatalf("first = %+v, %v", la, err)
	}
	la, _ = s.RecordLoginFailure(ctx, "k", t0.Add(5*time.Minute), win)
	if la.Count != 2 || !la.WindowStart.Equal(t0) {
		t.Fatalf("second = %+v", la)
	}
	got, _ := s.GetLoginAttempts(ctx, "k")
	if !reflect.DeepEqual(got, la) {
		t.Fatalf("get = %+v want %+v", got, la)
	}
	// Exactly at the window edge is still inside it.
	la, _ = s.RecordLoginFailure(ctx, "k", t0.Add(win), win)
	if la.Count != 3 || !la.WindowStart.Equal(t0) {
		t.Fatalf("edge = %+v", la)
	}
	// Rollover.
	roll := t0.Add(win + time.Second)
	la, _ = s.RecordLoginFailure(ctx, "k", roll, win)
	if la.Count != 1 || !la.WindowStart.Equal(roll) {
		t.Fatalf("rollover = %+v", la)
	}
	other, _ := s.RecordLoginFailure(ctx, "other", roll, win)
	if other.Count != 1 {
		t.Fatalf("keys must be independent: %+v", other)
	}
	if err := s.ClearLoginAttempts(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetLoginAttempts(ctx, "k"); got != (LoginAttempts{}) {
		t.Fatalf("after clear: %+v", got)
	}
	if err := s.ClearLoginAttempts(ctx, "k"); err != nil {
		t.Fatalf("clearing absent key: %v", err)
	}
}

func TestWorkspacesAndMemberships(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "a@x.com")
	mkUser(t, s, "u2", "b@x.com")

	q := Quota{MaxPageCount: 50, MaxConcurrentJobs: 3, ArtifactBytes: 1 << 40}
	zeta := Workspace{ID: ids.NewWorkspaceID(), Name: "zeta", Status: StatusActive, Quota: q, CreatedAt: t0, CreatedBy: "u1"}
	if err := s.CreateWorkspace(ctx, zeta, "u1"); err != nil {
		t.Fatal(err)
	}
	alpha := mkWS(t, s, "Alpha", "u1")
	dup := Workspace{ID: ids.NewWorkspaceID(), Name: "ALPHA", Status: StatusActive, CreatedAt: t0, CreatedBy: "u2"}
	if err := s.CreateWorkspace(ctx, dup, "u2"); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup name: %v", err)
	}
	// The failed create must not leave a membership behind.
	if _, err := s.GetRole(ctx, dup.ID, "u2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("leaked membership: %v", err)
	}
	// Unknown owner rolls the workspace back too.
	bad := Workspace{ID: ids.NewWorkspaceID(), Name: "bad", Status: StatusActive, CreatedAt: t0, CreatedBy: "ghost"}
	if err := s.CreateWorkspace(ctx, bad, "ghost"); err == nil {
		t.Fatal("expected FK failure for unknown owner")
	}
	if _, err := s.GetWorkspace(ctx, bad.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("workspace not rolled back: %v", err)
	}

	got, err := s.GetWorkspace(ctx, zeta.ID)
	if err != nil || !reflect.DeepEqual(got, zeta) {
		t.Fatalf("GetWorkspace = %+v, %v; want %+v", got, err, zeta)
	}
	if _, err := s.GetWorkspace(ctx, ids.NewWorkspaceID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if role, err := s.GetRole(ctx, zeta.ID, "u1"); err != nil || role != RoleOwner {
		t.Fatalf("owner role = %q, %v", role, err)
	}

	list, err := s.ListWorkspaces(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "Alpha" || list[1].Name != "zeta" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	// Update.
	nn, st, nq := "Beta", StatusSuspended, Quota{MaxPageCount: 7}
	if err := s.UpdateWorkspace(ctx, alpha.ID, WorkspacePatch{Name: &nn, Status: &st, Quota: &nq}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetWorkspace(ctx, alpha.ID)
	if got.Name != "Beta" || got.Status != StatusSuspended || got.Quota != nq {
		t.Fatalf("after update: %+v", got)
	}
	if err := s.UpdateWorkspace(ctx, alpha.ID, WorkspacePatch{}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetWorkspace(ctx, alpha.ID)
	if got.Name != "Beta" || got.Quota != nq {
		t.Fatalf("empty patch changed data: %+v", got)
	}
	clash := "ZETA"
	if err := s.UpdateWorkspace(ctx, alpha.ID, WorkspacePatch{Name: &clash}); !errors.Is(err, ErrConflict) {
		t.Fatalf("rename clash: %v", err)
	}
	if err := s.UpdateWorkspace(ctx, ids.NewWorkspaceID(), WorkspacePatch{Name: &nn}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	// Memberships.
	if err := s.SetMembership(ctx, Membership{WorkspaceID: zeta.ID, UserID: "u2", Role: RoleViewer}); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.GetRole(ctx, zeta.ID, "u2"); role != RoleViewer {
		t.Fatalf("role %q", role)
	}
	if err := s.SetMembership(ctx, Membership{WorkspaceID: zeta.ID, UserID: "u2", Role: RoleOwner}); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.GetRole(ctx, zeta.ID, "u2"); role != RoleOwner {
		t.Fatalf("upsert did not change role: %q", role)
	}
	if n, err := s.CountOwners(ctx, zeta.ID); err != nil || n != 2 {
		t.Fatalf("owners = %d, %v", n, err)
	}
	if err := s.SetMembership(ctx, Membership{WorkspaceID: zeta.ID, UserID: "u2", Role: RoleEditor}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountOwners(ctx, zeta.ID); n != 1 {
		t.Fatalf("owners = %d", n)
	}
	if n, err := s.CountOwners(ctx, ids.NewWorkspaceID()); err != nil || n != 0 {
		t.Fatalf("owners of unknown ws = %d, %v", n, err)
	}
	if err := s.SetMembership(ctx, Membership{WorkspaceID: zeta.ID, UserID: "u2", Role: "bogus"}); err == nil {
		t.Fatal("invalid role must be rejected")
	}

	members, err := s.ListMembers(ctx, zeta.ID)
	want := []Member{
		{UserID: "u1", Email: "a@x.com", DisplayName: "N u1", Role: RoleOwner},
		{UserID: "u2", Email: "b@x.com", DisplayName: "N u2", Role: RoleEditor},
	}
	if err != nil || !reflect.DeepEqual(members, want) {
		t.Fatalf("members = %+v, %v", members, err)
	}

	// Candidates for zeta (users not yet in zeta).
	cands, err := s.ListCandidateMembers(ctx, zeta.ID)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	// u1 and u2 are in zeta, so candidates must not contain u1 or u2.
	for _, c := range cands {
		if c.UserID == "u1" || c.UserID == "u2" {
			t.Fatalf("unexpected member in candidates: %+v", c)
		}
	}

	// Per-user list.
	acc, err := s.ListWorkspacesForUser(ctx, "u2")
	if err != nil || len(acc) != 1 || acc[0].Workspace.ID != zeta.ID || acc[0].Role != RoleEditor {
		t.Fatalf("u2 access = %+v, %v", acc, err)
	}
	acc, _ = s.ListWorkspacesForUser(ctx, "u1")
	if len(acc) != 2 || acc[0].Workspace.Name != "Beta" || acc[1].Workspace.Name != "zeta" {
		t.Fatalf("u1 access = %+v", acc)
	}

	// Deleted workspaces are hidden from lists but still gettable.
	del := StatusDeleted
	if err := s.UpdateWorkspace(ctx, alpha.ID, WorkspacePatch{Status: &del}); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListWorkspaces(ctx); len(l) != 1 || l[0].ID != zeta.ID {
		t.Fatalf("deleted visible in ListWorkspaces: %+v", l)
	}
	if a, _ := s.ListWorkspacesForUser(ctx, "u1"); len(a) != 1 || a[0].Workspace.ID != zeta.ID {
		t.Fatalf("deleted visible in ListWorkspacesForUser: %+v", a)
	}
	if w, err := s.GetWorkspace(ctx, alpha.ID); err != nil || w.Status != StatusDeleted {
		t.Fatalf("get deleted: %+v %v", w, err)
	}

	// Delete membership.
	if err := s.DeleteMembership(ctx, zeta.ID, "u2"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMembership(ctx, zeta.ID, "u2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.GetRole(ctx, zeta.ID, "u2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.GetSetting(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	if m, err := s.ListSettings(ctx); err != nil || len(m) != 1 || m[settingBackend] == "" {
		t.Fatalf("a new store holds only its backend: %v %v", m, err)
	}
	if err := s.PutSetting(ctx, "k", `{"a":1}`); err != nil {
		t.Fatal(err)
	}
	if v, err := s.GetSetting(ctx, "k"); err != nil || v != `{"a":1}` {
		t.Fatalf("got %q %v", v, err)
	}
	if err := s.PutSetting(ctx, "k", `"two"`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSetting(ctx, "j", `true`); err != nil {
		t.Fatal(err)
	}
	m, err := s.ListSettings(ctx)
	if want := map[string]string{"k": `"two"`, "j": `true`, settingBackend: m[settingBackend]}; err != nil || !reflect.DeepEqual(m, want) {
		t.Fatalf("list = %v, %v", m, err)
	}
}

func TestAudit(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "a@x.com")

	if l, err := s.ListAudit(ctx, 10); err != nil || len(l) != 0 {
		t.Fatalf("empty: %v %v", l, err)
	}
	e1 := AuditEntry{At: t0, UserID: "u1", Action: "login", ObjectKind: "user", ObjectID: "u1",
		Details: map[string]any{"ip": "1.2.3.4", "n": float64(3), "nested": map[string]any{"x": true}}}
	e2 := AuditEntry{At: t0.Add(time.Minute), Action: "system.start"}
	e3 := AuditEntry{At: t0.Add(2 * time.Minute), Action: "ws.create", ObjectKind: "workspace"}
	e4 := AuditEntry{At: t0.Add(2 * time.Minute), Action: "ws.rename"} // same instant as e3
	for _, e := range []AuditEntry{e1, e2, e3, e4} {
		if err := s.Audit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	l, err := s.ListAudit(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []AuditEntry{e4, e3, e2, e1}; !reflect.DeepEqual(l, want) {
		t.Fatalf("audit = %+v\nwant   %+v", l, want)
	}
	if l[2].Details != nil || l[2].UserID != "" {
		t.Fatalf("empty fields must round-trip as zero: %+v", l[2])
	}
	if l, _ := s.ListAudit(ctx, 2); len(l) != 2 || l[0].Action != "ws.rename" {
		t.Fatalf("limit: %+v", l)
	}
	if err := s.Audit(ctx, AuditEntry{At: t0, UserID: "u1", Action: "bad", Details: map[string]any{"c": make(chan int)}}); err == nil {
		t.Fatal("unmarshalable details must fail")
	}
}

func TestReopenKeepsData(t *testing.T) {
	ctx := context.Background()
	open := storeOpener(t)
	s, err := open()
	if err != nil {
		t.Fatal(err)
	}
	mkUser(t, s, "u1", "a@x.com")
	w := mkWS(t, s, "keep", "u1")
	if err := s.PutSetting(ctx, "k", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = open()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("users = %d", n)
	}
	if got, err := s.GetWorkspace(ctx, w.ID); err != nil || got.Name != "keep" {
		t.Fatalf("workspace: %+v %v", got, err)
	}
	if role, err := s.GetRole(ctx, w.ID, "u1"); err != nil || role != RoleOwner {
		t.Fatalf("role: %q %v", role, err)
	}
	if v, err := s.GetSetting(ctx, "k"); err != nil || v != "1" {
		t.Fatalf("setting: %q %v", v, err)
	}
}

// The store records its backend on first open and refuses to open as the
// other one (ADR 0012 §1).
func TestBackendLock(t *testing.T) {
	ctx := context.Background()
	open := storeOpener(t)
	s, err := open()
	if err != nil {
		t.Fatal(err)
	}
	want := "sqlite"
	if s.pg != nil {
		want = "postgres"
	}
	if got, err := s.GetSetting(ctx, settingBackend); err != nil || got != `"`+want+`"` {
		t.Fatalf("backend = %q, %v; want %q", got, err, want)
	}
	if err := s.PutSetting(ctx, settingBackend, `"elsewhere"`); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if s, err := open(); !errors.Is(err, ErrBackendMismatch) {
		if s != nil {
			_ = s.Close()
		}
		t.Fatalf("reopen as %s of a store recorded for another backend: %v", want, err)
	}
}

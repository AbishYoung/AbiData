package admin

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"abidata/internal/ids"
)

func mkKey(ws Workspace, name string, role Role, at time.Time) APIKey {
	return APIKey{ID: ids.New(), WorkspaceID: ws.ID, Name: name, Role: role, SecretHash: make([]byte, 32), CreatedBy: "u1", CreatedAt: at}
}

func TestAPIKeys(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	w := mkWS(t, s, "W", "u1")
	other := mkWS(t, s, "Other", "u1")
	now := time.Now()

	k := mkKey(w, "bot", RoleEditor, now)
	exp := now.Add(time.Hour)
	k.ExpiresAt = &exp
	if err := s.CreateAPIKey(ctx, k); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAPIKey(ctx, k.ID)
	if err != nil || got.Name != "bot" || got.Role != RoleEditor || got.CreatedByName != "N u1" || got.ExpiresAt == nil || !got.Active(now) || got.Active(now.Add(2*time.Hour)) {
		t.Fatalf("get %+v %v", got, err)
	}
	if _, err := s.GetAPIKey(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// The owner role is refused by the table itself.
	if err := s.CreateAPIKey(ctx, mkKey(w, "bad", RoleOwner, now)); err == nil {
		t.Fatal("owner key stored")
	}
	// Listing is per workspace, newest first; "" lists all.
	if err := s.CreateAPIKey(ctx, mkKey(other, "o", RoleViewer, now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if ks, _ := s.ListAPIKeys(ctx, w.ID); len(ks) != 1 || ks[0].ID != k.ID {
		t.Fatalf("list w: %v", ks)
	}
	if ks, _ := s.ListAPIKeys(ctx, ""); len(ks) != 2 {
		t.Fatalf("list all: %v", ks)
	}
	// Touch and revoke (twice: the first revocation time is kept).
	if err := s.TouchAPIKey(ctx, k.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAPIKey(ctx, k.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAPIKey(ctx, k.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetAPIKey(ctx, k.ID)
	if got.RevokedAt == nil || got.RevokedAt.Sub(now) > time.Second || got.LastUsedAt == nil || got.Active(now) {
		t.Fatalf("after revoke %+v", got)
	}
	if err := s.RevokeAPIKey(ctx, "nope", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
}

func TestAPIKeyCapCountsOnlyActiveKeys(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	w := mkWS(t, s, "W", "u1")
	now := time.Now()
	var first APIKey
	for i := range MaxActiveAPIKeys {
		k := mkKey(w, fmt.Sprintf("k%d", i), RoleViewer, now)
		if i == 0 {
			first = k
		}
		if err := s.CreateAPIKey(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateAPIKey(ctx, mkKey(w, "extra", RoleViewer, now)); !errors.Is(err, ErrTooManyKeys) {
		t.Fatalf("cap: %v", err)
	}
	// Another workspace has its own budget.
	w2 := mkWS(t, s, "W2", "u1")
	if err := s.CreateAPIKey(ctx, mkKey(w2, "x", RoleViewer, now)); err != nil {
		t.Fatal(err)
	}
	// Revoked keys free a slot; old revoked keys are purged on the next create.
	if err := s.RevokeAPIKey(ctx, first.ID, now.Add(-100*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAPIKey(ctx, mkKey(w, "fits", RoleViewer, now)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAPIKey(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale revoked key not purged: %v", err)
	}
}

func TestPurgeSessionsDropsStaleThrottleCounters(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Now()
	if _, err := s.RecordLoginFailure(ctx, "apikey-ip:old", now.Add(-3*time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordLoginFailure(ctx, "apikey-ip:new", now, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurgeSessions(ctx, now, time.Hour); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.GetLoginAttempts(ctx, "apikey-ip:old"); a.Count != 0 {
		t.Fatalf("old counter kept: %+v", a)
	}
	if a, _ := s.GetLoginAttempts(ctx, "apikey-ip:new"); a.Count != 1 {
		t.Fatalf("fresh counter lost: %+v", a)
	}
}

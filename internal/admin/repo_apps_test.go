package admin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"abidata/internal/ids"
)

func mkApp(ws Workspace, name string, at time.Time) App {
	tok := ids.New() + name
	h := sha256.Sum256([]byte(tok))
	return App{ID: ids.New(), WorkspaceID: ws.ID, Name: name, Token: tok, TokenHash: h[:], Enabled: true, CreatedBy: "u1", CreatedAt: at, UpdatedAt: at,
		Items: []AppItem{{ID: "i1", Kind: "form", ObjectID: "F1", Title: "Sign up", Fingerprint: "f:1"}, {ID: "i2", Kind: "report", ObjectID: "R1", Title: "List", Fingerprint: "r:1"}}}
}

func TestApps(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	w := mkWS(t, s, "W", "u1")
	other := mkWS(t, s, "Other", "u1")
	now := time.Now().UTC().Truncate(time.Millisecond)

	a := mkApp(w, "Volunteers", now)
	a.StartItem = "i1"
	if err := s.CreateApp(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetApp(ctx, a.ID)
	if err != nil || got.Name != "Volunteers" || got.CreatedByName != "N u1" || got.StartItem != "i1" || !got.Enabled || got.TakenDownAt != nil {
		t.Fatalf("get %+v %v", got, err)
	}
	if len(got.Items) != 2 || got.Items[0].ID != "i1" || got.Items[1].Kind != "report" || got.Items[1].Fingerprint != "r:1" {
		t.Fatalf("items %+v", got.Items)
	}
	if it, ok := got.Item("i2"); !ok || it.Title != "List" {
		t.Fatalf("item lookup %+v %v", it, ok)
	}
	byTok, err := s.GetAppByTokenHash(ctx, a.TokenHash)
	if err != nil || byTok.ID != a.ID {
		t.Fatalf("by token %+v %v", byTok, err)
	}
	if _, err := s.GetAppByTokenHash(ctx, make([]byte, 32)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token: %v", err)
	}

	// The owner's update reorders items and never lifts a take-down or changes the link.
	down := now.Add(time.Minute)
	if err := s.SetAppTakenDown(ctx, a.ID, &down, "admin1"); err != nil {
		t.Fatal(err)
	}
	upd := got
	upd.Name, upd.Enabled, upd.UpdatedAt = "Helpers", false, now.Add(2*time.Minute)
	upd.Items = []AppItem{got.Items[1]}
	upd.TakenDownAt, upd.Token = nil, "ignored"
	if err := s.UpdateApp(ctx, upd); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetApp(ctx, a.ID)
	if got.Name != "Helpers" || got.Enabled || len(got.Items) != 1 || got.Items[0].ID != "i2" || got.TakenDownAt == nil || got.TakenDownBy != "admin1" || got.Token != a.Token {
		t.Fatalf("after update %+v", got)
	}
	if err := s.SetAppTakenDown(ctx, a.ID, nil, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetApp(ctx, a.ID); got.TakenDownAt != nil || got.TakenDownBy != "" {
		t.Fatalf("restore %+v", got)
	}

	// Rotating the link: the old hash no longer finds the app.
	newHash := sha256.Sum256([]byte("fresh"))
	if err := s.SetAppToken(ctx, a.ID, "fresh", newHash[:], now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAppByTokenHash(ctx, a.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old link still works: %v", err)
	}

	// Listing per workspace and across workspaces.
	b := mkApp(other, "Shop", now)
	if err := s.CreateApp(ctx, b); err != nil {
		t.Fatal(err)
	}
	mine, _ := s.ListApps(ctx, w.ID)
	all, _ := s.ListApps(ctx, "")
	if len(mine) != 1 || len(all) != 2 || len(all[1].Items) != 2 {
		t.Fatalf("lists %d %d", len(mine), len(all))
	}

	// The cap counts apps in the workspace.
	for i := 1; i < MaxAppsPerWorkspace; i++ {
		if err := s.CreateApp(ctx, mkApp(w, fmt.Sprintf("a%d", i), now)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateApp(ctx, mkApp(w, "extra", now)); !errors.Is(err, ErrTooManyApps) {
		t.Fatalf("cap: %v", err)
	}

	// Deleting an app removes its items; deleting a workspace removes its apps.
	if err := s.DeleteApp(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteApp(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

package admin

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"abidata/internal/ids"
)

// Regression (Phase 0 review #9): concurrent owner demotions keep one owner.
func TestChangeMembershipKeepsLastOwner(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "a.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for _, id := range []string{"u1", "u2"} {
		if err := s.CreateUser(ctx, User{ID: id, Email: id + "@x.test", DisplayName: id, PasswordHash: "h", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	ws := ids.NewWorkspaceID()
	if err := s.CreateWorkspace(ctx, Workspace{ID: ws, Name: "W", Status: StatusActive, CreatedAt: time.Now(), CreatedBy: "u1"}, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembership(ctx, Membership{WorkspaceID: ws, UserID: "u2", Role: RoleOwner}); err != nil {
		t.Fatal(err)
	}
	editor := RoleEditor
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, u := range []string{"u1", "u2"} {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = s.ChangeMembership(ctx, ws, u, &editor) }()
	}
	wg.Wait()
	if n, _ := s.CountOwners(ctx, ws); n != 1 {
		t.Fatalf("owners = %d (errs %v)", n, errs)
	}
	if !errors.Is(errs[0], ErrLastOwner) && !errors.Is(errs[1], ErrLastOwner) {
		t.Fatal("neither change was refused")
	}
	if err := s.ChangeMembership(ctx, ws, "nobody", nil); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

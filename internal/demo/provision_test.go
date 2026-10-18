package demo_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/demo"
	"abidata/internal/ids"
)

func TestProvisionAllGivesEveryActiveUserTheirOwnDemo(t *testing.T) {
	r := newRig(t)
	// The setup administrator already owns the shared demo.
	if _, err := r.b.Create(r.ctx, demo.Traders, r.owner); err != nil {
		t.Fatal(err)
	}
	users := map[string]admin.User{}
	for _, u := range []admin.User{
		{Email: "ada@example.com", DisplayName: "Ada", IsPlatformAdmin: true},
		{Email: "bo@example.com", DisplayName: "Bo"},
		{Email: "gone@example.com", DisplayName: "Gone", Disabled: true},
	} {
		u.ID, u.CreatedAt = ids.New(), time.Now()
		if err := r.adm.CreateUser(r.ctx, u); err != nil {
			t.Fatal(err)
		}
		users[u.Email] = u
	}
	// Bo is a viewer of the shared demo: that is not owning one.
	shared, _ := r.adm.ListWorkspaces(r.ctx)
	if err := r.adm.SetMembership(r.ctx, admin.Membership{WorkspaceID: shared[0].ID, UserID: users["bo@example.com"].ID, Role: admin.RoleViewer}); err != nil {
		t.Fatal(err)
	}

	got, err := r.b.ProvisionAll(r.ctx, demo.Traders)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got.Created)
	if !slices.Equal(got.Created, []string{"ada@example.com", "bo@example.com"}) || !slices.Equal(got.Skipped, []string{"owner@example.com"}) || len(got.Failed) != 0 {
		t.Fatalf("ProvisionAll = %+v", got)
	}
	for _, email := range got.Created {
		u := users[email]
		access, err := r.adm.ListWorkspacesForUser(r.ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		var mine []string
		for _, a := range access {
			if a.Role == admin.RoleOwner {
				mine = append(mine, a.Workspace.Name)
			}
		}
		if want := "Northwind Traders (" + u.DisplayName + ")"; !slices.Equal(mine, []string{want}) {
			t.Errorf("%s owns %q, want [%q]", email, mine, want)
		}
	}
	// Each copy is a full demo, not an empty workspace.
	for _, w := range mustList(t, r) {
		if !strings.HasPrefix(w.Name, "Northwind Traders (") {
			continue
		}
		l, err := r.wm.Acquire(r.ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if n := count(t, r, l, `SELECT count(*) FROM _sys_tables`); n < 5 {
			t.Errorf("%s has %d tables, want the full demo", w.Name, n)
		}
		l.Release()
	}

	// Running again changes nothing.
	again, err := r.b.ProvisionAll(r.ctx, demo.Traders)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Created) != 0 || len(again.Skipped) != 3 {
		t.Fatalf("second ProvisionAll = %+v, want everyone skipped", again)
	}
}

func TestProvisionAllFallsBackWhenTheNameIsTaken(t *testing.T) {
	r := newRig(t)
	// Someone else's workspace already has the owner's personal name.
	other := admin.User{ID: ids.New(), Email: "x@example.com", DisplayName: "X", CreatedAt: time.Now()}
	if err := r.adm.CreateUser(r.ctx, other); err != nil {
		t.Fatal(err)
	}
	taken := admin.Workspace{ID: ids.NewWorkspaceID(), Name: "Northwind Traders (Owner)", Status: admin.StatusActive, CreatedAt: time.Now(), CreatedBy: other.ID}
	if err := r.adm.CreateWorkspace(r.ctx, taken, other.ID); err != nil {
		t.Fatal(err)
	}
	got, err := r.b.ProvisionAll(r.ctx, demo.Traders)
	if err != nil {
		t.Fatal(err)
	}
	// X owns a demo-named workspace, so X is skipped; the owner gets one under another name.
	if !slices.Equal(got.Created, []string{"owner@example.com"}) || !slices.Equal(got.Skipped, []string{"x@example.com"}) {
		t.Fatalf("ProvisionAll = %+v, want owner provisioned under another name and x skipped", got)
	}
	found := false
	for _, w := range mustList(t, r) {
		if w.Name == "Northwind Traders (owner@example.com)" {
			found = true
		}
	}
	if !found {
		t.Error("want the owner's copy named after their email when their display name is taken")
	}
}

func TestIsDemoName(t *testing.T) {
	for name, want := range map[string]bool{
		"Northwind Traders":              true,
		"northwind traders":              true,
		"Northwind Traders (Ada)":        true,
		"Northwind Traders (failed 01H)": true,
		"Northwind Traders Ltd":          false,
		"Northwind Traders ()":           false,
		"My Northwind Traders":           false,
		"Northwind Invasives (Ada)":      false,
	} {
		if got := demo.Traders.IsDemoName(name); got != want {
			t.Errorf("Traders.IsDemoName(%q) = %v, want %v", name, got, want)
		}
	}
	if !demo.Invasives.IsDemoName("Northwind Invasives (Ada)") || demo.Invasives.IsDemoName("Northwind Traders (Ada)") {
		t.Error("Invasives.IsDemoName must match only Northwind Invasives copies")
	}
}

// TestProvisionAllIsPerSample: owning one sample does not count as owning the
// other, and each sample's copies get that sample's name.
func TestProvisionAllIsPerSample(t *testing.T) {
	r := newRig(t)
	if _, err := r.b.Create(r.ctx, demo.Traders, r.owner); err != nil {
		t.Fatal(err)
	}
	got, err := r.b.ProvisionAll(r.ctx, demo.Invasives)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Created, []string{"owner@example.com"}) || len(got.Skipped) != 0 {
		t.Fatalf("ProvisionAll(invasives) = %+v, want the Traders owner to get an Invasives copy", got)
	}
	found := false
	for _, w := range mustList(t, r) {
		found = found || w.Name == "Northwind Invasives (Owner)"
	}
	if !found {
		t.Error(`want a workspace named "Northwind Invasives (Owner)"`)
	}
	if _, err := r.b.ProvisionAll(r.ctx, demo.Kind("")); err == nil {
		t.Error("an empty kind must be refused")
	}
}

func mustList(t *testing.T, r *rig) []admin.Workspace {
	t.Helper()
	ws, err := r.adm.ListWorkspaces(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

package httpapi

// Phase 6 security review: repro tests. Each asserts the vulnerable behaviour
// (it passes today and fails once the issue is fixed). Gated by ABIDATA_SECREVIEW.

import (
	"fmt"
	"os"
	"testing"
)

// M1: restore-as-new (routes_backups.go:116) lets any workspace owner create
// workspaces, which the permissions matrix reserves to platform admins, with
// no cap: each call adds a full copy of the database on disk, a new admin row
// and an open workspace handle, with the source's quota copied. Workspace
// names are globally unique (NOCASE), so the 409/201 split also tells an
// owner whether a workspace name exists in another tenant.
func TestSecReview_OwnerCreatesUnlimitedWorkspacesViaRestoreAsNew(t *testing.T) {
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("set ABIDATA_SECREVIEW=1 to run security-review repros")
	}
	e := newEnv(t)
	ws, root, owner, _, _, _ := roles(t, e)
	if st, _, _ := owner.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Direct"}); st != 403 {
		t.Fatalf("owner created a workspace directly: %d", st)
	}
	base := "/api/v1/w/" + ws + "/backups"
	st, bk, _ := owner.do("POST", base, nil)
	if st != 201 {
		t.Fatalf("backup %d %v", st, bk)
	}
	const n = 10
	created := 0
	for i := range n {
		st, body, _ := owner.do("POST", base+"/"+bk["id"].(string)+"/restore-as-new", map[string]string{"name": fmt.Sprintf("Copy %d", i)})
		if st != 201 {
			t.Logf("restore-as-new %d refused: %d %v", i, st, body)
			break
		}
		created++
	}
	if created < n {
		t.Fatalf("owner limited to %d workspaces (fixed?)", created)
	}
	// Cross-tenant name oracle: another tenant's workspace, owner is not a member.
	if st, _, _ := root.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Acme Merger Plan"}); st != 201 {
		t.Fatalf("admin create %d", st)
	}
	stTaken, _, _ := owner.do("POST", base+"/"+bk["id"].(string)+"/restore-as-new", map[string]string{"name": "acme merger plan"})
	stFree, _, _ := owner.do("POST", base+"/"+bk["id"].(string)+"/restore-as-new", map[string]string{"name": "Unused Name 42"})
	if stTaken == stFree {
		t.Fatalf("no name oracle (fixed?): %d vs %d", stTaken, stFree)
	}
	t.Logf("owner created %d workspaces; existing foreign name → %d, free name → %d", created+1, stTaken, stFree)
}

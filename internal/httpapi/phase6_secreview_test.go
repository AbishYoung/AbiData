package httpapi

// Phase 6 security review: regression tests.

import (
	"fmt"
	"testing"
)

// M1 (fixed): restore-as-new let any owner create unlimited workspaces. Owners
// who are not platform admins may now create MaxOwnerCopies; admins are not
// capped. (Workspace names stay platform-unique, so a conflict still tells an
// owner a name is taken: accepted, see ADR 0012.)
func TestSecReview_OwnerCreatesUnlimitedWorkspacesViaRestoreAsNew(t *testing.T) {
	e := newEnv(t)
	ws, _, owner, _, _, _ := roles(t, e)
	base := "/api/v1/w/" + ws + "/backups"
	st, bk, _ := owner.do("POST", base, nil)
	if st != 201 {
		t.Fatalf("backup %d %v", st, bk)
	}
	for i := range MaxOwnerCopies {
		if st, body, _ := owner.do("POST", base+"/"+bk["id"].(string)+"/restore-as-new", map[string]string{"name": fmt.Sprintf("Copy %d", i)}); st != 201 {
			t.Fatalf("copy %d: %d %v", i, st, body)
		}
	}
	if st, body, _ := owner.do("POST", base+"/"+bk["id"].(string)+"/restore-as-new", map[string]string{"name": "One too many"}); st != 507 {
		t.Fatalf("over the cap: %d %v", st, body)
	}
	// A failed restore-as-new leaves no workspace behind (Phase 6 review L3).
	if st, _, _ := owner.do("POST", base+"/"+bk["id"].(string)+"/restore-as-new", map[string]string{"name": "Copy 0"}); st != 507 {
		t.Fatalf("duplicate name: %d", st)
	}
}

func TestDeleteManualBackup(t *testing.T) {
	e := newEnv(t)
	ws, _, owner, b, _, _ := roles(t, e)
	base := "/api/v1/w/" + ws + "/backups"
	_, bk, _ := owner.do("POST", base, nil)
	if st, _, _ := b.do("DELETE", base+"/"+bk["id"].(string), nil); st != 403 {
		t.Fatalf("builder delete %d", st)
	}
	if st, body, _ := owner.do("DELETE", base+"/"+bk["id"].(string), nil); st != 204 {
		t.Fatalf("delete %d %v", st, body)
	}
	_, list, _ := owner.do("GET", base, nil)
	if items := list["items"].([]any); len(items) != 0 {
		t.Fatalf("still listed: %v", items)
	}
}

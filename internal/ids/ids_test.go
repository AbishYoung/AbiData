package ids

import (
	"path/filepath"
	"testing"
)

func TestParseWorkspaceID(t *testing.T) {
	good := New()
	if _, err := ParseWorkspaceID(good); err != nil {
		t.Fatalf("%s: %v", good, err)
	}
	for _, bad := range []string{"", "../x", "..%2Fx", "%2e%2e%2fx", "01ARZ3NDEKTSV4RRFFQ69G5FA", "01ARZ3NDEKTSV4RRFFQ69G5FAVX",
		"01arz3ndektsv4rrffq69g5fav", "01ARZ3NDEKTSV4RRFFQ69G5FAI", "01ARZ3NDEKTSV4RRFFQ69G5FA/", "01ARZ3NDEKTSV4RRFFQ69G5F\x00V"} {
		if _, err := ParseWorkspaceID(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestParseThreadAndMessageID(t *testing.T) {
	tid := NewThreadID()
	if got, err := ParseThreadID(tid.String()); err != nil || got != tid {
		t.Fatalf("ParseThreadID(%q) = %q, %v", tid, got, err)
	}
	mid := NewMessageID()
	if got, err := ParseMessageID(mid.String()); err != nil || got != mid {
		t.Fatalf("ParseMessageID(%q) = %q, %v", mid, got, err)
	}
	if _, err := ParseThreadID(mid.String()); err == nil {
		t.Errorf("ParseThreadID accepted MessageID %q", mid)
	}
	if _, err := ParseMessageID(tid.String()); err == nil {
		t.Errorf("ParseMessageID accepted ThreadID %q", tid)
	}
}

func TestSafeJoin(t *testing.T) {
	base := t.TempDir()
	if p, err := SafeJoin(base, "workspaces", New()+".db"); err != nil || filepath.Dir(filepath.Dir(p)) != base {
		t.Fatalf("got %q, %v", p, err)
	}
	for _, bad := range [][]string{{".."}, {"../x"}, {"a", "../../x"}, {"/etc/passwd"}, {""}, {"."}, {"a\x00b"}} {
		if p, err := SafeJoin(base, bad...); err == nil && bad[0] != "/etc/passwd" {
			t.Errorf("%q accepted as %q", bad, p)
		} else if err == nil {
			// filepath.Join treats "/etc/passwd" as relative to base; it must stay inside.
			if filepath.Dir(filepath.Dir(p)) != base {
				t.Errorf("%q escaped to %q", bad, p)
			}
		}
	}
}

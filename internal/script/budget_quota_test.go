package script

import (
	"testing"
	"time"

	"abidata/internal/ids"
)

func TestUIEventBudgetClamp(t *testing.T) {
	for ms, want := range map[int]time.Duration{0: time.Second, 100: MinUIEventWall, 2000: 2 * time.Second, 60000: MaxUIEventWall} {
		if got := UIEventBudget(ms).Wall; got != want {
			t.Errorf("%d ms: %v, want %v", ms, got, want)
		}
	}
	if b := UIEventBudget(2000); b.TxBlock != DefaultBudgets[UIEvent].TxBlock || !b.Writes {
		t.Fatalf("other limits changed: %+v", b)
	}
}

func TestRunnerPerWorkspaceQuota(t *testing.T) {
	rn := NewRunner(8, 2)
	ws := ids.NewWorkspaceID()
	q := 5
	rn.PerWorkspace = func(ids.WorkspaceID) int { return q }
	if c := cap(rn.slots(ws)); c != 5 {
		t.Fatalf("cap %d", c)
	}
	q = 100 // clamped to the global pool
	if c := cap(rn.slots(ws)); c != 8 {
		t.Fatalf("cap %d", c)
	}
	s := rn.slots(ws)
	s <- struct{}{} // a script is running: the quota change waits
	q = 1
	if c := cap(rn.slots(ws)); c != 8 {
		t.Fatalf("resized while busy: %d", c)
	}
	<-s
	if c := cap(rn.slots(ws)); c != 1 {
		t.Fatalf("cap %d", c)
	}
}

package jobs

// Phase 5 security review repros (assert current, vulnerable behaviour).

import (
	"context"
	"os"
	"testing"
	"time"

	"abidata/internal/ids"
)

func secreview(t *testing.T) {
	t.Helper()
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("security review repro; set ABIDATA_SECREVIEW=1")
	}
}

// blockingStore makes PutRun for one workspace wait, like SQLStore.PutRun does
// when that workspace's writer gate is held (Background priority has no gate
// timeout): e.g. by an all_or_nothing import or a staging merge_into.
type blockingStore struct {
	*memStore
	slow    ids.WorkspaceID
	entered chan struct{}
	release chan struct{}
}

func (b *blockingStore) PutRun(ctx context.Context, w ids.WorkspaceID, r Run) error {
	if w == b.slow {
		select {
		case b.entered <- struct{}{}:
		default:
		}
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return b.memStore.PutRun(ctx, w, r)
}

// The scheduler holds its single global mutex (s.mu) while it writes run rows
// into a workspace DB (trigger → PutRun inside fire/RunNow, finish). While one
// tenant's writer is busy, every other tenant's Run now, scheduled fires and run
// completions wait on that mutex: a cross-tenant stall a builder can provoke at
// will by keeping their own writer busy and triggering one of their jobs.
func TestSecReview_SchedulerGlobalLockHeldAcrossWorkspaceWrite(t *testing.T) {
	secreview(t)
	wsA := ids.WorkspaceID("01J00000000000000000000WSA")
	wsB := ids.WorkspaceID("01J00000000000000000000WSB")
	st := &blockingStore{memStore: newMem(), slow: wsA, entered: make(chan struct{}, 1), release: make(chan struct{})}
	exec := &fakeExec{}
	s := New(Config{Store: st, Executor: exec, Workers: 8, Clock: &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}})
	for _, j := range []Job{{ID: "jobA", Name: "A"}, {ID: "jobB", Name: "B"}} {
		j.Enabled, j.Timezone, j.OverlapPolicy, j.MisfirePolicy = true, "UTC", "skip", "run_once"
		st.jobs[j.ID] = j
	}
	defer func() {
		select {
		case <-st.release:
		default:
			close(st.release)
		}
		s.Wait()
	}()

	go func() { _, _ = s.RunNow(context.Background(), wsA, "jobA", TriggerManual, "attacker", nil) }()
	<-st.entered // tenant A's run write is now stuck on A's writer, inside s.mu

	done := make(chan error, 1)
	go func() {
		_, err := s.RunNow(context.Background(), wsB, "jobB", TriggerManual, "victim", nil)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("tenant B's Run now was not blocked by tenant A (err=%v): the scheduler lock is no longer global", err)
	case <-time.After(500 * time.Millisecond):
		t.Log("tenant B's Run now is blocked while tenant A's writer is busy")
	}
	close(st.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

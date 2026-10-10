package backup

// Phase 6 security review: repro tests. Each asserts the vulnerable behaviour
// (it passes today and fails once the issue is fixed). Gated by ABIDATA_SECREVIEW.

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/workspace"
)

func secreview(t *testing.T) {
	t.Helper()
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("set ABIDATA_SECREVIEW=1 to run security-review repros")
	}
}

// H1: Restore's "already in progress" check (backup.go:614) is a bare
// fileExists on the journal, taken before the slow prepare and not under any
// lock. Two restores of one workspace (owner double-click, two tabs, two
// owners) are both admitted. The loser's Exclusive fails (WORKSPACE_MAINTENANCE)
// and its error path runs recoverOne, which reads and then DELETES the shared
// journal while the winner is inside Exclusive. If the process then dies after
// the winner moved ws.db aside but before it rewrote the journal, startup
// Recover finds no journal: the workspace file is gone and the old database is
// stranded in .pre-restore (never "fully old or fully new").
func TestSecReview_ConcurrentRestoreDeletesWinnersJournal(t *testing.T) {
	secreview(t)
	for attempt := 0; attempt < 5; attempt++ {
		if concurrentRestoreAttempt(t) {
			return
		}
	}
	t.Fatal("no attempt left the workspace without its database file (fixed?)")
}

func concurrentRestoreAttempt(t *testing.T) bool {
	e := newEnv(t)
	e.seed(e.ws)
	b1 := e.snapshot(e.ws, admin.BackupManual)
	b2 := e.snapshot(e.ws, admin.BackupManual)

	var arrivals atomic.Int32
	second := make(chan struct{})
	loserDone := make(chan struct{})
	var journalGone, statusActive atomic.Bool
	e.svc.hook = func(step string) error {
		switch step {
		case "prepared":
			if arrivals.Add(1) == 1 {
				select {
				case <-second:
				case <-time.After(5 * time.Second):
				}
			} else {
				close(second)
			}
		case "restoring":
			// Only the restore that won the drain gets here.
			select {
			case <-loserDone:
			case <-time.After(10 * time.Second):
			}
			j, _ := e.svc.readJournal(e.ws)
			journalGone.Store(j == nil)
			w, _ := e.ad.GetWorkspace(e.ctx, e.ws)
			statusActive.Store(w.Status == admin.StatusActive)
		case "move:.db":
			return errCrash // the process dies right after ws.db was moved aside
		}
		return nil
	}
	errs := make(chan error, 2)
	for _, b := range []admin.Backup{b1, b2} {
		go func() { errs <- e.svc.Restore(e.ctx, e.ws, b.ID, e.owner) }()
	}
	first := <-errs
	close(loserDone)
	secondErr := <-errs
	t.Logf("arrivals at prepared: %d; first: %v; second: %v", arrivals.Load(), first, secondErr)
	if !errors.Is(first, errCrash) && !errors.Is(secondErr, errCrash) {
		return false // nobody reached the simulated crash; retry
	}
	if arrivals.Load() == 2 {
		t.Logf("journal deleted under the winner: %v; status active mid-restore: %v", journalGone.Load(), statusActive.Load())
	} else {
		t.Log("both admitted; their journal writes collided on the shared <ws>.json.partial")
	}
	e.svc.hook = nil
	e.restart()
	if err := e.svc.Recover(e.ctx); err != nil {
		t.Logf("recover: %v", err)
	}
	p, _ := e.wm.PathsFor(e.ws)
	if _, err := os.Stat(p.DB); !errors.Is(err, os.ErrNotExist) {
		t.Logf("workspace file present after recovery this attempt: %v", err)
		return false
	}
	if _, err := e.wm.Acquire(e.ctx, e.ws); !errors.Is(err, workspace.ErrNotExist) {
		t.Fatalf("acquire: %v", err)
	}
	stranded, _ := filepath.Glob(filepath.Join(e.svc.asideRoot(), "*", string(e.ws)+".db"))
	if len(stranded) != 1 {
		t.Fatalf("expected the live DB stranded in .pre-restore, got %v", stranded)
	}
	t.Logf("workspace file missing after recovery; old DB stranded at %s", stranded[0])
	return true
}

// M2: an admin suspension made while a restore is preparing is silently undone:
// finish (backup.go:741) and recoverOne (backup.go:871) set the status to
// active unconditionally instead of restoring the pre-restore status.
func TestSecReview_RestoreUndoesSuspension(t *testing.T) {
	secreview(t)
	e := newEnv(t)
	e.seed(e.ws)
	b := e.snapshot(e.ws, admin.BackupManual)
	e.svc.hook = func(step string) error {
		if step == "prepared" {
			// What PATCH /admin/workspaces/{id} {"status":"suspended"} does; it
			// is allowed because the status is still "active" here.
			st := admin.StatusSuspended
			return e.ad.UpdateWorkspace(e.ctx, e.ws, admin.WorkspacePatch{Status: &st})
		}
		return nil
	}
	if err := e.svc.Restore(e.ctx, e.ws, b.ID, e.owner); err != nil {
		t.Fatal(err)
	}
	e.waitAside()
	w, _ := e.ad.GetWorkspace(e.ctx, e.ws)
	if w.Status != admin.StatusActive {
		t.Fatalf("suspension kept (fixed?): %s", w.Status)
	}
	t.Log("suspended workspace is active again after the restore")
}

// M3: manual backups are never pruned (keepSet, backup.go:988) and nothing
// caps their number or bytes per workspace or counts them against the
// workspace quota; POST /backups is synchronous with a detached context. An
// owner can loop it until the backup volume is full (likewise pre_restore
// backups: one full copy per restore, kept 7 days, uncapped).
func TestSecReview_ManualBackupsUnbounded(t *testing.T) {
	secreview(t)
	e := newEnv(t)
	e.seed(e.ws)
	const n = 25
	for range n {
		e.snapshot(e.ws, admin.BackupManual)
	}
	if err := e.svc.ApplyRetention(e.ctx, e.ws); err != nil {
		t.Fatal(err)
	}
	all, err := e.ad.ListBackups(e.ctx, e.ws)
	if err != nil {
		t.Fatal(err)
	}
	var bytes int64
	for _, b := range all {
		bytes += b.GzBytes
	}
	if len(all) < n {
		t.Fatalf("retention capped manual backups at %d (fixed?)", len(all))
	}
	t.Logf("%d manual backups kept (%d gz bytes), no cap", len(all), bytes)
}

// L1: leftovers of interrupted work are never swept. A crash during prepare
// (before the journal exists) leaves a full decompressed copy in
// workspaces/.restore/, and a crash or failure after finish deleted the journal
// but before keepAside archived the old files (keepAside runs in a goroutine,
// backup.go:751) leaves the whole old database in .pre-restore/. Recover only
// walks journals, so both stay forever.
func TestSecReview_RecoverNeverSweepsOrphans(t *testing.T) {
	secreview(t)
	e := newEnv(t)
	e.seed(e.ws)
	orphanRestore := filepath.Join(e.svc.restoreDir(), string(e.ws)+"-orphan.db")
	orphanAside := filepath.Join(e.svc.asideRoot(), string(e.ws)+"-1", string(e.ws)+".db")
	for _, p := range []string{orphanRestore, orphanAside} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, 1<<20), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e.restart()
	if err := e.svc.Recover(e.ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{orphanRestore, orphanAside} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was swept (fixed?): %v", p, err)
		}
	}
}

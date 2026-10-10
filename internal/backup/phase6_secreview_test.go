package backup

// Phase 6 security review: regression tests (each finding is fixed).

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"abidata/internal/admin"
	"abidata/internal/fault"
)

// H1 (fixed): two restores of one workspace were both admitted; they shared the
// journal and the loser's cleanup undid the winner's, which could leave the
// workspace without its database after a crash. Now the second is refused, and
// a crash mid-restore still recovers to the old database.
func TestSecReview_ConcurrentRestoreDeletesWinnersJournal(t *testing.T) {
	e := newEnv(t)
	e.seed(e.ws)
	b1 := e.snapshot(e.ws, admin.BackupManual)
	b2 := e.snapshot(e.ws, admin.BackupManual)
	before := e.fingerprint(e.ws)

	inside := make(chan struct{})
	release := make(chan struct{})
	e.svc.hook = func(step string) error {
		switch step {
		case "prepared":
			close(inside)
			<-release
		case "move:.db":
			return errCrash // the process dies right after ws.db was moved aside
		}
		return nil
	}
	first := make(chan error, 1)
	go func() { first <- e.svc.Restore(e.ctx, e.ws, b1.ID, e.owner) }()
	<-inside
	err := e.svc.Restore(e.ctx, e.ws, b2.ID, e.owner)
	if fault.Classify(err).Code != fault.WorkspaceBusy {
		t.Fatalf("second restore: %v", err)
	}
	close(release)
	if err := <-first; !errors.Is(err, errCrash) {
		t.Fatalf("first restore: %v", err)
	}
	e.svc.hook = nil
	e.restart()
	if err := e.svc.Recover(e.ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := e.wm.PathsFor(e.ws)
	if _, err := os.Stat(p.DB); err != nil {
		t.Fatalf("workspace file missing after recovery: %v", err)
	}
	if got := e.fingerprint(e.ws); len(got) != len(before) {
		t.Fatalf("not the old database after recovery: %v vs %v", got, before)
	}
	w, _ := e.ad.GetWorkspace(e.ctx, e.ws)
	if w.Status != admin.StatusActive {
		t.Fatalf("status %s", w.Status)
	}
}

// M2 (fixed): a suspension made while a restore was preparing was undone.
func TestSecReview_RestoreUndoesSuspension(t *testing.T) {
	e := newEnv(t)
	e.seed(e.ws)
	b := e.snapshot(e.ws, admin.BackupManual)
	e.svc.hook = func(step string) error {
		if step == "prepared" {
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
	if w.Status != admin.StatusSuspended {
		t.Fatalf("suspension lost: %s", w.Status)
	}
}

// M3 (fixed): manual backups are capped per workspace, one runs at a time, and
// retention keeps at most MaxPreRestore pre_restore backups.
func TestSecReview_ManualBackupsUnbounded(t *testing.T) {
	e := newEnv(t)
	e.seed(e.ws)
	l := e.lease(e.ws)
	defer l.Release()
	for i := range MaxManualBackups {
		if _, err := e.svc.Manual(e.ctx, l, e.owner); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
	}
	if _, err := e.svc.Manual(e.ctx, l, e.owner); fault.Classify(err).Code != fault.QuotaExceeded {
		t.Fatalf("over the cap: %v", err)
	}
	if !e.svc.claim("backup:" + string(e.ws)) {
		t.Fatal("claim")
	}
	if _, err := e.svc.Manual(e.ctx, l, e.owner); fault.Classify(err).Code != fault.WorkspaceBusy {
		t.Fatalf("concurrent backup: %v", err)
	}
	e.svc.unclaim("backup:" + string(e.ws))

	now := time.Now().UTC()
	var all []admin.Backup
	for i := range 6 {
		all = append(all, admin.Backup{ID: string(rune('a' + i)), Kind: admin.BackupPreRestore, CreatedAt: now.Add(-time.Duration(i) * time.Hour)})
	}
	if keep := keepSet(all, DefaultRetention, now); len(keep) != MaxPreRestore {
		t.Fatalf("kept %d pre_restore backups", len(keep))
	}
}

// L1 (fixed): Recover sweeps leftovers of interrupted work: restore copies are
// deleted and moved-aside databases are archived as pre_restore backups.
func TestSecReview_RecoverNeverSweepsOrphans(t *testing.T) {
	e := newEnv(t)
	e.seed(e.ws)
	orphanRestore := filepath.Join(e.svc.restoreDir(), string(e.ws)+"-orphan.db")
	asideDir := filepath.Join(e.svc.asideRoot(), string(e.ws)+"-1")
	if err := os.MkdirAll(filepath.Dir(orphanRestore), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanRestore, make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	e.restart() // closes the workspace, so its file can be copied as an orphan
	p, _ := e.wm.PathsFor(e.ws)
	raw, err := os.ReadFile(p.DB)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(asideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(asideDir, string(e.ws)+".db"), raw, 0o600); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	if err := e.svc.Recover(e.ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{orphanRestore, asideDir} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s not swept: %v", p, err)
		}
	}
	all, _ := e.ad.ListBackups(e.ctx, e.ws)
	if len(all) != 1 || all[0].Kind != admin.BackupPreRestore {
		t.Fatalf("orphan not archived: %+v", all)
	}
}

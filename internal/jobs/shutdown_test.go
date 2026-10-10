package jobs_test

import (
	"testing"
	"time"

	"abidata/internal/fault"
	"abidata/internal/jobs"
)

// A run still going when the shutdown grace ends is cancelled and recorded as
// interrupted (brief <deployment>).
func TestShutdownInterruptsAfterGrace(t *testing.T) {
	e := setup(t)
	sid := e.script(t, "Spin", `while true do end`)
	job, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Spin", ScriptID: sid, TimeoutSeconds: 60}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if e.sched.Shutdown(200 * time.Millisecond) {
		t.Fatal("Shutdown reported that the run finished within grace")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("shutdown took %v", d)
	}
	got := e.wait(t, run.ID)
	if got.Status != jobs.StatusInterrupted || got.ErrorCode != "INTERRUPTED" {
		t.Fatalf("%+v", got)
	}
}

// Runs that finish within grace are left alone.
func TestShutdownWaitsForQuickRuns(t *testing.T) {
	e := setup(t)
	sid := e.script(t, "Quick", `return 1`)
	job, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Quick", ScriptID: sid}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !e.sched.Shutdown(10 * time.Second) {
		t.Fatal("quick run was interrupted")
	}
	if got := e.wait(t, run.ID); got.Status != jobs.StatusSucceeded {
		t.Fatalf("%+v", got)
	}
}

// A suspended workspace runs nothing: started runs are cancelled by
// CancelWorkspace, and runs that would start are cancelled (review M4).
func TestSuspendedWorkspaceRunsNothing(t *testing.T) {
	e := setup(t)
	sid := e.script(t, "Spin", `while true do end`)
	job, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Spin", ScriptID: sid, TimeoutSeconds: 60}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	e.inactive.Store(true)
	if n := e.sched.CancelWorkspace(e.ws); n != 1 {
		t.Fatalf("cancelled %d runs", n)
	}
	if got := e.wait(t, run.ID); got.Status != jobs.StatusCancelled {
		t.Fatalf("%+v", got)
	}
	run2, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.wait(t, run2.ID); got.Status != jobs.StatusCancelled || got.StartedAt != nil {
		t.Fatalf("%+v", got)
	}
}

// After Shutdown begins, no new run starts (it would outlive the workspace).
func TestRunNowRefusedAfterShutdown(t *testing.T) {
	e := setup(t)
	sid := e.script(t, "Quick", `return 1`)
	job, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Quick", ScriptID: sid}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	e.sched.Shutdown(time.Second)
	if _, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil); fault.Classify(err).Code != fault.WorkspaceBusy {
		t.Fatalf("err %v", err)
	}
}

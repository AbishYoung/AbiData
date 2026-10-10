package jobs_test

import (
	"testing"
	"time"

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

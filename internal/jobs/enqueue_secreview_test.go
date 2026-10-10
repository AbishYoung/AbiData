package jobs_test

// Phase 5 security review repros (assert current, vulnerable behaviour).

import (
	"os"
	"testing"
	"time"

	"abidata/internal/jobs"
)

// A job that enqueues itself (overlap_policy=queue) runs back to back forever:
// finish() starts the queued run without checking job.Enabled and RunNow /
// Enqueue never look at it either, there is no enqueue depth or rate limit, and
// queued runs cannot be cancelled (Scheduler.Cancel only knows started runs).
// Disabling the job does not stop the chain; only deleting it does. Each lap
// adds a _sys_job_runs row, so the chain also grows the workspace file until
// QUOTA_EXCEEDED.
func TestSecReview_SelfEnqueueSurvivesDisable(t *testing.T) {
	if os.Getenv("ABIDATA_SECREVIEW") == "" {
		t.Skip("security review repro; set ABIDATA_SECREVIEW=1")
	}
	e := setup(t)
	sid := e.script(t, "Loop", `pcall(jobs.enqueue, "Loop")`)
	job, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Loop", ScriptID: sid, OverlapPolicy: "queue"}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil); err != nil {
		t.Fatal(err)
	}
	off := false
	job, err = e.svc.Put(e.ctx, e.l, job.ID, job.Version, jobs.JobInput{Name: "Loop", ScriptID: sid, OverlapPolicy: "queue", Enabled: &off}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		runs, _ := e.svc.Runs(e.ctx, e.l, job.ID, 200, time.Time{})
		return len(runs)
	}
	before := count()
	deadline := time.Now().Add(5 * time.Second)
	for count() < before+10 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	after := count()
	// Stop the chain: deleting the job is the only lever.
	for i := 0; i < 50; i++ {
		j, err := e.svc.Get(e.ctx, e.l, job.ID)
		if err != nil {
			break
		}
		if e.svc.Delete(e.ctx, e.l, j.ID, j.Version) == nil {
			break
		}
	}
	if after < before+10 {
		t.Fatalf("expected the disabled job to keep re-running itself; runs %d -> %d", before, after)
	}
	t.Logf("disabled job ran %d more times within the window", after-before)
}

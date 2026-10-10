package jobs_test

// Phase 5 security review: regression tests.

import (
	"testing"
	"time"

	"abidata/internal/jobs"
)

// L2 (fixed): a job that enqueued itself (overlap_policy=queue) ran back to back forever:
// finish() starts the queued run without checking job.Enabled and RunNow /
// Enqueue never look at it either, there is no enqueue depth or rate limit, and
// queued runs cannot be cancelled (Scheduler.Cancel only knows started runs).
// Disabling the job does not stop the chain; only deleting it does. Each lap
// adds a _sys_job_runs row, so the chain also grows the workspace file until
// QUOTA_EXCEEDED.
func TestSecReview_SelfEnqueueSurvivesDisable(t *testing.T) {
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
	// After at most the in-flight run and its queued successor, the chain stops.
	time.Sleep(300 * time.Millisecond)
	settled := count()
	time.Sleep(500 * time.Millisecond)
	if after := count(); after != settled {
		t.Fatalf("the disabled job kept re-running itself: %d -> %d", settled, after)
	}
}

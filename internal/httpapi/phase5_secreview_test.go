package httpapi

// Phase 5 security review: regression tests.

import (
	"context"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/ids"
	"abidata/internal/jobs"
)

// M4 (fixed): POST /exports is open to viewers and the operations service had
// no per-user or per-workspace concurrency limit. Every export runs at once
// in its own goroutine (a saved-query export buffers up to 64 MiB of rows) and
// every result is charged to the workspace's shared artifact quota for 30 days,
// so a viewer can exhaust memory/CPU and fill the quota that builders' job
// artifacts.save calls need.
func TestSecReview_ViewerUnboundedExports(t *testing.T) {
	e := newEnv(t)
	ws, _, owner, _, _, _ := roles(t, e)
	e.user("v@example.com", false)
	if st, body, _ := owner.do("POST", "/api/v1/w/"+ws+"/members", map[string]string{"email": "v@example.com", "role": "viewer"}); st != 201 {
		t.Fatalf("%d %v", st, body)
	}
	viewer := e.login("v@example.com")
	wsID, _ := ids.ParseWorkspaceID(ws)
	l, err := e.s.deps.Workspaces.Acquire(context.Background(), wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.deps.Catalog.Run(context.Background(), l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{ID: ids.New(), DisplayName: "T",
		Columns: []catalog.ColumnSpec{{DisplayName: "A", Type: catalog.ShortText}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, _ := e.s.deps.Schemas.Get(context.Background(), l)
	l.Release()
	tid := sch.Tables[0].ID
	accepted := 0
	for i := 0; i < 40; i++ {
		st, body, _ := viewer.do("POST", "/api/v1/w/"+ws+"/exports", map[string]any{"source_kind": "table", "source_id": tid, "format": "xlsx"})
		if st == 202 {
			accepted++
		} else {
			t.Logf("export %d refused: %d %v", i, st, body)
			break
		}
	}
	if accepted >= 40 {
		t.Fatalf("a viewer started %d concurrent exports", accepted)
	}
	t.Logf("viewer was limited after %d concurrent exports", accepted)
}

// L1 (fixed): Scheduler.Cancel was keyed by run id only. cancelJobRun checks that the run id
// exists in the caller's workspace DB and then cancels whatever live run has
// that id, in any workspace. Run ids are copied verbatim when a workspace file
// is duplicated (restore-as-new, Phase 6), so a member of the copy can cancel
// the original's live runs. ops.Cancel already checks the workspace; the job
// path does not.
func TestSecReview_CrossWorkspaceRunCancel(t *testing.T) {
	e := newEnv(t)
	ws1, _, _, b, _, _ := roles(t, e)
	base1 := "/api/v1/w/" + ws1
	_, sc, _ := b.do("POST", base1+"/scripts", map[string]any{"name": "Spin", "kind": "job", "source": `while true do end`})
	_, job, _ := b.do("POST", base1+"/jobs", map[string]any{"name": "Spin", "script_id": sc["id"], "timeout_seconds": 30})
	st, run, _ := b.do("POST", base1+"/jobs/"+job["id"].(string)+"/run", nil)
	if st != 202 {
		t.Fatalf("run %d %v", st, run)
	}
	rid := run["id"].(string)

	// Tenant 2: an unrelated workspace whose DB happens to hold a run row with the same id.
	e.user("other@example.com", false)
	_, w2, _ := e.login("root@example.com").do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Other", "owner_email": "other@example.com"})
	ws2 := w2["id"].(string)
	other := e.login("other@example.com")
	base2 := "/api/v1/w/" + ws2
	_, sc2, _ := other.do("POST", base2+"/scripts", map[string]any{"name": "Noop", "kind": "job", "source": `return 1`})
	_, job2, _ := other.do("POST", base2+"/jobs", map[string]any{"name": "Noop", "script_id": sc2["id"]})
	ws2ID, _ := ids.ParseWorkspaceID(ws2)
	now := time.Now().UTC()
	if err := (&jobs.SQLStore{Admin: e.ad, Workspaces: e.s.deps.Workspaces}).PutRun(context.Background(), ws2ID,
		jobs.Run{ID: rid, JobID: job2["id"].(string), TriggerKind: "manual", Status: "running", Attempt: 1, QueuedAt: now, StartedAt: &now}); err != nil {
		t.Fatal(err)
	}

	if st, _, _ := other.do("POST", base2+"/runs/"+rid+"/cancel", nil); st == 202 {
		t.Fatal("tenant 2's cancel was accepted")
	}
	time.Sleep(300 * time.Millisecond)
	if _, d, _ := b.do("GET", base1+"/runs/"+rid, nil); d["finished_at"] != nil {
		t.Fatalf("tenant 1's run was stopped: %v", d["status"])
	}
	if st, _, _ := b.do("POST", base1+"/runs/"+rid+"/cancel", nil); st != 202 {
		t.Fatalf("own cancel %d", st)
	}
}

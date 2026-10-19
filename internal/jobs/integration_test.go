package jobs_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/admin/admintest"
	"abidata/internal/artifacts"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/jobs"
	"abidata/internal/records"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
	"abidata/internal/scripts"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

type env struct {
	ctx   context.Context
	ws    ids.WorkspaceID
	wm    *workspace.Manager
	store *jobs.SQLStore
	sched *jobs.Scheduler
	svc   *jobs.Service
	arts  *artifacts.Store
	l     *workspace.Lease
	// inactive makes Config.Active report the workspace as suspended.
	inactive atomic.Bool
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o700); err != nil {
		t.Fatal(err)
	}
	ad := admintest.Open(t)
	w := wstest.New(t)
	wm := w.Manager
	ws := w.Create(t)
	l, err := wm.Acquire(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	cache := catalog.NewCache()
	if _, err := (&catalog.Engine{}).Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Notes",
		Columns: []catalog.ColumnSpec{{DisplayName: "Body", Type: catalog.ShortText}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sc := &scripts.Service{}
	arts := &artifacts.Store{Dir: filepath.Join(dir, "artifacts")}
	store := &jobs.SQLStore{Admin: ad, Workspaces: wm}
	e := &env{ctx: ctx, ws: ws, wm: wm, store: store, arts: arts, l: l}
	exec := &jobs.LuaExecutor{Workspaces: wm, Scripts: sc, Runner: script.NewRunner(4, 2), Schemas: cache,
		Records: &records.Service{Schemas: cache}, Artifacts: arts}
	e.sched = jobs.New(jobs.Config{Store: store, Executor: exec, Workers: 2,
		Active: func(context.Context, ids.WorkspaceID) bool { return !e.inactive.Load() }})
	e.svc = &jobs.Service{Scripts: sc, Scheduler: e.sched}
	exec.Enqueue = func(ctx context.Context, ws ids.WorkspaceID, name, by string, args map[string]any, fromForm bool) (string, error) {
		l, err := wm.Acquire(ctx, ws)
		if err != nil {
			return "", err
		}
		defer l.Release()
		return e.svc.Enqueue(ctx, l, name, by, args, fromForm)
	}
	t.Cleanup(func() {
		e.sched.Wait()
		l.Release()
	})
	return e
}

func (e *env) script(t *testing.T, name, src string) string {
	t.Helper()
	s, err := (&scripts.Service{}).Put(e.ctx, e.l, "", 0, scripts.ScriptInput{Name: name, Kind: "job", Source: src}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	return s.ID
}

func (e *env) wait(t *testing.T, runID string) jobs.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := e.svc.GetRun(e.ctx, e.l, runID)
		if err == nil && r.FinishedAt != nil {
			e.sched.Wait()
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish", runID)
	return jobs.Run{}
}

func TestJobRunEndToEnd(t *testing.T) {
	e := setup(t)
	sid := e.script(t, "Nightly", `
		log.info("starting", {who = job.name, n = job.args.n})
		db.exec([[INSERT INTO "Notes"("Body") VALUES (:b)]], {b = "from job " .. job.run_id})
		local a = artifacts.save("out.csv", csv.encode({{1, "x"}}, {columns = {"id", "v"}}))
		log.info("saved", {size = a.size})
	`)
	job, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Nightly", ScriptID: sid, Schedule: "0 2 * * *", Args: map[string]any{"n": 3}}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Timezone != "UTC" || job.OverlapPolicy != "skip" || job.TimeoutSeconds != 300 || !job.Enabled {
		t.Fatalf("defaults: %+v", job)
	}
	entries, _ := e.store.Entries(e.ctx, e.ws)
	if len(entries) != 1 || entries[0].NextRunAt.IsZero() {
		t.Fatalf("index not rebuilt: %+v", entries)
	}
	run, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := e.wait(t, run.ID)
	if got.Status != jobs.StatusSucceeded {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Log, `INFO  starting n=3 who="Nightly"`) || !strings.Contains(got.Log, "saved size=") {
		t.Fatalf("log:\n%s", got.Log)
	}
	var body, by string
	if err := e.l.Read(e.ctx, func(q dbx.Querier) error {
		return q.QueryRow(e.ctx, `SELECT "Body", coalesce((SELECT updated_by FROM _sys_jobs LIMIT 1), '') FROM "Notes"`).Scan(&body, &by)
	}); err != nil || body != "from job "+run.ID {
		t.Fatalf("row %q %v", body, err)
	}
	f, info, err := e.arts.Open(e.ws, run.ID, "out.csv")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if string(b) != "id,v\n1,x\n" || info.MIME != "text/csv; charset=utf-8" {
		t.Fatalf("%q %+v", b, info)
	}
	list, err := e.svc.Runs(e.ctx, e.l, job.ID, 10, time.Time{})
	if err != nil || len(list) != 1 || list[0].Log != "" {
		t.Fatalf("runs %+v %v", list, err)
	}
	ix, _ := e.store.Entries(e.ctx, e.ws)
	if ix[0].LastStatus != jobs.StatusSucceeded || ix[0].RunningRunID != "" {
		t.Fatalf("index %+v", ix[0])
	}
}

func TestJobFailureAndEnqueue(t *testing.T) {
	e := setup(t)
	bad := e.script(t, "Bad", `log.warn("about to fail") error("boom")`)
	child := e.script(t, "Child", `log.info("child", {x = job.args.x})`)
	parent := e.script(t, "Parent", `local id = jobs.enqueue("Child", {x = 7}) log.info("queued", {id = id})`)
	jb, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Bad", ScriptID: bad}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	run, _ := e.sched.RunNow(e.ctx, e.ws, jb.ID, jobs.TriggerManual, "u1", nil)
	got := e.wait(t, run.ID)
	if got.Status != jobs.StatusFailed || got.ErrorCode != "SCRIPT_ERROR" || !strings.Contains(got.ErrorMessage, "boom") || !strings.Contains(got.Log, "about to fail") {
		t.Fatalf("%+v", got)
	}

	if _, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Child", ScriptID: child}, "u1"); err != nil {
		t.Fatal(err)
	}
	pj, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "Parent", ScriptID: parent}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	prun, _ := e.sched.RunNow(e.ctx, e.ws, pj.ID, jobs.TriggerManual, "u1", nil)
	if p := e.wait(t, prun.ID); p.Status != jobs.StatusSucceeded {
		t.Fatalf("parent %+v", p)
	}
	cj, _ := e.svc.ByName(e.ctx, e.l, "child")
	deadline := time.Now().Add(5 * time.Second)
	for {
		runs, _ := e.svc.Runs(e.ctx, e.l, cj.ID, 10, time.Time{})
		if len(runs) == 1 && runs[0].FinishedAt != nil {
			full, _ := e.svc.GetRun(e.ctx, e.l, runs[0].ID)
			if full.TriggerKind != jobs.TriggerEnqueue || !strings.Contains(full.Log, "child x=7") {
				t.Fatalf("%+v", full)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never ran")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Forms may only enqueue jobs that opt in.
	if _, err := e.svc.Enqueue(e.ctx, e.l, "Child", "u2", nil, true); fault.Classify(err).Code != fault.Forbidden {
		t.Fatalf("form enqueue: %v", err)
	}
	if _, err := e.svc.Enqueue(e.ctx, e.l, "Nope", "u2", nil, false); fault.Classify(err).Code != fault.NotFound {
		t.Fatalf("missing: %v", err)
	}
}

func TestJobValidationAndOCC(t *testing.T) {
	e := setup(t)
	sid := e.script(t, "J", `return 1`)
	mod, _ := (&scripts.Service{}).Put(e.ctx, e.l, "", 0, scripts.ScriptInput{Name: "shared", Kind: "module", Source: "return {}"}, "u1")
	for _, in := range []jobs.JobInput{
		{Name: "", ScriptID: sid},
		{Name: "x", ScriptID: "nope"},
		{Name: "x", ScriptID: mod.ID},
		{Name: "x", ScriptID: sid, Schedule: "every day"},
		{Name: "x", ScriptID: sid, Timezone: "Nowhere/City"},
		{Name: "x", ScriptID: sid, TimeoutSeconds: 4000},
		{Name: "x", ScriptID: sid, Args: map[string]any{"big": strings.Repeat("a", 70<<10)}},
	} {
		if _, err := e.svc.Put(e.ctx, e.l, "", 0, in, "u1"); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%+v: %v", in.Name+in.Schedule+in.Timezone, err)
		}
	}
	j, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "J", ScriptID: sid}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "j", ScriptID: sid}, "u1"); fault.Classify(err).Code != fault.DuplicateValue {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := e.svc.Put(e.ctx, e.l, j.ID, j.Version+1, jobs.JobInput{Name: "J2", ScriptID: sid}, "u1"); fault.Classify(err).Code != fault.ConcurrentModification {
		t.Fatalf("occ: %v", err)
	}
	j2, err := e.svc.Put(e.ctx, e.l, j.ID, j.Version, jobs.JobInput{Name: "J2", ScriptID: sid, Schedule: "@hourly"}, "u1")
	if err != nil || j2.Version != 2 || j2.Schedule != "@hourly" {
		t.Fatalf("%+v %v", j2, err)
	}
	if err := e.svc.Delete(e.ctx, e.l, j.ID, 1); fault.Classify(err).Code != fault.ConcurrentModification {
		t.Fatalf("delete occ: %v", err)
	}
	if err := e.svc.Delete(e.ctx, e.l, j.ID, 2); err != nil {
		t.Fatal(err)
	}
	if ix, _ := e.store.Entries(e.ctx, e.ws); len(ix) != 0 {
		t.Fatalf("index not cleaned: %+v", ix)
	}
}

func TestRecoverWithRealStore(t *testing.T) {
	e := setup(t)
	sid := e.script(t, "J", `return 1`)
	j, _ := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: "J", ScriptID: sid, Schedule: "@daily"}, "u1")
	now := time.Now().UTC()
	if err := e.store.PutRun(e.ctx, e.ws, jobs.Run{ID: ids.New(), JobID: j.ID, TriggerKind: "schedule", Status: "running", Attempt: 1, QueuedAt: now, StartedAt: &now}); err != nil {
		t.Fatal(err)
	}
	runs, _ := e.svc.Runs(e.ctx, e.l, j.ID, 5, time.Time{})
	ix, _ := e.store.Entries(e.ctx, e.ws)
	ix[0].RunningRunID = runs[0].ID
	_ = e.store.PutEntry(e.ctx, ix[0])
	if err := jobs.New(jobs.Config{Store: e.store}).Recover(e.ctx); err != nil {
		t.Fatal(err)
	}
	r, _ := e.svc.GetRun(e.ctx, e.l, runs[0].ID)
	ix, _ = e.store.Entries(e.ctx, e.ws)
	if r.Status != jobs.StatusInterrupted || ix[0].RunningRunID != "" || ix[0].NextRunAt.IsZero() {
		t.Fatalf("%+v %+v", r, ix[0])
	}
	// NextWake sees the scheduled job.
	if at, ok, err := e.store.NextWake(e.ctx); err != nil || !ok || !at.Equal(ix[0].NextRunAt) {
		t.Fatalf("wake %v %v %v", at, ok, err)
	}
}

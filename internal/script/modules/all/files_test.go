package all_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

type filesRig struct {
	t    *testing.T
	ctx  context.Context
	dir  string
	m    *workspace.Manager
	l    *workspace.Lease
	svc  *files.Service
	data *host.Data
	tbl  *catalog.Table
}

func newFilesRig(t *testing.T) *filesRig {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	w := wstest.New(t)
	m := w.Manager
	ws := w.Create(t)
	l, err := m.Acquire(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Release)
	cache := catalog.NewCache()
	if _, err := (&catalog.Engine{}).Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Reports",
		Columns: []catalog.ColumnSpec{{DisplayName: "Title", Type: catalog.ShortText}, {DisplayName: "Attachment", Type: catalog.Attachments}}}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, _ := cache.Get(ctx, l)
	tbl, _ := sch.TableByPhysical("Reports")
	svc := &files.Service{DataDir: dir}
	return &filesRig{t: t, ctx: ctx, dir: dir, m: m, l: l, svc: svc, tbl: tbl,
		data: &host.Data{Lease: l, Schemas: cache, Records: &records.Service{Schemas: cache}, Files: svc, CanWrite: true, UserID: "u1"}}
}

func (f *filesRig) run(kind script.Context, d *host.Data, src string) (script.ExecResult, error) {
	f.t.Helper()
	proto, err := script.Compile(src, "t")
	if err != nil {
		f.t.Fatal(err)
	}
	return runner.Exec(f.ctx, script.ExecRequest{Kind: kind, Workspace: f.l.ID(), Proto: proto, Host: d})
}

func (f *filesRig) upload(name string, data []byte) files.Meta {
	f.t.Helper()
	m, err := f.svc.Upload(f.ctx, f.l, "u1", name, bytes.NewReader(data))
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func TestFilesModuleJobCreateAttachRead(t *testing.T) {
	f := newFilesRig(t)
	res, err := f.run(script.Job, f.data, `
		local info = files.create("totals.csv", "id,total\n1,10\n2,20\n", "text/plain")
		local rid = records.insert("Reports", {Title = "March", Attachment = {info.id}})
		local row = records.get("Reports", rid)
		local again = files.info(row.Attachment[1])
		local rows = csv.parse(files.read(again.id))
		return info.name, info.content_type, info.size, info.state, again.state, #row.Attachment, #rows, rows[2].total, files.info("01ARZ3NDEKTSV4RRFFQ69G5FAV") == nil, files.read("nope") == nil
	`)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"totals.csv", "text/csv", int64(19), "pending", "attached", int64(1), int64(2), "20", true, true}
	for i, w := range want {
		if res.Values[i] != w {
			t.Fatalf("value %d = %#v, want %#v (all %#v)", i, res.Values[i], w, res.Values)
		}
	}
	// Clearing the field with an empty list releases the file.
	if _, err := f.run(script.Job, f.data, `records.update("Reports", 1, {Attachment = {}})`); err != nil {
		t.Fatal(err)
	}
	res, err = f.run(script.Job, f.data, `return records.get("Reports", 1).Attachment == db.NULL`)
	if err != nil || res.Values[0] != true {
		t.Fatalf("cleared field: %v %v", err, res.Values)
	}
	// The detected type wins over whatever the script says.
	res, err = f.run(script.Job, f.data, `return files.create("x.png", "<html>hi</html>", "image/png").content_type`)
	if err != nil || res.Values[0] != "text/html" {
		t.Fatalf("content type: %v %v", err, res.Values)
	}
}

func TestFilesModuleContextRestrictions(t *testing.T) {
	f := newFilesRig(t)
	small := f.upload("small.txt", []byte("hello"))
	big := f.upload("big.bin", bytes.Repeat([]byte{7, 8, 9}, 500_000)) // 1.5 MB
	huge := f.upload("huge.bin", bytes.Repeat([]byte{1, 2, 3, 4}, 3_000_000))

	// Form events and reports: info and read, no create.
	for _, kind := range []script.Context{script.UIEvent, script.Report} {
		res, err := f.run(kind, f.data, `return files.create == nil, files.info("`+small.ID+`").size, files.read("`+small.ID+`")`)
		if err != nil || res.Values[0] != true || res.Values[1] != int64(5) || res.Values[2] != "hello" {
			t.Fatalf("%s: %v %v", kind, err, res.Values)
		}
		// 1.5 MB is over the 1 MiB read limit there.
		_, err = f.run(kind, f.data, `files.read("`+big.ID+`")`)
		if fault.Classify(err).Code != fault.BadRequest || !strings.Contains(err.Error(), "at most") {
			t.Fatalf("%s big read: %v", kind, err)
		}
	}
	// Jobs read up to 10 MiB.
	if res, err := f.run(script.Job, f.data, `return #files.read("`+big.ID+`")`); err != nil || res.Values[0] != int64(1_500_000) {
		t.Fatalf("job read: %v %v", err, res.Values)
	}
	if _, err := f.run(script.Job, f.data, `files.read("`+huge.ID+`")`); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("12 MB read in a job: %v", err)
	}
	// Reads add up against the run's memory budget (16 MiB in jobs).
	_, err := f.run(script.Job, f.data, `for i = 1, 20 do files.read("`+big.ID+`") end`)
	var se *script.Error
	if !errors.As(err, &se) || se.Kind != script.KindLimit {
		t.Fatalf("cumulative reads were not limited: %v", err)
	}
	// Without a files service or a lease the module refuses cleanly.
	if _, err := f.run(script.Job, &host.Data{}, `files.info("x")`); fault.Classify(err).Code != fault.Forbidden {
		t.Fatalf("no binding: %v", err)
	}
	// Read-only data access cannot create.
	ro := *f.data
	ro.CanWrite = false
	if _, err := f.run(script.Job, &ro, `files.create("a.txt", "x")`); fault.Classify(err).Code != fault.Forbidden {
		t.Fatalf("read-only create: %v", err)
	}
	// Not inside a transaction block.
	if _, err := f.run(script.Job, f.data, `db.transaction(function() files.create("a.txt", "x") end)`); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("create in transaction: %v", err)
	}
}

func TestFilesModuleIsWorkspaceScoped(t *testing.T) {
	f := newFilesRig(t)
	other := ids.NewWorkspaceID()
	if err := f.m.Create(f.ctx, other); err != nil {
		t.Fatal(err)
	}
	ol, err := f.m.Acquire(f.ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	defer ol.Release()
	foreign, err := f.svc.Upload(f.ctx, ol, "u", "f.txt", strings.NewReader("foreign"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.run(script.Job, f.data, `return files.info("`+foreign.ID+`") == nil, files.read("`+foreign.ID+`") == nil`)
	if err != nil || res.Values[0] != true || res.Values[1] != true {
		t.Fatalf("%v %v", err, res.Values)
	}
	if _, err := f.run(script.Job, f.data, `records.insert("Reports", {Attachment = {"`+foreign.ID+`"}})`); fault.Classify(err).Code != fault.BadRequest {
		t.Fatalf("attached a foreign file: %v", err)
	}
}

package files_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/files"
	"abidata/internal/ids"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

type rig struct {
	t     *testing.T
	ctx   context.Context
	dir   string
	m     *workspace.Manager
	l     *workspace.Lease
	svc   *files.Service
	rec   *records.Service
	cache *catalog.Cache
	eng   *catalog.Engine
	tbl   *catalog.Table
	att   catalog.Column
	title catalog.Column
	now   time.Time
	quota int64
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	m, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	id := ids.NewWorkspaceID()
	if err := m.Create(ctx, id); err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	r := &rig{t: t, ctx: ctx, dir: dir, m: m, l: l, cache: catalog.NewCache(), eng: &catalog.Engine{}, now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	r.svc = &files.Service{DataDir: dir, Now: func() time.Time { return r.now }, Quota: func(ids.WorkspaceID) int64 { return r.quota }}
	r.rec = &records.Service{Schemas: r.cache, Now: func() time.Time { return r.now }}
	r.apply(catalog.Op{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sightings", TrackHistory: true,
		Columns: []catalog.ColumnSpec{
			{DisplayName: "Title", Type: catalog.ShortText},
			{DisplayName: "Photos", Type: catalog.Attachments, Options: catalog.TypeOptions{MaxFiles: 3}},
		}}})
	r.reload()
	return r
}

func (r *rig) apply(ops ...catalog.Op) {
	r.t.Helper()
	if _, err := r.eng.Run(r.ctx, r.l, ops, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		r.t.Fatal(err)
	}
	r.cache.Drop(r.l.ID())
	r.reload()
}

func (r *rig) reload() {
	sch, err := r.cache.Get(r.ctx, r.l)
	if err != nil {
		r.t.Fatal(err)
	}
	r.tbl, _ = sch.TableByPhysical("Sightings")
	if r.tbl == nil {
		return
	}
	if r.att.ID == "" {
		a, _ := r.tbl.ColumnByPhysical("Photos")
		ti, _ := r.tbl.ColumnByPhysical("Title")
		r.att, r.title = *a, *ti
		return
	}
	a, _ := r.tbl.Column(r.att.ID)
	ti, _ := r.tbl.Column(r.title.ID)
	r.att, r.title = *a, *ti
}

func pngBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeaderOnly is a PNG whose header claims w x h pixels and nothing else:
// enough for image.DecodeConfig, useless to decode (a decompression bomb).
func pngHeaderOnly(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2
	chunk := append([]byte("IHDR"), ihdr...)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func (r *rig) upload(name string, data []byte) files.Meta {
	r.t.Helper()
	m, err := r.svc.Upload(r.ctx, r.l, "u1", name, bytes.NewReader(data))
	if err != nil {
		r.t.Fatal(err)
	}
	return m
}

func (r *rig) insert(title string, fileIDs ...string) (records.Row, error) {
	fields := map[string]any{r.title.ID: title}
	if fileIDs != nil {
		list := make([]any, len(fileIDs))
		for i, id := range fileIDs {
			list[i] = id
		}
		fields[r.att.ID] = list
	}
	return r.rec.Insert(r.ctx, r.l, r.tbl.ID, fields, records.Actor{UserID: "u1"}, nil)
}

func (r *rig) setFiles(row records.Row, fileIDs ...string) (records.Row, error) {
	list := make([]any, len(fileIDs))
	for i, id := range fileIDs {
		list[i] = id
	}
	return r.rec.Update(r.ctx, r.l, r.tbl.ID, row.ID, row.Version, map[string]any{r.att.ID: list}, records.Actor{UserID: "u1"}, nil)
}

func (r *rig) fileState(id string) (state string, unref *string) {
	r.t.Helper()
	err := r.l.Read(r.ctx, func(q dbx.Querier) error {
		return q.QueryRow(r.ctx, `SELECT state, last_unreferenced_at FROM _sys_files WHERE id = ?`, id).Scan(&state, &unref)
	})
	if err != nil {
		r.t.Fatalf("file %s: %v", id, err)
	}
	return state, unref
}

func (r *rig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.l.Read(r.ctx, func(q dbx.Querier) error { return q.QueryRow(r.ctx, sql, args...).Scan(&n) }); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *rig) blobPath(m files.Meta) string {
	return filepath.Join(r.dir, "workspaces", string(r.l.ID())+".files", m.SHA256[:2], m.SHA256[2:4], m.SHA256)
}

func code(err error) fault.Code {
	if err == nil {
		return ""
	}
	return fault.Classify(err).Code
}

func TestUploadMetadataLayoutAndDedupe(t *testing.T) {
	r := newRig(t)
	data := pngBytes(t, 40, 20)
	m := r.upload("../../etc/Photo 1.png", data)
	if m.Name != "Photo 1.png" || m.ContentType != "image/png" || m.State != "pending" || m.Size != int64(len(data)) {
		t.Fatalf("meta %+v", m)
	}
	if m.Width == nil || *m.Width != 40 || *m.Height != 20 || !m.HasThumbnail {
		t.Fatalf("dimensions %+v", m)
	}
	if _, err := os.Stat(r.blobPath(m)); err != nil {
		t.Fatalf("blob not at its content address: %v", err)
	}
	if got, err := os.ReadFile(r.blobPath(m)); err != nil || !bytes.Equal(got, data) {
		t.Fatal("stored bytes differ")
	}
	// Same bytes again: a new file id, one blob.
	m2 := r.upload("again.png", data)
	if m2.ID == m.ID || m2.SHA256 != m.SHA256 {
		t.Fatalf("dedupe %+v %+v", m, m2)
	}
	var blobs int
	_ = files.Bucket{Dir: filepath.Join(r.dir, "workspaces", string(r.l.ID())+".files")}.Walk(func(files.BlobInfo) error { blobs++; return nil })
	if blobs != 1 {
		t.Fatalf("%d blobs, want 1", blobs)
	}
	// No temp files left behind.
	tmp, _ := os.ReadDir(filepath.Join(r.dir, "workspaces", string(r.l.ID())+".files", ".tmp"))
	if len(tmp) != 0 {
		t.Fatalf("leftover temp files: %v", tmp)
	}
	got, err := r.svc.Get(r.ctx, r.l, m.ID)
	if err != nil || got.SHA256 != m.SHA256 {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := r.svc.Get(r.ctx, r.l, "not-an-id"); code(err) != fault.NotFound {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := r.svc.Get(r.ctx, r.l, ids.New()); code(err) != fault.NotFound {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestSizeLimitEmptyAndQuota(t *testing.T) {
	r := newRig(t)
	r.svc.MaxFileBytes = 1000
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "big.txt", bytes.NewReader(bytes.Repeat([]byte("a"), 1001))); code(err) != fault.BadRequest || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("over limit: %v", err)
	}
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "ok.txt", bytes.NewReader(bytes.Repeat([]byte("a"), 1000))); err != nil {
		t.Fatalf("at limit: %v", err)
	}
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "e.txt", bytes.NewReader(nil)); code(err) != fault.BadRequest {
		t.Fatalf("empty: %v", err)
	}
	// The limit applies while streaming: a reader that never ends stops at max+1.
	endless := io.MultiReader(bytes.NewReader([]byte("x")), zeroes{})
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "z.bin", endless); code(err) != fault.BadRequest {
		t.Fatalf("endless: %v", err)
	}
	if left, _ := os.ReadDir(filepath.Join(r.dir, "workspaces", string(r.l.ID())+".files", ".tmp")); len(left) != 0 {
		t.Fatalf("rejected uploads left temp files: %v", left)
	}

	// Quota counts distinct bytes; re-uploading known bytes is free.
	r.quota = 1500
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "second.txt", bytes.NewReader(bytes.Repeat([]byte("b"), 600))); code(err) != fault.QuotaExceeded {
		t.Fatalf("over quota: %v", err)
	}
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "again.txt", bytes.NewReader(bytes.Repeat([]byte("a"), 1000))); err != nil {
		t.Fatalf("known bytes should not count twice: %v", err)
	}
	used, n, err := r.svc.Usage(r.ctx, r.l)
	if err != nil || used != 1000 || n != 2 {
		t.Fatalf("usage %d bytes %d files %v", used, n, err)
	}
}

type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestSniffingIgnoresClientAndNames(t *testing.T) {
	r := newRig(t)
	html := r.upload("photo.png", []byte("<!DOCTYPE html><html><script>alert(1)</script></html>"))
	if html.ContentType != "text/html" || html.Width != nil || html.HasThumbnail {
		t.Fatalf("html named .png: %+v", html)
	}
	if files.Inline(html.ContentType) || files.ServeType(html.ContentType) != "application/octet-stream" {
		t.Fatal("html would be served as html")
	}
	svg := r.upload("logo.svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if files.Inline(svg.ContentType) || files.ServeType(svg.ContentType) != "application/octet-stream" {
		t.Fatalf("svg: %+v", svg)
	}
	csv := r.upload("data.csv", []byte("a,b\n1,2\n"))
	if csv.ContentType != "text/csv" {
		t.Fatalf("csv: %+v", csv)
	}
	pdf := r.upload("x", []byte("%PDF-1.7\n%âãÏÓ\n1 0 obj\n"))
	if pdf.ContentType != "application/pdf" || files.Inline(pdf.ContentType) {
		t.Fatalf("pdf: %+v", pdf)
	}
	// A JPEG is a JPEG whatever it is called.
	var jb bytes.Buffer
	_ = jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil)
	j := r.upload("notes.txt", jb.Bytes())
	if j.ContentType != "image/jpeg" || !files.Inline(j.ContentType) {
		t.Fatalf("jpeg: %+v", j)
	}
	// Corrupt "images" are refused rather than served as images.
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "bad.png", bytes.NewReader(append([]byte("\x89PNG\r\n\x1a\n"), 1, 2, 3))); code(err) != fault.BadRequest {
		t.Fatalf("corrupt png: %v", err)
	}
}

func TestDecompressionBombRefused(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.Upload(r.ctx, r.l, "u", "bomb.png", bytes.NewReader(pngHeaderOnly(50000, 50000)))
	if code(err) != fault.BadRequest || !strings.Contains(err.Error(), "megapixel") {
		t.Fatalf("bomb accepted: %v", err)
	}
	// Exactly at the cap is fine (6324 x 6324 is just under 40 MP).
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "big.png", bytes.NewReader(pngHeaderOnly(6324, 6324))); err != nil {
		t.Fatalf("under the cap: %v", err)
	}
	if _, err := r.svc.Upload(r.ctx, r.l, "u", "toobig.png", bytes.NewReader(pngHeaderOnly(6400, 6400))); code(err) != fault.BadRequest {
		t.Fatalf("over the cap: %v", err)
	}
	// The thumbnailer re-checks and never decodes it.
	if _, err := files.MakeThumbnail(r.ctx, bytes.NewReader(pngHeaderOnly(50000, 50000)), "image/png"); err == nil {
		t.Fatal("thumbnail of a bomb")
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"photo.jpg":                       "photo.jpg",
		`C:\Users\me\Desktop\a b.pdf`:     "a b.pdf",
		"../../../etc/passwd":             "passwd",
		"a\x00b\x1f\x7fc.txt":             "abc.txt",
		"  spaced   out  .txt":            "spaced out .txt",
		"evil\u202etxt.exe":               "eviltxt.exe",
		"":                                "file",
		"...":                             "file",
		"/":                               "file",
		"nul\u200bl.txt":                  "null.txt",
		strings.Repeat("é", 400) + ".png": strings.Repeat("é", 251) + ".png",
	}
	for in, want := range cases {
		if got := files.SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRefsFollowEveryWritePath(t *testing.T) {
	r := newRig(t)
	a, b, c := r.upload("a.txt", []byte("aaa")), r.upload("b.txt", []byte("bbb")), r.upload("c.txt", []byte("ccc"))

	row, err := r.insert("one", a.ID, b.ID, a.ID) // duplicate dropped, order kept
	if err != nil {
		t.Fatal(err)
	}
	if got := row.Fields[r.att.ID]; fmt.Sprint(got) != fmt.Sprint([]string{a.ID, b.ID}) {
		t.Fatalf("stored %v", got)
	}
	for _, m := range []files.Meta{a, b} {
		if st, un := r.fileState(m.ID); st != "attached" || un != nil {
			t.Fatalf("%s is %s %v", m.Name, st, un)
		}
	}
	if st, _ := r.fileState(c.ID); st != "pending" {
		t.Fatalf("c is %s", st)
	}
	if n := r.count(`SELECT count(*) FROM _sys_file_refs`); n != 2 {
		t.Fatalf("%d refs", n)
	}

	// Update: replace b with c. b loses its last reference.
	row, err = r.setFiles(row, a.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st, un := r.fileState(b.ID); st != "attached" || un == nil {
		t.Fatalf("b after detach: %s %v", st, un)
	}
	if st, un := r.fileState(c.ID); st != "attached" || un != nil {
		t.Fatalf("c after attach: %s %v", st, un)
	}
	// Re-attaching b clears its grace period.
	row, err = r.setFiles(row, b.ID, a.ID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, un := r.fileState(b.ID); un != nil {
		t.Fatal("b still marked unreferenced")
	}
	// Clearing the field with [] / null stores NULL and drops all references.
	row, err = r.setFiles(row)
	if err != nil {
		t.Fatal(err)
	}
	if row.Fields[r.att.ID] != nil || r.count(`SELECT count(*) FROM _sys_file_refs`) != 0 {
		t.Fatalf("clear: %v refs=%d", row.Fields[r.att.ID], r.count(`SELECT count(*) FROM _sys_file_refs`))
	}

	// Batch insert + update + delete in one transaction.
	row, _ = r.setFiles(row, a.ID)
	res, err := r.rec.Batch(r.ctx, r.l, r.tbl.ID, []records.BatchOp{
		{Op: "insert", Fields: map[string]any{r.att.ID: []any{b.ID}}},
		{Op: "update", ID: row.ID, Version: row.Version, Fields: map[string]any{r.att.ID: []any{c.ID}}},
	}, records.Actor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := r.count(`SELECT count(*) FROM _sys_file_refs`); n != 2 {
		t.Fatalf("after batch: %d refs", n)
	}
	// A failing batch rolls the references back with the rows.
	_, err = r.rec.Batch(r.ctx, r.l, r.tbl.ID, []records.BatchOp{
		{Op: "insert", Fields: map[string]any{r.att.ID: []any{a.ID}}},
		{Op: "insert", Fields: map[string]any{r.att.ID: []any{ids.New()}}},
	}, records.Actor{}, nil)
	if code(err) != fault.BadRequest {
		t.Fatalf("batch with unknown file: %v", err)
	}
	if n := r.count(`SELECT count(*) FROM _sys_file_refs`); n != 2 {
		t.Fatalf("rolled-back batch changed refs: %d", n)
	}

	// Deleting rows releases their references.
	for _, br := range res {
		if err := r.rec.Delete(r.ctx, r.l, r.tbl.ID, br.Row.ID, br.Row.Version, records.Actor{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.count(`SELECT count(*) FROM _sys_file_refs`); n != 0 {
		t.Fatalf("after delete: %d refs", n)
	}
	if _, un := r.fileState(b.ID); un == nil {
		t.Fatal("b should be unreferenced after its row was deleted")
	}
}

func TestWritesValidateIDs(t *testing.T) {
	r := newRig(t)
	a := r.upload("a.txt", []byte("aaa"))
	// Unknown, malformed, too many.
	if _, err := r.insert("x", ids.New()); code(err) != fault.BadRequest || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := r.rec.Insert(r.ctx, r.l, r.tbl.ID, map[string]any{r.att.ID: []any{"nope"}}, records.Actor{}, nil); code(err) != fault.BadRequest {
		t.Fatalf("malformed id: %v", err)
	}
	if _, err := r.rec.Insert(r.ctx, r.l, r.tbl.ID, map[string]any{r.att.ID: "a string"}, records.Actor{}, nil); code(err) != fault.BadRequest {
		t.Fatalf("non-list: %v", err)
	}
	ids4 := []string{a.ID, r.upload("b", []byte("b1")).ID, r.upload("c", []byte("c1")).ID, r.upload("d", []byte("d1")).ID}
	if _, err := r.insert("x", ids4...); code(err) != fault.BadRequest || !strings.Contains(err.Error(), "at most 3") {
		t.Fatalf("max_files: %v", err)
	}
	if _, err := r.insert("x", ids4[:3]...); err != nil {
		t.Fatal(err)
	}

	// A file of another workspace is never accepted, even with a valid id.
	other := ids.NewWorkspaceID()
	if err := r.m.Create(r.ctx, other); err != nil {
		t.Fatal(err)
	}
	ol, err := r.m.Acquire(r.ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	defer ol.Release()
	foreign, err := r.svc.Upload(r.ctx, ol, "u", "f.txt", strings.NewReader("foreign"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.insert("x", foreign.ID); code(err) != fault.BadRequest {
		t.Fatalf("foreign file accepted: %v", err)
	}
	if _, err := r.svc.Get(r.ctx, r.l, foreign.ID); code(err) != fault.NotFound {
		t.Fatalf("foreign file visible: %v", err)
	}
}

// Writes that bypass the row API (scripts' db.exec, imports, restore of a row)
// keep references consistent because the triggers do the bookkeeping.
func TestTriggersCoverRawSQL(t *testing.T) {
	r := newRig(t)
	a, b := r.upload("a.txt", []byte("aaa")), r.upload("b.txt", []byte("bbb"))
	tq, cq := catalog.Quote(r.tbl.PhysicalName), catalog.Quote(r.att.PhysicalName)
	exec := func(sql string, args ...any) error {
		return r.l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error { _, err := tx.Exec(r.ctx, sql, args...); return err })
	}
	if err := exec(`INSERT INTO `+tq+` (`+cq+`) VALUES (?)`, `["`+a.ID+`","`+b.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	if n := r.count(`SELECT count(*) FROM _sys_file_refs`); n != 2 {
		t.Fatalf("%d refs after raw insert", n)
	}
	if err := exec(`UPDATE `+tq+` SET `+cq+` = ?`, `["`+b.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	if n := r.count(`SELECT count(*) FROM _sys_file_refs WHERE file_id = ?`, a.ID); n != 0 {
		t.Fatal("a still referenced")
	}
	// An id that is not a file fails the statement (foreign key on the refs table).
	if err := exec(`UPDATE `+tq+` SET `+cq+` = ?`, `["`+ids.New()+`"]`); err == nil {
		t.Fatal("unknown id accepted by raw SQL")
	}
	// Not an array: refused by the column's CHECK.
	if err := exec(`UPDATE `+tq+` SET `+cq+` = ?`, `{"a":1}`); err == nil {
		t.Fatal("non-array accepted")
	}
	if err := exec(`DELETE FROM ` + tq); err != nil {
		t.Fatal(err)
	}
	if n := r.count(`SELECT count(*) FROM _sys_file_refs`); n != 0 {
		t.Fatalf("%d refs after raw delete", n)
	}
}

func TestSchemaChangesKeepTriggers(t *testing.T) {
	r := newRig(t)
	a := r.upload("a.txt", []byte("aaa"))
	row, err := r.insert("one", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	refs := func() int { return r.count(`SELECT count(*) FROM _sys_file_refs`) }
	if refs() != 1 {
		t.Fatal("setup")
	}

	// A rebuild (changing another field's size) must keep the triggers.
	r.apply(catalog.Op{Kind: catalog.OpAlterColumn, TableID: r.tbl.ID, ColumnID: r.title.ID,
		Column: &catalog.ColumnSpec{DisplayName: "Title", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: 50}}})
	b := r.upload("b.txt", []byte("bbb"))
	row, err = r.rec.Get(r.ctx, r.l, r.tbl.ID, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.setFiles(row, b.ID); err != nil {
		t.Fatal(err)
	}
	if refs() != 1 || r.count(`SELECT count(*) FROM _sys_file_refs WHERE file_id = ?`, b.ID) != 1 {
		t.Fatalf("refs after rebuild: %d", refs())
	}

	// Rename the column physically.
	r.apply(catalog.Op{Kind: catalog.OpRenameColumn, TableID: r.tbl.ID, ColumnID: r.att.ID, DisplayName: "Pictures", PhysicalName: "Pictures"})
	row, _ = r.rec.Get(r.ctx, r.l, r.tbl.ID, row.ID)
	if _, err = r.setFiles(row, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if refs() != 2 {
		t.Fatalf("refs after rename: %d", refs())
	}

	// A second attachments column is covered too (native ADD COLUMN path).
	r.apply(catalog.Op{Kind: catalog.OpAddColumn, TableID: r.tbl.ID, Column: &catalog.ColumnSpec{DisplayName: "Permits", Type: catalog.Attachments}})
	permits, _ := r.tbl.ColumnByPhysical("Permits")
	row, _ = r.rec.Get(r.ctx, r.l, r.tbl.ID, row.ID)
	if _, err = r.rec.Update(r.ctx, r.l, r.tbl.ID, row.ID, row.Version, map[string]any{permits.ID: []any{a.ID}}, records.Actor{}, nil); err != nil {
		t.Fatal(err)
	}
	if refs() != 3 {
		t.Fatalf("refs with two columns: %d", refs())
	}

	// Type changes to or from attachments are refused.
	_, err = r.eng.Run(r.ctx, r.l, []catalog.Op{{Kind: catalog.OpAlterColumn, TableID: r.tbl.ID, ColumnID: permits.ID,
		Column: &catalog.ColumnSpec{DisplayName: "Permits", Type: catalog.LongText}}}, catalog.Apply, catalog.ApplyOptions{})
	if err == nil {
		t.Fatal("attachments -> long_text accepted")
	}
	// Dropping a column releases its references; dropping the table releases the rest.
	r.apply(catalog.Op{Kind: catalog.OpDropColumn, TableID: r.tbl.ID, ColumnID: permits.ID})
	if refs() != 2 {
		t.Fatalf("refs after dropping a column: %d", refs())
	}
	r.apply(catalog.Op{Kind: catalog.OpDropTable, TableID: r.tbl.ID})
	if refs() != 0 {
		t.Fatalf("refs after dropping the table: %d", refs())
	}
	if _, un := r.fileState(a.ID); un == nil {
		t.Fatal("a should start its grace period when its table is dropped")
	}
}

func TestMaxFilesOptionValidation(t *testing.T) {
	r := newRig(t)
	for _, bad := range []int{-1, 101} {
		_, err := r.eng.Run(r.ctx, r.l, []catalog.Op{{Kind: catalog.OpAddColumn, TableID: r.tbl.ID,
			Column: &catalog.ColumnSpec{DisplayName: "X", Type: catalog.Attachments, Options: catalog.TypeOptions{MaxFiles: bad}}}}, catalog.Apply, catalog.ApplyOptions{})
		if err == nil {
			t.Fatalf("max_files %d accepted", bad)
		}
	}
	r.apply(catalog.Op{Kind: catalog.OpAddColumn, TableID: r.tbl.ID, Column: &catalog.ColumnSpec{DisplayName: "Docs", Type: catalog.Attachments}})
	c, _ := r.tbl.ColumnByPhysical("Docs")
	if c.Options.MaxFiles != catalog.DefaultMaxFiles || c.MaxFilesLimit() != 20 {
		t.Fatalf("default max_files %d", c.Options.MaxFiles)
	}
	// Existing data survives lowering max_files; only new writes are checked.
	r.apply(catalog.Op{Kind: catalog.OpAlterColumn, TableID: r.tbl.ID, ColumnID: c.ID,
		Column: &catalog.ColumnSpec{DisplayName: "Docs", Type: catalog.Attachments, Options: catalog.TypeOptions{MaxFiles: 1}}})
	c, _ = r.tbl.ColumnByPhysical("Docs")
	if c.Options.MaxFiles != 1 {
		t.Fatal("max_files not updated")
	}
}

// Concurrent writers must leave _sys_file_refs exactly equal to what the cells say.
func TestRefsConsistentUnderConcurrentWrites(t *testing.T) {
	r := newRig(t)
	r.apply(catalog.Op{Kind: catalog.OpAlterColumn, TableID: r.tbl.ID, ColumnID: r.att.ID,
		Column: &catalog.ColumnSpec{DisplayName: "Photos", Type: catalog.Attachments, Options: catalog.TypeOptions{MaxFiles: 3}}})
	var pool []string
	for i := range 8 {
		pool = append(pool, r.upload(fmt.Sprintf("f%d.txt", i), []byte(fmt.Sprintf("file %d", i))).ID)
	}
	var rows []records.Row
	for i := range 6 {
		row, err := r.insert(fmt.Sprintf("r%d", i), pool[i%len(pool)])
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	var wg sync.WaitGroup
	for w := range 6 {
		wg.Go(func() {
			for i := range 25 {
				row := rows[(w+i)%len(rows)]
				cur, err := r.rec.Get(r.ctx, r.l, r.tbl.ID, row.ID)
				if err != nil {
					continue
				}
				n := 1 + (w+i)%3
				list := make([]any, 0, n)
				for k := range n {
					list = append(list, pool[(w*3+i+k)%len(pool)])
				}
				switch i % 5 {
				case 0:
					_, _ = r.rec.Update(r.ctx, r.l, r.tbl.ID, cur.ID, cur.Version, map[string]any{r.att.ID: nil}, records.Actor{}, nil)
				case 1:
					if nr, err := r.insert("new", pool[i%len(pool)]); err == nil {
						_ = r.rec.Delete(r.ctx, r.l, r.tbl.ID, nr.ID, nr.Version, records.Actor{}, nil)
					}
				default:
					_, _ = r.rec.Update(r.ctx, r.l, r.tbl.ID, cur.ID, cur.Version, map[string]any{r.att.ID: list}, records.Actor{}, nil)
				}
			}
		})
	}
	wg.Wait()

	want := map[string]bool{}
	err := r.l.Read(r.ctx, func(q dbx.Querier) error {
		rs, err := q.Query(r.ctx, `SELECT t.id, j.value FROM `+catalog.Quote(r.tbl.PhysicalName)+` t, json_each(t.`+catalog.Quote(r.att.PhysicalName)+`) j`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var id int64
			var fid string
			if err := rs.Scan(&id, &fid); err != nil {
				return err
			}
			want[fmt.Sprintf("%s/%d", fid, id)] = true
		}
		return rs.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	err = r.l.Read(r.ctx, func(q dbx.Querier) error {
		rs, err := q.Query(r.ctx, `SELECT file_id, row_id FROM _sys_file_refs`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var id int64
			var fid string
			if err := rs.Scan(&fid, &id); err != nil {
				return err
			}
			got[fmt.Sprintf("%s/%d", fid, id)] = true
		}
		return rs.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("refs %d, cells %d", len(got), len(want))
	}
	for k := range want {
		if !got[k] {
			t.Fatalf("missing ref %s", k)
		}
	}
	// And the file states agree: attached iff referenced or once referenced.
	if n := r.count(`SELECT count(*) FROM _sys_files f WHERE EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = f.id) AND (f.state <> 'attached' OR f.last_unreferenced_at IS NOT NULL)`); n != 0 {
		t.Fatalf("%d referenced files not marked attached", n)
	}
	if n := r.count(`SELECT count(*) FROM _sys_files f WHERE NOT EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = f.id) AND f.state = 'attached' AND f.last_unreferenced_at IS NULL`); n != 0 {
		t.Fatalf("%d unreferenced files without a grace start", n)
	}
}

func TestSweepTimingWithFakeClock(t *testing.T) {
	r := newRig(t)
	pending := r.upload("pending.txt", []byte("pending"))
	kept := r.upload("kept.txt", []byte("kept"))
	dropped := r.upload("dropped.txt", []byte("dropped"))
	row, err := r.insert("x", kept.ID, dropped.ID)
	if err != nil {
		t.Fatal(err)
	}
	// "dropped" loses its reference; the trigger stamps real time, so set the
	// stamp from the fake clock for a deterministic test.
	if _, err = r.setFiles(row, kept.ID); err != nil {
		t.Fatal(err)
	}
	stamp := func(id string, at time.Time) {
		err := r.l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error {
			_, err := tx.Exec(r.ctx, `UPDATE _sys_files SET last_unreferenced_at = ? WHERE id = ?`, dbx.FormatTime(at), id)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	stamp(dropped.ID, r.now)
	setCreated := func(id string, at time.Time) {
		err := r.l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error {
			_, err := tx.Exec(r.ctx, `UPDATE _sys_files SET created_at = ? WHERE id = ?`, dbx.FormatTime(at), id)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sweep := func(at time.Time) files.SweepResult {
		res, err := r.svc.Sweep(r.ctx, r.l, at)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	exists := func(m files.Meta) bool { _, err := os.Stat(r.blobPath(m)); return err == nil }

	// Just under the limits nothing goes.
	if res := sweep(r.now.Add(files.PendingTTL - time.Minute)); res.PurgedPending+res.PurgedUnreferenced != 0 {
		t.Fatalf("early sweep purged: %+v", res)
	}
	// Past 24h the pending file goes; the unreferenced one stays for its 7 days.
	res := sweep(r.now.Add(files.PendingTTL + time.Minute))
	if res.PurgedPending != 1 || res.PurgedUnreferenced != 0 || exists(pending) || !exists(dropped) || !exists(kept) {
		t.Fatalf("after 24h: %+v pending=%v dropped=%v", res, exists(pending), exists(dropped))
	}
	if _, err := r.svc.Get(r.ctx, r.l, pending.ID); code(err) != fault.NotFound {
		t.Fatalf("purged file still readable: %v", err)
	}
	// Day 6: still there. The row history of the update names it within the window.
	if res := sweep(r.now.Add(6 * 24 * time.Hour)); res.PurgedUnreferenced != 0 || !exists(dropped) {
		t.Fatalf("day 6: %+v", res)
	}
	// Day 8: the stamp is old enough, but history written "now" (real clock time
	// in the fake timeline: 2026-03-01) is within 7 days of day 8? No: 8 days later it is not.
	res = sweep(r.now.Add(8 * 24 * time.Hour))
	if res.PurgedUnreferenced != 1 || exists(dropped) || !exists(kept) {
		t.Fatalf("day 8: %+v dropped=%v kept=%v", res, exists(dropped), exists(kept))
	}
	// The attached, referenced file is never touched, however old.
	if res := sweep(r.now.Add(400 * 24 * time.Hour)); res.PurgedUnreferenced != 0 || res.PurgedPending != 0 || !exists(kept) {
		t.Fatalf("referenced file swept: %+v", res)
	}
	_ = setCreated
}

func TestSweepKeepsFilesNamedByRecentHistory(t *testing.T) {
	r := newRig(t)
	a := r.upload("a.txt", []byte("aaa"))
	row, _ := r.insert("x", a.ID)
	if _, err := r.setFiles(row); err != nil { // history "after" no longer names a, "before" does
		t.Fatal(err)
	}
	// Pretend the grace stamp is old, but the history entry is from the fake clock's "now".
	if err := r.l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(r.ctx, `UPDATE _sys_files SET last_unreferenced_at = ?`, dbx.FormatTime(r.now.Add(-30*24*time.Hour)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Sweep(r.ctx, r.l, r.now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if res.PurgedUnreferenced != 0 || res.Skipped != 1 {
		t.Fatalf("file named by recent history was purged: %+v", res)
	}
	if _, err := os.Stat(r.blobPath(a)); err != nil {
		t.Fatal("blob removed")
	}
	// Once that history is older than the grace period it can go.
	res, err = r.svc.Sweep(r.ctx, r.l, r.now.Add(8*24*time.Hour))
	if err != nil || res.PurgedUnreferenced != 1 {
		t.Fatalf("later: %+v %v", res, err)
	}
}

func TestSweepKeepsSharedBlobsAndPrunesDirs(t *testing.T) {
	r := newRig(t)
	data := []byte("same bytes")
	a, b := r.upload("a.txt", data), r.upload("b.txt", data)
	row, _ := r.insert("x", b.ID) // b is used, a is not
	_ = row
	// a expires as pending; its blob is shared with b and must stay.
	res, err := r.svc.Sweep(r.ctx, r.l, r.now.Add(files.PendingTTL+time.Hour))
	if err != nil || res.PurgedPending != 1 || res.Bytes != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(r.blobPath(b)); err != nil {
		t.Fatal("shared blob deleted with the other file's row")
	}
	_ = a
	// Free b too (row deleted, grace over): the blob and its empty directories go.
	cur, _ := r.rec.Get(r.ctx, r.l, r.tbl.ID, row.ID)
	if err := r.rec.Delete(r.ctx, r.l, r.tbl.ID, cur.ID, cur.Version, records.Actor{}, nil); err != nil {
		t.Fatal(err)
	}
	_ = r.l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(r.ctx, `UPDATE _sys_files SET last_unreferenced_at = ?`, dbx.FormatTime(r.now.Add(-48*time.Hour)))
		return err
	})
	_ = r.l.Write(r.ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(r.ctx, `DELETE FROM _sys_row_history`)
		return err
	})
	if res, err := r.svc.Sweep(r.ctx, r.l, r.now.Add(8*24*time.Hour)); err != nil || res.PurgedUnreferenced != 1 || res.Bytes != int64(len(data)) {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Dir(r.blobPath(b))); !os.IsNotExist(err) {
		t.Fatalf("empty bucket directory not pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "workspaces", string(r.l.ID())+".files", b.SHA256[:2])); !os.IsNotExist(err) {
		t.Fatal("empty aa directory not pruned")
	}
}

func TestOrphanBlobsAndStaleTempsSwept(t *testing.T) {
	r := newRig(t)
	bk, _ := r.svc.BucketFor(r.l.ID())
	orphan := strings.Repeat("ab", 32)
	p, _ := bk.Path(orphan)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := r.now.Add(-48 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(bk.Dir, ".tmp"), 0o750); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(bk.Dir, ".tmp", "up-stale")
	_ = os.WriteFile(tmp, []byte("x"), 0o600)
	_ = os.Chtimes(tmp, old, old)
	res, err := r.svc.Sweep(r.ctx, r.l, r.now)
	if err != nil || res.Orphans != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("orphan blob kept")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("stale temp kept")
	}
}

func TestThumbnails(t *testing.T) {
	r := newRig(t)
	big := r.upload("big.png", pngBytes(t, 1000, 500))
	f, m, err := r.svc.Thumbnail(r.ctx, r.l, big.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(f)
	_ = f.Close()
	if err != nil || format != "jpeg" || cfg.Width != 320 || cfg.Height != 160 || m.ID != big.ID {
		t.Fatalf("thumbnail %v %s %dx%d", err, format, cfg.Width, cfg.Height)
	}
	// Cached on disk, so a second call needs no decode (the blob can go).
	if _, err := os.Stat(filepath.Join(r.dir, "workspaces", string(r.l.ID())+".files", ".thumb", big.SHA256+".jpg")); err != nil {
		t.Fatalf("thumbnail not cached: %v", err)
	}
	small := r.upload("small.png", pngBytes(t, 30, 60))
	f, _, err = r.svc.Thumbnail(r.ctx, r.l, small.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ = image.DecodeConfig(f)
	_ = f.Close()
	if cfg.Width != 30 || cfg.Height != 60 {
		t.Fatalf("small images are not enlarged: %dx%d", cfg.Width, cfg.Height)
	}
	txt := r.upload("a.txt", []byte("hello"))
	if _, _, err := r.svc.Thumbnail(r.ctx, r.l, txt.ID); code(err) != fault.NotFound {
		t.Fatalf("text thumbnail: %v", err)
	}
}

func TestThumbnailAppliesExifOrientation(t *testing.T) {
	// A 40x20 JPEG tagged "rotate 90 CW" displays as 20x40.
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}
	raw := jb.Bytes()
	exif := []byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x06\x00\x00\x00\x00\x00\x00")
	seg := append([]byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	tagged := append(append(append([]byte{}, raw[:2]...), seg...), raw[2:]...)
	data, err := files.MakeThumbnail(context.Background(), bytes.NewReader(tagged), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 20 || cfg.Height != 40 {
		t.Fatalf("orientation not applied: %v %dx%d", err, cfg.Width, cfg.Height)
	}
}

func TestIntegrityCheckHelper(t *testing.T) {
	r := newRig(t)
	m := r.upload("a.txt", []byte("integrity"))
	if err := files.CheckBlob(r.blobPath(m), m.SHA256, m.Size); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.blobPath(m), []byte("integrity!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := files.CheckBlob(r.blobPath(m), m.SHA256, m.Size); err == nil {
		t.Fatal("tampered blob passed")
	}
}

func TestUploadRateLimit(t *testing.T) {
	r := newRig(t)
	// Hold four uploads open; the fifth is refused.
	var gates []*io.PipeWriter
	var wg sync.WaitGroup
	for i := range 4 {
		pr, pw := io.Pipe()
		gates = append(gates, pw)
		wg.Go(func() {
			_, _ = r.svc.Upload(r.ctx, r.l, "u", fmt.Sprintf("slow%d.txt", i), pr)
		})
	}
	// Wait until all four are inside Upload.
	deadline := time.Now().Add(5 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		_, err = r.svc.Upload(r.ctx, r.l, "u", "fifth.txt", strings.NewReader("x"))
		if code(err) == fault.RateLimited {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code(err) != fault.RateLimited {
		t.Fatalf("expected RATE_LIMITED, got %v", err)
	}
	for _, g := range gates {
		_ = g.Close()
	}
	wg.Wait()
}

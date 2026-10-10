package httpapi

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"abidata/internal/ids"
)

// upload posts a multipart import.
func (c *client) upload(ws, filename, content string, fields map[string]string) (int, map[string]any) {
	c.e.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = io.WriteString(fw, content)
	_ = mw.Close()
	return c.postMultipart("/api/v1/w/"+ws+"/imports", mw.FormDataContentType(), &b, -1)
}

func (c *client) postMultipart(path, ctype string, body io.Reader, contentLength int64) (int, map[string]any) {
	req, _ := http.NewRequest("POST", c.e.srv.URL+path, body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-CSRF-Token", c.csrf)
	if contentLength >= 0 {
		req.ContentLength = contentLength
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (c *client) download(path string) (int, []byte, http.Header) {
	req, _ := http.NewRequest("GET", c.e.srv.URL+path, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

func (c *client) waitOp(base, id string) map[string]any {
	c.e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		st, op, _ := c.do("GET", base+"/operations/"+id, nil)
		if st != 200 {
			c.e.t.Fatalf("operation %d %v", st, op)
		}
		if op["finished_at"] != nil {
			return op
		}
		if time.Now().After(deadline) {
			c.e.t.Fatal("operation timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestImportExportOverHTTP(t *testing.T) {
	e := newEnv(t)
	ws, _, owner, builder, editor, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	csvData := "Name,Price,Active\nWidget,19.99,yes\nGadget,5,no\n\"Thing, large\",1000.5,yes\n"

	// Only builders import; editors and viewers are refused.
	if st, _ := editor.upload(ws, "p.csv", csvData, nil); st != 403 {
		t.Fatalf("editor upload %d", st)
	}
	st, meta := builder.upload(ws, "p.csv", csvData, nil)
	if st != 201 || meta["row_count"] != float64(3) || meta["status"] != "parsed" {
		t.Fatalf("upload %d %v", st, meta)
	}
	id := meta["id"].(string)
	cols := meta["columns"].([]any)
	if cols[1].(map[string]any)["inferred_type"] != "number" || cols[2].(map[string]any)["inferred_type"] != "yes_no" {
		t.Fatalf("%v", cols)
	}
	if st, _, _ := editor.do("GET", base+"/imports/"+id, nil); st != 403 {
		t.Fatalf("editor read import %d", st)
	}
	st, view, _ := builder.do("GET", base+"/imports/"+id, nil)
	if st != 200 || len(view["preview"].([]any)) != 3 {
		t.Fatalf("get %d %v", st, view)
	}

	// Traversal-looking and foreign ids are 404, from another workspace too.
	for _, bad := range []string{"..%2F..%2Fsystem%2Fadmin", "not-an-id", ids.New()} {
		if st, _, _ := builder.do("GET", base+"/imports/"+bad, nil); st != 404 {
			t.Errorf("import id %q: %d", bad, st)
		}
	}
	e.user("other@example.com", false)
	_, wsb, _ := e.login("root@example.com").do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Other", "owner_email": "other@example.com"})
	other := e.login("other@example.com")
	if st, _, _ := other.do("GET", "/api/v1/w/"+wsb["id"].(string)+"/imports/"+id, nil); st != 404 {
		t.Fatalf("cross-workspace import %d", st)
	}
	if st, _, _ := other.do("GET", base+"/imports/"+id, nil); st != 404 {
		t.Fatalf("non-member import %d", st)
	}

	// Map onto a new table, validate, commit.
	mapping := map[string]any{"mode": "append", "new_table": map[string]any{"name": "Products", "columns": []map[string]any{
		{"source": 0, "name": "Name", "field_type": "short_text", "is_required": true},
		{"source": 1, "name": "Price", "field_type": "currency"},
		{"source": 2, "name": "Active", "field_type": "yes_no"}}}}
	if st, b, _ := builder.do("PUT", base+"/imports/"+id+"/mapping", map[string]any{"mode": "upsert", "new_table": mapping["new_table"]}); st != 400 {
		t.Fatalf("bad mapping %d %v", st, b)
	}
	if st, b, _ := builder.do("PUT", base+"/imports/"+id+"/mapping", mapping); st != 200 || b["status"] != "mapped" {
		t.Fatalf("mapping %d %v", st, b)
	}
	st, val, _ := builder.do("POST", base+"/imports/"+id+"/validate", nil)
	if st != 200 || val["error_count"] != float64(0) || val["rows_checked"] != float64(3) {
		t.Fatalf("validate %d %v", st, val)
	}
	st, op, _ := builder.do("POST", base+"/imports/"+id+"/commit", nil)
	if st != 202 {
		t.Fatalf("commit %d %v", st, op)
	}
	done := builder.waitOp(base, op["id"].(string))
	res := done["result"].(map[string]any)
	if done["kind"] != "import" || done["status"] != "succeeded" || res["inserted"] != float64(3) || res["table_created"] != true {
		t.Fatalf("%v", done)
	}
	tid := res["table_id"].(string)
	if st, _, _ := builder.do("GET", base+"/imports/"+id, nil); st != 404 {
		t.Fatalf("committed import still there: %d", st)
	}

	// Viewers (and editors) can export; the file is the operation's artifact.
	for _, tc := range []struct{ format, ctype string }{{"csv", "text/csv"}, {"json", "application/json"}, {"xlsx", "spreadsheetml"}} {
		st, eop, _ := editor.do("POST", base+"/exports", map[string]any{"source_kind": "table", "source_id": tid, "format": tc.format})
		if st != 202 {
			t.Fatalf("export %s %d %v", tc.format, st, eop)
		}
		d := editor.waitOp(base, eop["id"].(string))
		if d["status"] != "succeeded" || d["kind"] != "export" {
			t.Fatalf("%v", d)
		}
		name := d["result"].(map[string]any)["artifact"].(string)
		st2, body, h := editor.download(base + "/operations/" + eop["id"].(string) + "/artifacts/" + name)
		if st2 != 200 || !strings.Contains(h.Get("Content-Type"), tc.ctype) || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
			t.Fatalf("download %s: %d %v", tc.format, st2, h)
		}
		switch tc.format {
		case "csv":
			recs, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
			if err != nil || len(recs) != 4 || strings.Join(recs[3], "|") != "3|Thing, large|1000.5000|true" {
				t.Fatalf("csv %v %v", err, recs)
			}
		case "json":
			if !strings.Contains(string(body), `"Price":"19.9900"`) || !strings.Contains(string(body), `"Active":false`) {
				t.Fatalf("json %s", body)
			}
		case "xlsx":
			f, err := excelize.OpenReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := f.GetRows("Sheet1")
			if len(rows) != 4 || rows[1][2] != "19.9900" {
				t.Fatalf("xlsx %v", rows)
			}
		}
		// Another workspace's operation id is not visible, and neither is a traversal name.
		if st, _, _ := other.do("GET", "/api/v1/w/"+wsb["id"].(string)+"/operations/"+eop["id"].(string), nil); st != 404 {
			t.Fatalf("cross-workspace operation %d", st)
		}
		if st, _, _ := editor.do("GET", base+"/operations/"+eop["id"].(string)+"/artifacts/..%2F..%2Fx", nil); st != 404 {
			t.Fatalf("artifact traversal %d", st)
		}
	}
	if st, b, _ := editor.do("POST", base+"/exports", map[string]any{"source_kind": "table", "source_id": ids.New(), "format": "csv"}); st != 404 {
		t.Fatalf("unknown table %d %v", st, b)
	}
	if st, _, _ := editor.do("POST", base+"/exports", map[string]any{"source_kind": "table", "source_id": tid, "format": "pdf"}); st != 422 && st != 400 {
		t.Fatalf("bad format %d", st)
	}
	// Discarding an import removes it; deleting twice is 404.
	_, m2 := builder.upload(ws, "d.csv", "a\n1\n", nil)
	if st, _, _ := builder.do("DELETE", base+"/imports/"+m2["id"].(string), nil); st != 204 {
		t.Fatalf("delete %d", st)
	}
	if st, _, _ := builder.do("DELETE", base+"/imports/"+m2["id"].(string), nil); st != 404 {
		t.Fatalf("second delete %d", st)
	}
	_ = owner
}

func TestImportUploadLimits(t *testing.T) {
	e := newEnv(t)
	ws, _, _, builder, _, _ := roles(t, e)
	// Declared size beyond the cap is refused before reading the body.
	const declared = 100<<20 + 2<<20
	st, b := builder.postMultipart("/api/v1/w/"+ws+"/imports", "multipart/form-data; boundary=zz", io.LimitReader(zeros{}, declared), declared)
	if st != 400 || !strings.Contains(strings.ToLower(b["error"].(map[string]any)["message"].(string)), "limit") {
		t.Fatalf("content-length cap %d %v", st, b)
	}
	// A streamed upload over 100 MiB (no usable Content-Length) is cut off.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		fw, _ := mw.CreateFormFile("file", "big.csv")
		_, _ = io.WriteString(fw, "a,b\n")
		chunk := bytes.Repeat([]byte("1,2\n"), 1<<18)
		for i := 0; i < 101; i++ {
			if _, err := fw.Write(chunk); err != nil {
				return
			}
		}
		_ = mw.Close()
		_ = pw.Close()
	}()
	st, b = builder.postMultipart("/api/v1/w/"+ws+"/imports", mw.FormDataContentType(), pr, -1)
	_ = pr.Close()
	if st != 400 {
		t.Fatalf("streamed over-limit upload %d %v", st, b)
	}
	// Wrong content type, no file part.
	if st, _ := builder.postMultipart("/api/v1/w/"+ws+"/imports", "application/json", strings.NewReader("{}"), -1); st != 400 {
		t.Fatalf("json body %d", st)
	}
	var nf bytes.Buffer
	w := multipart.NewWriter(&nf)
	_ = w.WriteField("format", "csv")
	_ = w.Close()
	if st, _ := builder.postMultipart("/api/v1/w/"+ws+"/imports", w.FormDataContentType(), &nf, int64(nf.Len())); st != 400 {
		t.Fatalf("no file %d", st)
	}
	// Options are honoured.
	st, m := builder.upload(ws, "x.txt", "a;b\n1;2\n", map[string]string{"format": "csv", "delimiter": ";", "has_header": "false"})
	if st != 201 || m["row_count"] != float64(2) {
		t.Fatalf("%d %v", st, m)
	}
	_ = strconv.Itoa
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

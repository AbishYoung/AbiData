package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

func testPNG(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

type rawResp struct {
	status int
	hdr    http.Header
	body   []byte
}

func (r rawResp) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.body, &m)
	return m
}

// send issues a request with a raw body. A session client carries its cookie,
// Origin and CSRF token (unless hdr overrides them); a bearer token sends only
// Authorization.
func (e *env) send(c *client, token, method, path string, body []byte, hdr ...string) rawResp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if c != nil {
		req.Header.Set("Origin", testOrigin)
		if c.csrf != "" {
			req.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			req.Header.Del(hdr[i])
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	hc := http.DefaultClient
	if c != nil && token == "" {
		hc = c.http
	}
	resp, err := hc.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return rawResp{resp.StatusCode, resp.Header, raw}
}

func multipartBody(field, filename string, data []byte) ([]byte, string) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	h.Set("Content-Type", "text/html") // a lie: ignored
	w, _ := mw.CreatePart(h)
	_, _ = w.Write(data)
	_ = mw.Close()
	return b.Bytes(), mw.FormDataContentType()
}

type filesEnv struct {
	*keyEnv
	editor, viewer *client
	base           string
}

func newFilesEnv(t *testing.T) *filesEnv {
	t.Helper()
	k := newKeyEnv(t)
	// roles() already made ed@example.com an editor of the workspace.
	k.user("vw@example.com", false)
	if st, b, _ := k.owner.do("POST", "/api/v1/w/"+k.ws+"/members", map[string]string{"email": "vw@example.com", "role": "viewer"}); st != 201 {
		t.Fatalf("member %d %v", st, b)
	}
	return &filesEnv{keyEnv: k, editor: k.login("ed@example.com"), viewer: k.login("vw@example.com"), base: "/api/v1/w/" + k.ws}
}

func (f *filesEnv) upload(c *client, name string, data []byte) map[string]any {
	f.t.Helper()
	r := f.send(c, "", "POST", f.base+"/files", data, "X-File-Name", name, "Content-Type", "application/octet-stream")
	if r.status != 201 {
		f.t.Fatalf("upload %s: %d %s", name, r.status, r.body)
	}
	return r.json()
}

func TestFileUploadRolesAndAuth(t *testing.T) {
	f := newFilesEnv(t)
	data := testPNG(30, 20)

	m := f.upload(f.editor, "Sighting 1.png", data)
	if m["name"] != "Sighting 1.png" || m["content_type"] != "image/png" || m["state"] != "pending" || m["width"] != float64(30) ||
		m["size"] != float64(len(data)) || m["has_thumbnail"] != true || len(m["sha256"].(string)) != 64 {
		t.Fatalf("meta %v", m)
	}

	// Viewers cannot upload; anonymous callers get 401.
	if r := f.send(f.viewer, "", "POST", f.base+"/files", data, "X-File-Name", "x.png"); r.status != 403 {
		t.Fatalf("viewer upload %d", r.status)
	}
	if r := f.send(nil, "", "POST", f.base+"/files", data, "X-File-Name", "x.png", "Origin", testOrigin); r.status != 401 {
		t.Fatalf("anonymous upload %d", r.status)
	}
	// Sessions need CSRF and a matching Origin on this unsafe request too.
	if r := f.send(f.editor, "", "POST", f.base+"/files", data, "X-File-Name", "x.png", "X-CSRF-Token", ""); r.status != 403 {
		t.Fatalf("no csrf %d", r.status)
	}
	if r := f.send(f.editor, "", "POST", f.base+"/files", data, "X-File-Name", "x.png", "Origin", "http://evil.test"); r.status != 403 {
		t.Fatalf("wrong origin %d", r.status)
	}
	// Multipart works, the client's part type is ignored, and the name comes from the part.
	body, ct := multipartBody("file", "page.html", []byte("<script>alert(1)</script>"))
	r := f.send(f.editor, "", "POST", f.base+"/files", body, "Content-Type", ct)
	if r.status != 201 || r.json()["content_type"] != "text/html" || r.json()["name"] != "page.html" {
		t.Fatalf("multipart %d %s", r.status, r.body)
	}
	body, ct = multipartBody("other", "x.txt", []byte("x"))
	if r := f.send(f.editor, "", "POST", f.base+"/files", body, "Content-Type", ct); r.status != 400 {
		t.Fatalf("multipart without file field %d", r.status)
	}
	// Empty and over-limit bodies.
	if r := f.send(f.editor, "", "POST", f.base+"/files", []byte{}, "X-File-Name", "e.txt"); r.status != 400 {
		t.Fatalf("empty %d", r.status)
	}
	f.s.deps.Files.MaxFileBytes = 1000
	big := bytes.Repeat([]byte("a"), 1001)
	if r := f.send(f.editor, "", "POST", f.base+"/files", big, "X-File-Name", "big.txt"); r.status != 400 || !strings.Contains(string(r.body), "larger") {
		t.Fatalf("over limit %d %s", r.status, r.body)
	}
	// A hostile name is sanitised.
	n := f.upload(f.editor, "..%2F..%2Fetc%2Fpasswd", []byte("root"))
	if n["name"] != "passwd" {
		t.Fatalf("name %v", n["name"])
	}
	// Percent-decoding of non-ASCII names; a literal % that is not an escape survives.
	if n := f.upload(f.editor, "caf%C3%A9.txt", []byte("x")); n["name"] != "café.txt" {
		t.Fatalf("name %v", n["name"])
	}
	if n := f.upload(f.editor, "100%.txt", []byte("y")); n["name"] != "100%.txt" {
		t.Fatalf("name %v", n["name"])
	}
}

func TestFileDownloadHeadersRangeAndThumbnail(t *testing.T) {
	f := newFilesEnv(t)
	img := f.upload(f.editor, "photo.png", testPNG(600, 300))
	id := img["id"].(string)
	content := f.base + "/files/" + id + "/content"

	// Any member, viewers included, can read.
	r := f.send(f.viewer, "", "GET", content, nil)
	if r.status != 200 {
		t.Fatalf("viewer download %d %s", r.status, r.body)
	}
	h := r.hdr
	if h.Get("Content-Type") != "image/png" || h.Get("Content-Disposition") != `inline; filename=photo.png` ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") ||
		h.Get("Cache-Control") != "private, max-age=31536000, immutable" || h.Get("ETag") == "" ||
		h.Get("Accept-Ranges") != "bytes" || h.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatalf("headers %v", h)
	}
	if !bytes.Equal(r.body, testPNG(600, 300)) {
		t.Fatal("bytes differ from the upload")
	}
	// Range.
	r = f.send(f.viewer, "", "GET", content, nil, "Range", "bytes=0-7")
	if r.status != 206 || string(r.body) != "\x89PNG\r\n\x1a\n" || r.hdr.Get("Content-Range") == "" {
		t.Fatalf("range %d %q %v", r.status, r.body, r.hdr)
	}
	// Conditional request.
	if r2 := f.send(f.viewer, "", "GET", content, nil, "If-None-Match", h.Get("ETag")); r2.status != 304 {
		t.Fatalf("conditional %d", r2.status)
	}
	// download=true forces an attachment.
	if r2 := f.send(f.viewer, "", "GET", content+"?download=true", nil); !strings.HasPrefix(r2.hdr.Get("Content-Disposition"), "attachment") {
		t.Fatalf("disposition %q", r2.hdr.Get("Content-Disposition"))
	}

	// Active content is only ever an attachment of an opaque type.
	for name, data := range map[string]string{
		"x.html": "<!DOCTYPE html><script>alert(1)</script>",
		"x.svg":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"x.png":  "<html><body onload=alert(1)>",
	} {
		m := f.upload(f.editor, name, []byte(data))
		rr := f.send(f.viewer, "", "GET", f.base+"/files/"+m["id"].(string)+"/content", nil)
		if rr.status != 200 || !strings.HasPrefix(rr.hdr.Get("Content-Disposition"), "attachment") ||
			rr.hdr.Get("Content-Type") != "application/octet-stream" || rr.hdr.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(rr.hdr.Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("%s served as %v", name, rr.hdr)
		}
	}
	pdf := f.upload(f.editor, "doc.pdf", []byte("%PDF-1.7\n1 0 obj\n"))
	if rr := f.send(f.viewer, "", "GET", f.base+"/files/"+pdf["id"].(string)+"/content", nil); !strings.HasPrefix(rr.hdr.Get("Content-Disposition"), "attachment") ||
		rr.hdr.Get("Content-Type") != "application/pdf" {
		t.Fatalf("pdf %v", rr.hdr)
	}
	// Non-ASCII names use RFC 5987 encoding in the header.
	u := f.upload(f.editor, "caf%C3%A9.pdf", []byte("%PDF-1.7\n"))
	if rr := f.send(f.viewer, "", "GET", f.base+"/files/"+u["id"].(string)+"/content", nil); !strings.Contains(rr.hdr.Get("Content-Disposition"), "filename*=utf-8''caf%C3%A9.pdf") {
		t.Fatalf("disposition %q", rr.hdr.Get("Content-Disposition"))
	}

	// Thumbnails.
	r = f.send(f.viewer, "", "GET", f.base+"/files/"+id+"/thumbnail", nil)
	if r.status != 200 || r.hdr.Get("Content-Type") != "image/jpeg" || r.hdr.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("thumbnail %d %v", r.status, r.hdr)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(r.body))
	if err != nil || cfg.Width != 320 || cfg.Height != 160 {
		t.Fatalf("thumbnail size %v %v", cfg, err)
	}
	if rr := f.send(f.viewer, "", "GET", f.base+"/files/"+pdf["id"].(string)+"/thumbnail", nil); rr.status != 404 {
		t.Fatalf("pdf thumbnail %d", rr.status)
	}

	// Metadata, unknown and malformed ids.
	if rr := f.send(f.viewer, "", "GET", f.base+"/files/"+id, nil); rr.status != 200 || rr.json()["id"] != id {
		t.Fatalf("metadata %d %s", rr.status, rr.body)
	}
	for _, bad := range []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "nope", "..%2F..%2Fetc%2Fpasswd"} {
		if rr := f.send(f.viewer, "", "GET", f.base+"/files/"+bad+"/content", nil); rr.status != 404 {
			t.Fatalf("%s: %d", bad, rr.status)
		}
	}
	// Batch lookup leaves unknown ids out.
	rr := f.send(f.viewer, "", "GET", f.base+"/files?ids="+id+",01ARZ3NDEKTSV4RRFFQ69G5FAV,"+pdf["id"].(string), nil)
	items := rr.json()["items"].([]any)
	if rr.status != 200 || len(items) != 2 {
		t.Fatalf("lookup %d %s", rr.status, rr.body)
	}
}

func TestFilesAreWorkspaceScoped(t *testing.T) {
	f := newFilesEnv(t)
	m := f.upload(f.editor, "a.txt", []byte("secret"))
	id := m["id"].(string)

	// The same id through another workspace's URL does not exist there, and a
	// member of that workspace cannot see this one at all.
	otherOwner := f.login("o2@example.com")
	for _, p := range []string{"/content", "/thumbnail", ""} {
		if r := f.send(otherOwner, "", "GET", "/api/v1/w/"+f.other+"/files/"+id+p, nil); r.status != 404 {
			t.Fatalf("other workspace %q: %d", p, r.status)
		}
		if r := f.send(otherOwner, "", "GET", f.base+"/files/"+id+p, nil); r.status != 404 {
			t.Fatalf("non-member %q: %d", p, r.status)
		}
	}
	if r := f.send(otherOwner, "", "POST", f.base+"/files", []byte("x"), "X-File-Name", "x"); r.status != 404 {
		t.Fatalf("non-member upload %d", r.status)
	}
}

func (e *env) root() *client { return e.login("root@example.com") }

func TestFilesWithAPIKeys(t *testing.T) {
	f := newFilesEnv(t)
	_, viewer := f.mint("v", "viewer", nil)
	_, editor := f.mint("e", "editor", nil)
	data := testPNG(10, 10)

	r := f.send(nil, editor, "POST", f.base+"/files", data, "X-File-Name", "bot.png")
	if r.status != 201 {
		t.Fatalf("editor key upload %d %s", r.status, r.body)
	}
	id := r.json()["id"].(string)
	// A viewer key reads but cannot upload; the matrix is the role's.
	if r := f.send(nil, viewer, "POST", f.base+"/files", data, "X-File-Name", "bot.png"); r.status != 403 {
		t.Fatalf("viewer key upload %d", r.status)
	}
	if r := f.send(nil, viewer, "GET", f.base+"/files/"+id+"/content", nil); r.status != 200 || !bytes.Equal(r.body, data) {
		t.Fatalf("viewer key download %d", r.status)
	}
	if r := f.send(nil, viewer, "GET", f.base+"/files/"+id+"/thumbnail", nil); r.status != 200 {
		t.Fatalf("viewer key thumbnail %d", r.status)
	}
	// A key only ever reaches its own workspace.
	for _, p := range []string{"/files/" + id + "/content", "/files/" + id} {
		if r := f.send(nil, editor, "GET", "/api/v1/w/"+f.other+p, nil); r.status != 404 {
			t.Fatalf("key on other workspace %s: %d", p, r.status)
		}
	}
	if r := f.send(nil, editor, "POST", "/api/v1/w/"+f.other+"/files", data, "X-File-Name", "x.png"); r.status != 404 {
		t.Fatalf("key upload to other workspace %d", r.status)
	}
	// Revoked or bogus keys get nothing.
	if r := f.send(nil, "abd_bogus_key", "GET", f.base+"/files/"+id+"/content", nil); r.status != 401 {
		t.Fatalf("bogus key %d", r.status)
	}
}

func TestAttachFilesToRowsThroughTheAPI(t *testing.T) {
	f := newFilesEnv(t)
	// A table with an attachments field (max 2 files).
	st, res, _ := f.builder.do("POST", f.base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Sightings", "columns": []map[string]any{
			{"display_name": "Species", "field_type": "short_text"},
			{"display_name": "Photos", "field_type": "attachments", "type_options": map[string]any{"max_files": 2}}}}}}})
	if st != 200 {
		t.Fatalf("schema %d %v", st, res)
	}
	var tbl map[string]any
	for _, x := range res["schema"].(map[string]any)["tables"].([]any) {
		if x.(map[string]any)["display_name"] == "Sightings" {
			tbl = x.(map[string]any)
		}
	}
	cols := tbl["columns"].([]any)
	photos := cols[2].(map[string]any)
	if photos["field_type"] != "attachments" || photos["type_options"].(map[string]any)["max_files"] != float64(2) {
		t.Fatalf("column %v", photos)
	}
	rows := f.base + "/tables/" + tbl["id"].(string) + "/rows"
	pid := photos["id"].(string)

	a := f.upload(f.editor, "a.png", testPNG(8, 8))["id"].(string)
	b := f.upload(f.editor, "b.png", testPNG(9, 9))["id"].(string)
	c := f.upload(f.editor, "c.png", testPNG(10, 10))["id"].(string)

	st, row, _ := f.editor.do("POST", rows, map[string]any{"fields": map[string]any{pid: []string{a, b, a}}})
	if st != 201 {
		t.Fatalf("insert %d %v", st, row)
	}
	got := row["fields"].(map[string]any)[pid].([]any)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("stored %v", got)
	}
	// Attaching marks the file attached.
	if r := f.send(f.viewer, "", "GET", f.base+"/files/"+a, nil); r.json()["state"] != "attached" {
		t.Fatalf("state %s", r.body)
	}
	if r := f.send(f.viewer, "", "GET", f.base+"/files/"+c, nil); r.json()["state"] != "pending" {
		t.Fatalf("state %s", r.body)
	}
	// Too many, malformed, unknown.
	for name, v := range map[string]any{"too many": []string{a, b, c}, "malformed": []string{"nope"}, "string": a,
		"unknown": []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}} {
		if st, body, _ := f.editor.do("POST", rows, map[string]any{"fields": map[string]any{pid: v}}); st != 400 {
			t.Fatalf("%s: %d %v", name, st, body)
		}
	}
	// A file of the other workspace is refused with the same error as an unknown id.
	foreign := f.otherFile()
	if st, body, _ := f.editor.do("POST", rows, map[string]any{"fields": map[string]any{pid: []string{foreign}}}); st != 400 {
		t.Fatalf("foreign: %d %v", st, body)
	}
	// Update replaces the list; null clears it.
	id := fmt.Sprint(row["id"])
	st, upd, _ := f.editor.do("PATCH", rows+"/"+id, map[string]any{"fields": map[string]any{pid: []string{c}}}, func(r *http.Request) { r.Header.Set("If-Match", `"1"`) })
	if st != 200 || fmt.Sprint(upd["fields"].(map[string]any)[pid]) != fmt.Sprint([]any{c}) {
		t.Fatalf("update %d %v", st, upd)
	}
	st, upd, _ = f.editor.do("PATCH", rows+"/"+id, map[string]any{"fields": map[string]any{pid: nil}}, func(r *http.Request) { r.Header.Set("If-Match", `"2"`) })
	if st != 200 || upd["fields"].(map[string]any)[pid] != nil {
		t.Fatalf("clear %d %v", st, upd)
	}
	// Export of a table with an attachments column does not fail (raw ids for now).
	st, body, _ := f.owner.do("POST", f.base+"/exports", map[string]any{"source_kind": "table", "source_id": tbl["id"], "format": "json"})
	if st != 202 {
		t.Fatalf("export %d %v", st, body)
	}
}

// otherFile uploads a file into the other workspace and returns its id.
func (f *filesEnv) otherFile() string {
	_, tok := f.mintIn(f.other, "editor")
	r := f.send(nil, tok, "POST", "/api/v1/w/"+f.other+"/files", []byte("foreign file"), "X-File-Name", "f.txt")
	if r.status != 201 {
		f.t.Fatalf("other upload %d %s", r.status, r.body)
	}
	return r.json()["id"].(string)
}

func (k *keyEnv) mintIn(ws, role string) (string, string) {
	k.t.Helper()
	o2 := k.login("o2@example.com")
	st, out, _ := o2.do("POST", "/api/v1/w/"+ws+"/api-keys", map[string]any{"name": "k", "role": role})
	if st != 201 {
		k.t.Fatalf("create key: %d %v", st, out)
	}
	return out["id"].(string), out["token"].(string)
}

func TestFileQuota(t *testing.T) {
	f := newFilesEnv(t)
	root := f.root()
	if st, b, _ := root.do("PATCH", "/api/v1/admin/workspaces/"+f.ws, map[string]any{"quota": map[string]any{"max_file_bytes": 100}}); st != 200 {
		t.Fatalf("quota %d %v", st, b)
	}
	if r := f.send(f.editor, "", "POST", f.base+"/files", bytes.Repeat([]byte("a"), 60), "X-File-Name", "a.txt"); r.status != 201 {
		t.Fatalf("within quota %d %s", r.status, r.body)
	}
	r := f.send(f.editor, "", "POST", f.base+"/files", bytes.Repeat([]byte("b"), 60), "X-File-Name", "b.txt")
	if r.status != 507 || errCode(r.json()) != "QUOTA_EXCEEDED" {
		t.Fatalf("over quota %d %s", r.status, r.body)
	}
	// The same bytes again cost nothing.
	if r := f.send(f.editor, "", "POST", f.base+"/files", bytes.Repeat([]byte("a"), 60), "X-File-Name", "again.txt"); r.status != 201 {
		t.Fatalf("known bytes %d %s", r.status, r.body)
	}
}

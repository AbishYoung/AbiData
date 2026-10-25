package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// rawPost posts JSON and returns the status and the raw body (the NDJSON streams cannot go through client.do).
func (c *client) rawPost(path string, body any) (int, string) {
	c.e.t.Helper()
	enc, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", c.e.srv.URL+path, strings.NewReader(string(enc)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-CSRF-Token", c.csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestReportDraftPreview(t *testing.T) {
	e := newEnv(t)
	ws, _, _, b, ed, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	_, res, _ := b.do("POST", base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Notes", "columns": []map[string]any{{"display_name": "Text", "field_type": "short_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	f := appFixture{ws: ws, table: tbl["id"].(string), text: tbl["columns"].([]any)[1].(map[string]any)["id"].(string)}
	if st, body, _ := b.do("POST", base+"/tables/"+f.table+"/rows", map[string]any{"fields": map[string]any{f.text: "draft row"}}); st != 201 {
		t.Fatalf("row %d %v", st, body)
	}
	draft := reportBody(f)
	path := base + "/reports/preview"

	// A builder previews an unsaved draft; nothing is stored.
	st, raw := b.rawPost(path, map[string]any{"report": draft})
	if st != 200 || !strings.Contains(raw, "draft row") {
		t.Fatalf("preview %d %s", st, raw)
	}
	if _, list, _ := b.do("GET", base+"/reports", nil); len(list["items"].([]any)) != 0 {
		t.Fatalf("preview stored a report: %v", list)
	}

	// The draft is validated like a save.
	bad := reportBody(f)
	bad["layout"].(map[string]any)["bands"].(map[string]any)["detail"].(map[string]any)["controls"].([]any)[0].(map[string]any)["bind"] = map[string]any{"column_id": "nope"}
	if st, raw := b.rawPost(path, map[string]any{"report": bad}); st != 400 {
		t.Fatalf("invalid draft %d %s", st, raw)
	}
	// Only builders: editors may run saved reports but not arbitrary drafts.
	if st, _ := ed.rawPost(path, map[string]any{"report": draft}); st != 403 {
		t.Fatalf("editor preview %d", st)
	}
}

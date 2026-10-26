package httpapi

import (
	"strings"
	"testing"
)

// TestPublicAppBodiesCapped: the unauthenticated app routes accept bodies of
// at most 256 KB (ADR 0014), not the 1 MiB default of signed-in routes.
func TestPublicAppBodiesCapped(t *testing.T) {
	e := newEnv(t)
	ws, _, owner, b, _, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	f := appFixture{ws: ws}
	_, res, _ := b.do("POST", base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Notes", "columns": []map[string]any{{"display_name": "Text", "field_type": "long_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	f.table = tbl["id"].(string)
	f.text = tbl["columns"].([]any)[1].(map[string]any)["id"].(string)
	st, form, _ := b.do("POST", base+"/forms", formBody(f))
	if st != 201 {
		t.Fatalf("form %d %v", st, form)
	}
	st, rep, _ := b.do("POST", base+"/reports", reportBody(f))
	if st != 201 {
		t.Fatalf("report %d %v", st, rep)
	}
	st, app, _ := owner.do("POST", base+"/apps", map[string]any{"name": "Caps", "items": []map[string]any{
		{"kind": "form", "object_id": form["id"], "title": "Form"}, {"kind": "report", "object_id": rep["id"], "title": "Report"}}})
	if st != 201 {
		t.Fatalf("app %d %v", st, app)
	}
	link := app["link"].(string)
	pub := "/api/v1/apps/" + link[strings.LastIndex(link, "/")+1:]
	items := app["items"].([]any)
	formItem, reportItem := items[0].(map[string]any)["id"].(string), items[1].(map[string]any)["id"].(string)
	v := visitor(e)

	big := strings.Repeat("x", 300<<10)
	for name, req := range map[string]struct {
		path string
		body map[string]any
	}{
		"submit": {pub + "/forms/" + formItem + "/submit", map[string]any{"fields": map[string]any{f.text: big}}},
		"event":  {pub + "/forms/" + formItem + "/events", map[string]any{"event": "change", "new_value": big}},
		"run":    {pub + "/reports/" + reportItem + "/run", map[string]any{"params": map[string]any{"p": big}}},
	} {
		// Oversize bodies use the BAD_REQUEST envelope, as uploads do.
		if st, body, _ := v.post(req.path, req.body); st != 400 || !strings.Contains(body["error"].(map[string]any)["message"].(string), "too large") {
			t.Errorf("%s with a 300 KB body: %d %v", name, st, body)
		}
	}
	// Under the cap a body is read as before.
	if st, body, _ := v.post(pub+"/forms/"+formItem+"/submit", map[string]any{"fields": map[string]any{f.text: strings.Repeat("y", 200<<10)}}); st != 200 {
		t.Fatalf("submit with a 200 KB body: %d %v", st, body)
	}
}

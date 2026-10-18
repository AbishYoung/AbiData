package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// visitor is someone holding an app link: no cookies, no Authorization, only
// the Origin a browser adds on the app's own page.
func visitor(e *env) bearer { return bearer{e: e} }

func (b bearer) post(path string, body any, hdr ...string) (int, map[string]any, http.Header) {
	return b.do("POST", path, body, append([]string{"Origin", testOrigin}, hdr...)...)
}

type appFixture struct {
	ws, table, text, secret, form, report string
}

func control(id, typ, col string, y int) map[string]any {
	c := map[string]any{"id": id, "name": id, "type": typ, "x": 0, "y": y, "w": 200, "h": 24, "tab_index": y}
	if col != "" {
		c["bind"] = map[string]any{"column_id": col}
	}
	return c
}

func formBody(f appFixture, extra ...map[string]any) map[string]any {
	ctls := append([]any{control("Text", "textbox", f.text, 0)}, toAny(extra)...)
	section := func(h int, c []any) map[string]any { return map[string]any{"height": h, "controls": c} }
	return map[string]any{"name": "Sign up", "source_kind": "table", "source_id": f.table, "allow_additions": true, "allow_edits": true,
		"layout":        map[string]any{"width": 400, "sections": map[string]any{"header": section(0, []any{}), "detail": section(100, ctls), "footer": section(0, []any{})}},
		"module_source": ""}
}

func toAny(ms []map[string]any) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}

func reportBody(f appFixture) map[string]any {
	band := func(h int, c []any) map[string]any { return map[string]any{"height": h, "controls": c} }
	return map[string]any{"name": "Notes list", "source_kind": "table", "source_id": f.table,
		"page_setup": map[string]any{"paper": "letter", "orientation": "portrait", "margin_top": 48, "margin_bottom": 48, "margin_left": 48, "margin_right": 48},
		"layout": map[string]any{"width": 700, "bands": map[string]any{
			"report_header": band(0, []any{}), "page_header": band(0, []any{}), "page_footer": band(0, []any{}), "report_footer": band(0, []any{}),
			"detail": band(20, []any{func() map[string]any { c := control("txt", "textbox", f.text, 0); delete(c, "tab_index"); return c }()}),
		}}}
}

func TestPublicApps(t *testing.T) {
	e := newEnv(t)
	ws, root, owner, b, ed, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	f := appFixture{ws: ws}

	_, res, _ := b.do("POST", base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Notes", "track_history": true, "columns": []map[string]any{
			{"display_name": "Text", "field_type": "short_text"}, {"display_name": "Secret", "field_type": "short_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	f.table = tbl["id"].(string)
	cols := tbl["columns"].([]any)
	f.text, f.secret = cols[1].(map[string]any)["id"].(string), cols[2].(map[string]any)["id"].(string)
	if st, body, _ := b.do("POST", base+"/tables/"+f.table+"/rows", map[string]any{"fields": map[string]any{f.text: "public row", f.secret: "hidden value"}}); st != 201 {
		t.Fatalf("row %d %v", st, body)
	}
	st, form, _ := b.do("POST", base+"/forms", formBody(f))
	if st != 201 {
		t.Fatalf("form %d %v", st, form)
	}
	f.form = form["id"].(string)
	st, rep, _ := b.do("POST", base+"/reports", reportBody(f))
	if st != 201 {
		t.Fatalf("report %d %v", st, rep)
	}
	f.report = rep["id"].(string)
	items := []map[string]any{{"kind": "form", "object_id": f.form, "title": "Sign up"}, {"kind": "report", "object_id": f.report, "title": "Notes"}}

	// Only owners publish; builders and editors cannot even list.
	for _, c := range []*client{b, ed} {
		if st, _, _ := c.do("POST", base+"/apps", map[string]any{"name": "X", "items": items}); st != 403 {
			t.Fatalf("non-owner create: %d", st)
		}
		if st, _, _ := c.do("GET", base+"/apps", nil); st != 403 {
			t.Fatalf("non-owner list: %d", st)
		}
	}
	// A form that can do more than submit cannot be published.
	st, bad, _ := b.do("POST", base+"/forms", func() map[string]any {
		x := formBody(f, control("Go", "button", "", 40))
		x["name"] = "With button"
		return x
	}())
	if st != 201 {
		t.Fatalf("button form %d %v", st, bad)
	}
	if st, body, _ := owner.do("POST", base+"/apps", map[string]any{"name": "X", "items": []map[string]any{{"kind": "form", "object_id": bad["id"], "title": "B"}}}); st != 400 || !strings.Contains(body["error"].(map[string]any)["message"].(string), "button") {
		t.Fatalf("button form published: %d %v", st, body)
	}

	st, app, _ := owner.do("POST", base+"/apps", map[string]any{"name": "Volunteers", "items": items})
	if st != 201 {
		t.Fatalf("create %d %v", st, app)
	}
	appID := app["id"].(string)
	link := app["link"].(string)
	tok := link[strings.LastIndex(link, "/")+1:]
	pub := "/api/v1/apps/" + tok
	v := visitor(e)
	appItems := app["items"].([]any)
	formItem, reportItem := appItems[0].(map[string]any)["id"].(string), appItems[1].(map[string]any)["id"].(string)
	if appItems[0].(map[string]any)["status"] != "live" || app["start_item"] != formItem {
		t.Fatalf("app %v", app)
	}

	// The menu, with headers that keep the link out of referrers and search engines.
	st, menu, hdr := v.do("GET", pub, nil)
	if st != 200 || menu["name"] != "Volunteers" || len(menu["items"].([]any)) != 2 {
		t.Fatalf("menu %d %v", st, menu)
	}
	if hdr.Get("Referrer-Policy") != "no-referrer" || hdr.Get("X-Robots-Tag") == "" || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers %v", hdr)
	}
	if _, has := menu["workspace_id"]; has {
		t.Fatal("menu reveals the workspace")
	}

	// The form shows only the fields it binds.
	st, pf, _ := v.do("GET", pub+"/forms/"+formItem, nil)
	if st != 200 {
		t.Fatalf("form %d %v", st, pf)
	}
	pcols := pf["columns"].([]any)
	if len(pcols) != 1 || pcols[0].(map[string]any)["id"] != f.text {
		t.Fatalf("columns %v", pcols)
	}
	if _, has := pf["module_source"]; has {
		t.Fatal("module source leaked")
	}
	// A report item is not a form, and unknown items are not found.
	if st, _, _ := v.do("GET", pub+"/forms/"+reportItem, nil); st != 404 {
		t.Fatalf("report as form: %d", st)
	}

	// Submitting: only the form's fields, only with the page's Origin.
	if st, body, _ := v.post(pub+"/forms/"+formItem+"/submit", map[string]any{"fields": map[string]any{f.text: "hi", f.secret: "x"}}); st != 400 {
		t.Fatalf("hidden field accepted: %d %v", st, body)
	}
	if st, _, _ := v.do("POST", pub+"/forms/"+formItem+"/submit", map[string]any{"fields": map[string]any{f.text: "hi"}}); st != 403 {
		t.Fatalf("no origin: %d", st)
	}
	st, sub, _ := v.post(pub+"/forms/"+formItem+"/submit", map[string]any{"fields": map[string]any{f.text: "from a visitor"}}, "Idempotency-Key", "k1")
	if st != 200 || sub["submitted"] != true {
		t.Fatalf("submit %d %v", st, sub)
	}
	if _, has := sub["saved"]; has {
		t.Fatal("the stored row came back")
	}
	_, rows, _ := b.do("GET", base+"/tables/"+f.table+"/rows", nil)
	var newID float64
	for _, r := range rows["items"].([]any) {
		if r.(map[string]any)["fields"].(map[string]any)[f.text] == "from a visitor" {
			newID = r.(map[string]any)["id"].(float64)
		}
	}
	if newID == 0 {
		t.Fatalf("submission not stored: %v", rows)
	}
	_, hist, _ := b.do("GET", base+"/tables/"+f.table+"/rows/"+strconv.FormatInt(int64(newID), 10)+"/history", nil)
	if items := hist["items"].([]any); len(items) == 0 || items[0].(map[string]any)["user_name"] != `App "Volunteers"` {
		t.Fatalf("history %v", hist)
	}
	// Events always describe a new record, and only the visitor-safe events run.
	if st, _, _ := v.post(pub+"/forms/"+formItem+"/events", map[string]any{"event": "load", "record_id": 1}); st != 200 {
		t.Fatalf("load event %d", st)
	}
	if st, _, _ := v.post(pub+"/forms/"+formItem+"/events", map[string]any{"event": "click", "control": "Text"}); st != 400 {
		t.Fatalf("click event %d", st)
	}

	// The report shows what is on its page and nothing else.
	req, _ := http.NewRequest("POST", e.srv.URL+pub+"/reports/"+reportItem+"/run", strings.NewReader(`{}`))
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(raw), "public row") || strings.Contains(string(raw), "hidden value") {
		t.Fatalf("run %d %s", resp.StatusCode, raw)
	}

	// A builder's edit takes the item off the air until an owner approves it.
	_, cur, _ := b.do("GET", base+"/forms/"+f.form, nil)
	upd := formBody(f)
	upd["name"] = "Sign up now"
	if st, body, _ := b.do("PUT", base+"/forms/"+f.form, upd, func(r *http.Request) {
		r.Header.Set("If-Match", `"`+strconv.FormatInt(int64(cur["version"].(float64)), 10)+`"`)
	}); st != 200 {
		t.Fatalf("edit %d %v", st, body)
	}
	if st, body, _ := v.do("GET", pub+"/forms/"+formItem, nil); st != 503 || errCode(body) != "APP_ITEM_CHANGED" {
		t.Fatalf("changed form served: %d %v", st, body)
	}
	_, list, _ := owner.do("GET", base+"/apps", nil)
	if s := list["items"].([]any)[0].(map[string]any)["items"].([]any)[0].(map[string]any)["status"]; s != "changed" {
		t.Fatalf("owner status %v", s)
	}
	// Renaming the app does not approve anything on the side.
	if st, _, _ := owner.do("PUT", base+"/apps/"+appID, map[string]any{"name": "Helpers", "items": []map[string]any{
		{"id": formItem, "kind": "form", "object_id": f.form, "title": "Join"}, {"id": reportItem, "kind": "report", "object_id": f.report, "title": "Notes"}}}); st != 200 {
		t.Fatalf("rename %d", st)
	}
	if st, _, _ := v.do("GET", pub+"/forms/"+formItem, nil); st != 503 {
		t.Fatalf("rename approved the change: %d", st)
	}
	if st, _, _ := owner.do("POST", base+"/apps/"+appID+"/approve", map[string]any{}); st != 200 {
		t.Fatalf("approve %d", st)
	}
	if st, _, _ := v.do("GET", pub+"/forms/"+formItem, nil); st != 200 {
		t.Fatalf("approved form %d", st)
	}

	// Off, rotated, taken down, or off server-wide: the link is simply unknown.
	expect := func(want int, why string) {
		t.Helper()
		if st, body, _ := v.do("GET", pub, nil); st != want {
			t.Fatalf("%s: %d %v", why, st, body)
		}
	}
	set := func(enabled bool) {
		if st, _, _ := owner.do("PUT", base+"/apps/"+appID, map[string]any{"name": "Helpers", "enabled": enabled, "items": []map[string]any{
			{"id": formItem, "kind": "form", "object_id": f.form, "title": "Join"}, {"id": reportItem, "kind": "report", "object_id": f.report, "title": "Notes"}}}); st != 200 {
			t.Fatalf("set enabled %v: %d", enabled, st)
		}
	}
	set(false)
	expect(404, "disabled")
	set(true)
	expect(200, "enabled")

	if st, _, _ := root.do("POST", "/api/v1/admin/apps/"+appID+"/take-down", nil); st != 204 {
		t.Fatalf("take down %d", st)
	}
	expect(404, "taken down")
	set(true) // the owner cannot undo a take-down
	expect(404, "owner re-enabled after take-down")
	if st, _, _ := root.do("POST", "/api/v1/admin/apps/"+appID+"/restore", nil); st != 204 {
		t.Fatalf("restore %d", st)
	}
	expect(200, "restored")

	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/public-apps", map[string]any{"enabled": false}); st != 200 {
		t.Fatalf("switch off %d", st)
	}
	expect(404, "switched off")
	if st, _, _ := owner.do("POST", base+"/apps", map[string]any{"name": "Y", "items": items}); st != 403 {
		t.Fatalf("create while off: %d", st)
	}
	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/public-apps", map[string]any{"enabled": true}); st != 200 {
		t.Fatalf("switch on %d", st)
	}
	expect(200, "switched on")

	st, rot, _ := owner.do("POST", base+"/apps/"+appID+"/rotate", nil)
	if st != 200 || rot["link"] == link {
		t.Fatalf("rotate %d %v", st, rot)
	}
	expect(404, "old link after rotation")
	newTok := rot["link"].(string)[strings.LastIndex(rot["link"].(string), "/")+1:]
	if st, _, _ := v.do("GET", "/api/v1/apps/"+newTok, nil); st != 200 {
		t.Fatalf("new link %d", st)
	}
	pub = "/api/v1/apps/" + newTok

	// Malformed links never reach the database, and every miss looks the same.
	for _, p := range []string{"/api/v1/apps/short", "/api/v1/apps/" + strings.Repeat("Z", 43)} {
		if st, body, _ := v.do("GET", p, nil); st != 404 || errCode(body) != "NOT_FOUND" {
			t.Fatalf("%s: %d %v", p, st, body)
		}
	}
	// Admins see every app, without links.
	_, all, _ := root.do("GET", "/api/v1/admin/apps", nil)
	if a := all["items"].([]any)[0].(map[string]any); a["link"] != nil || a["workspace_name"] != "Jobs" {
		t.Fatalf("admin list %v", a)
	}

	// Submissions are throttled per address.
	limited := false
	for i := range 15 {
		st, _, h := v.post(pub+"/forms/"+formItem+"/submit", map[string]any{"fields": map[string]any{f.text: "spam " + strconv.Itoa(i)}})
		if st == 429 {
			limited = h.Get("Retry-After") != ""
			break
		}
	}
	if !limited {
		t.Fatal("submissions were not throttled")
	}

	// Deleting the app ends the link.
	if st, _, _ := owner.do("DELETE", base+"/apps/"+appID, nil); st != 204 {
		t.Fatalf("delete %d", st)
	}
	if st, _, _ := v.do("GET", pub, nil); st != 404 {
		t.Fatalf("deleted app %d", st)
	}
}

func TestPublicAppsForcedOffAndPageHeaders(t *testing.T) {
	e := newEnv(t)
	_, root, _, _, _, _ := roles(t, e)
	// The app's page itself keeps its link out of referrers and search engines.
	resp, err := http.Get(e.srv.URL + "/a/" + strings.Repeat("A", 43))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Referrer-Policy") != "no-referrer" || resp.Header.Get("X-Robots-Tag") == "" {
		t.Fatalf("page %d %v", resp.StatusCode, resp.Header)
	}
	// ABIDATA_PUBLIC_APPS=false wins over the administrator setting.
	e.s.deps.Config.PublicApps = false
	st, body, _ := root.do("GET", "/api/v1/admin/settings/public-apps", nil)
	if st != 200 || body["enabled"] != false || body["forced_off"] != true {
		t.Fatalf("setting %d %v", st, body)
	}
	if st, _, _ := root.do("PUT", "/api/v1/admin/settings/public-apps", map[string]any{"enabled": true}); st != 400 {
		t.Fatalf("turned on against the environment: %d", st)
	}
}

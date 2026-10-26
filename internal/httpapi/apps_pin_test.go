package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestPublicAppsPinIndirectDependencies: an owner's approval covers what a
// published item runs indirectly too. A shared Lua module the form module
// requires, and a saved query used as a source by the query behind a report,
// are code and design a builder can change; until an owner approves the
// change, visitors get APP_ITEM_CHANGED instead of the new behaviour.
func TestPublicAppsPinIndirectDependencies(t *testing.T) {
	e := newEnv(t)
	ws, _, owner, b, _, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	f := appFixture{ws: ws}

	_, res, _ := b.do("POST", base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Notes", "columns": []map[string]any{{"display_name": "Text", "field_type": "short_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	f.table = tbl["id"].(string)
	f.text = tbl["columns"].([]any)[1].(map[string]any)["id"].(string)

	ifMatch := func(v any) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("If-Match", `"`+strconv.FormatInt(int64(v.(float64)), 10)+`"`) }
	}

	// A shared module and a form whose module requires it.
	st, util, _ := b.do("POST", base+"/scripts", map[string]any{"name": "util", "kind": "module", "source": "local M = {}\nfunction M.greet() return 'v1' end\nreturn M\n"})
	if st != 201 {
		t.Fatalf("module %d %v", st, util)
	}
	fb := formBody(f)
	fb["module_source"] = "local util = require(\"util\")\nfunction on_load() ui.notify(util.greet()) end\n"
	fb["layout"].(map[string]any)["events"] = map[string]any{"load": "on_load"}
	st, form, _ := b.do("POST", base+"/forms", fb)
	if st != 201 {
		t.Fatalf("form %d %v", st, form)
	}

	// A report on a visual query whose source is another saved query.
	innerModel := func(fields ...map[string]any) map[string]any {
		return map[string]any{"sources": []any{map[string]any{"alias": "n", "table_id": f.table, "x": 0, "y": 0}}, "fields": toAny(fields)}
	}
	tField := map[string]any{"alias": "n", "column_id": f.text, "output_name": "t"}
	st, inner, _ := b.do("POST", base+"/queries", map[string]any{"name": "Inner", "kind": "visual", "visual_model": innerModel(tField)})
	if st != 201 {
		t.Fatalf("inner query %d %v", st, inner)
	}
	st, outer, _ := b.do("POST", base+"/queries", map[string]any{"name": "Outer", "kind": "visual", "visual_model": map[string]any{
		"sources": []any{map[string]any{"alias": "i", "query_id": inner["id"], "x": 0, "y": 0}},
		"fields":  []any{map[string]any{"alias": "i", "column_id": "t", "output_name": "t"}},
	}})
	if st != 201 {
		t.Fatalf("outer query %d %v", st, outer)
	}
	rb := reportBody(f)
	rb["source_kind"], rb["source_id"] = "query", outer["id"]
	detail := rb["layout"].(map[string]any)["bands"].(map[string]any)["detail"].(map[string]any)
	detail["controls"].([]any)[0].(map[string]any)["bind"] = map[string]any{"column_id": "t"}
	st, rep, _ := b.do("POST", base+"/reports", rb)
	if st != 201 {
		t.Fatalf("report %d %v", st, rep)
	}

	st, app, _ := owner.do("POST", base+"/apps", map[string]any{"name": "Pins", "items": []map[string]any{
		{"kind": "form", "object_id": form["id"], "title": "Form"}, {"kind": "report", "object_id": rep["id"], "title": "Report"}}})
	if st != 201 {
		t.Fatalf("create app %d %v", st, app)
	}
	link := app["link"].(string)
	pub := "/api/v1/apps/" + link[strings.LastIndex(link, "/")+1:]
	appItems := app["items"].([]any)
	formItem, reportItem := appItems[0].(map[string]any)["id"].(string), appItems[1].(map[string]any)["id"].(string)
	v := visitor(e)
	for _, p := range []string{pub + "/forms/" + formItem, pub + "/reports/" + reportItem} {
		if st, body, _ := v.do("GET", p, nil); st != 200 {
			t.Fatalf("%s before edits: %d %v", p, st, body)
		}
	}

	// A builder edits the required module: the form waits for approval.
	if st, body, _ := b.do("PUT", base+"/scripts/"+util["id"].(string), map[string]any{"name": "util", "kind": "module",
		"source": "local M = {}\nfunction M.greet() return 'v2' end\nreturn M\n"}, ifMatch(util["version"])); st != 200 {
		t.Fatalf("edit module %d %v", st, body)
	}
	if st, body, _ := v.do("GET", pub+"/forms/"+formItem, nil); st != 503 || errCode(body) != "APP_ITEM_CHANGED" {
		t.Fatalf("form after its required module changed: %d %v", st, body)
	}

	// A builder edits the nested query: the report waits for approval.
	if st, body, _ := b.do("PUT", base+"/queries/"+inner["id"].(string), map[string]any{"name": "Inner", "kind": "visual",
		"visual_model": innerModel(tField, map[string]any{"alias": "n", "column_id": "id", "output_name": "row"})}, ifMatch(inner["version"])); st != 200 {
		t.Fatalf("edit inner query %d %v", st, body)
	}
	if st, body, _ := v.do("GET", pub+"/reports/"+reportItem, nil); st != 503 || errCode(body) != "APP_ITEM_CHANGED" {
		t.Fatalf("report after its nested query changed: %d %v", st, body)
	}

	// The owner sees both waiting, and approving puts them back.
	_, list, _ := owner.do("GET", base+"/apps", nil)
	for _, it := range list["items"].([]any)[0].(map[string]any)["items"].([]any) {
		if s := it.(map[string]any)["status"]; s != "changed" {
			t.Fatalf("owner status %v", it)
		}
	}
	if st, _, _ := owner.do("POST", base+"/apps/"+app["id"].(string)+"/approve", map[string]any{}); st != 200 {
		t.Fatalf("approve %d", st)
	}
	for _, p := range []string{pub + "/forms/" + formItem, pub + "/reports/" + reportItem} {
		if st, body, _ := v.do("GET", p, nil); st != 200 {
			t.Fatalf("%s after approval: %d %v", p, st, body)
		}
	}

	// A form whose module does not require anything is not affected by edits
	// to shared modules.
	plain := formBody(f)
	plain["name"] = "Plain"
	plain["module_source"] = "function on_load() end\n"
	plain["layout"].(map[string]any)["events"] = map[string]any{"load": "on_load"}
	st, pform, _ := b.do("POST", base+"/forms", plain)
	if st != 201 {
		t.Fatalf("plain form %d %v", st, pform)
	}
	st, app2, _ := owner.do("POST", base+"/apps", map[string]any{"name": "Plain", "items": []map[string]any{{"kind": "form", "object_id": pform["id"], "title": "Plain"}}})
	if st != 201 {
		t.Fatalf("plain app %d %v", st, app2)
	}
	_, cur, _ := b.do("GET", base+"/scripts/"+util["id"].(string), nil)
	if st, body, _ := b.do("PUT", base+"/scripts/"+util["id"].(string), map[string]any{"name": "util", "kind": "module",
		"source": "return {}\n"}, ifMatch(cur["version"])); st != 200 {
		t.Fatalf("edit module again %d %v", st, body)
	}
	link2 := app2["link"].(string)
	pub2 := "/api/v1/apps/" + link2[strings.LastIndex(link2, "/")+1:]
	plainItem := app2["items"].([]any)[0].(map[string]any)["id"].(string)
	if st, body, _ := v.do("GET", pub2+"/forms/"+plainItem, nil); st != 200 {
		t.Fatalf("plain form after a module edit: %d %v", st, body)
	}
}

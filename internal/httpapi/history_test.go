package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

func TestRowHistoryRoute(t *testing.T) {
	e := newEnv(t)
	ws, _, owner, b, ed, _ := roles(t, e)
	base := "/api/v1/w/" + ws
	_, res, _ := b.do("POST", base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Notes", "track_history": true, "columns": []map[string]any{{"display_name": "Text", "field_type": "short_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	tid := tbl["id"].(string)
	col := tbl["columns"].([]any)[1].(map[string]any)["id"].(string)
	rows := base + "/tables/" + tid + "/rows"

	st, row, _ := ed.do("POST", rows, map[string]any{"fields": map[string]any{col: "v1"}})
	if st != 201 {
		t.Fatalf("insert %d %v", st, row)
	}
	id := fmt.Sprint(row["id"])
	ifMatch := func(v any) opt { return func(r *http.Request) { r.Header.Set("If-Match", fmt.Sprintf(`"%v"`, v)) } }
	ver := row["_version"]
	for i := 2; i <= 4; i++ {
		st, row, _ = ed.do("PATCH", rows+"/"+id, map[string]any{"fields": map[string]any{col: fmt.Sprintf("v%d", i)}}, ifMatch(ver))
		if st != 200 {
			t.Fatalf("update %d %v", st, row)
		}
		ver = row["_version"]
	}
	if st, _, _ := ed.do("DELETE", rows+"/"+id, nil, ifMatch(ver)); st != 204 {
		t.Fatalf("delete %d", st)
	}

	// Page 1 (newest first), then page 2 via before.
	st, h, _ := owner.do("GET", rows+"/"+id+"/history?limit=3", nil)
	if st != 200 || h["tracked"] != true || h["more"] != true {
		t.Fatalf("history %d %v", st, h)
	}
	items := h["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items %v", items)
	}
	first := items[0].(map[string]any)
	if first["op"] != "delete" || first["user_name"] == "" || first["user_id"] == "" || first["after"] != nil {
		t.Fatalf("newest %v", first)
	}
	if before := first["before"].(map[string]any); before[col] != "v4" {
		t.Fatalf("before %v", before)
	}
	last := items[2].(map[string]any)
	st, h, _ = owner.do("GET", fmt.Sprintf("%s/%s/history?limit=3&before=%v", rows, id, last["id"]), nil)
	items = h["items"].([]any)
	if st != 200 || h["more"] != false || len(items) != 2 || items[1].(map[string]any)["op"] != "insert" {
		t.Fatalf("page 2 %d %v", st, h)
	}
	if after := items[1].(map[string]any)["after"].(map[string]any); after[col] != "v1" {
		t.Fatalf("insert after %v", after)
	}

	// Another workspace's member cannot read it.
	e.user("x@example.com", false)
	e.login("root@example.com").do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Other", "owner_email": "x@example.com"})
	if st, _, _ := e.login("x@example.com").do("GET", rows+"/"+id+"/history", nil); st != 404 && st != 403 {
		t.Fatalf("outsider %d", st)
	}
	// Unknown table → 404.
	if st, _, _ := owner.do("GET", base+"/tables/nope/rows/1/history", nil); st != 404 {
		t.Fatalf("unknown table %d", st)
	}
}

package httpapi

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (c *client) doH(method, path string, body any, hdr map[string]string) (int, map[string]any, http.Header) {
	opts := []opt{}
	for k, v := range hdr {
		opts = append(opts, func(r *http.Request) { r.Header.Set(k, v) })
	}
	return c.do(method, path, body, opts...)
}

func setupWorkspace(t *testing.T) (*env, *client, string) {
	e := newEnv(t)
	e.user("root@example.com", true)
	e.user("ed@example.com", false)
	root := e.login("root@example.com")
	_, body, _ := root.do("POST", "/api/v1/admin/workspaces", map[string]string{"name": "Shop", "owner_email": "root@example.com"})
	ws := body["id"].(string)
	if st, b, _ := root.do("POST", "/api/v1/w/"+ws+"/members", map[string]string{"email": "ed@example.com", "role": "editor"}); st != 201 {
		t.Fatalf("%d %v", st, b)
	}
	return e, root, ws
}

func TestSchemaAndRowsOverHTTP(t *testing.T) {
	e, root, ws := setupWorkspace(t)
	base := "/api/v1/w/" + ws
	ops := map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Products", "columns": []map[string]any{
			{"display_name": "Name", "field_type": "short_text", "is_required": true},
			{"display_name": "Price", "field_type": "currency"},
		}}}}}
	st, rep, _ := root.do("POST", base+"/schema/preview", ops)
	if st != 200 || len(rep["report"].(map[string]any)["breaking"].([]any)) != 0 {
		t.Fatalf("preview %d %v", st, rep)
	}
	st, res, _ := root.do("POST", base+"/schema/apply", ops)
	if st != 200 {
		t.Fatalf("apply %d %v", st, res)
	}
	tables := res["schema"].(map[string]any)["tables"].([]any)
	tbl := tables[0].(map[string]any)
	tid := tbl["id"].(string)
	cols := tbl["columns"].([]any)
	nameID, priceID := cols[1].(map[string]any)["id"].(string), cols[2].(map[string]any)["id"].(string)

	ed := e.login("ed@example.com")
	// Editors cannot change the schema.
	if st, _, _ := ed.do("POST", base+"/schema/apply", ops); st != 403 {
		t.Fatalf("editor applied schema: %d", st)
	}
	rows := base + "/tables/" + tid + "/rows"
	// Currency precision survives JSON numbers and strings.
	body := `{"fields":{"` + nameID + `":"Widget","` + priceID + `":19.99}}`
	st, r1, h := ed.doRaw("POST", rows, body, map[string]string{"Idempotency-Key": "abc"})
	if st != 201 || h.Get("ETag") != `"1"` || r1["fields"].(map[string]any)[priceID] != "19.9900" {
		t.Fatalf("insert %d %v %v", st, r1, h)
	}
	// Idempotent replay: same key + body → same response, one row.
	st, r2, _ := ed.doRaw("POST", rows, body, map[string]string{"Idempotency-Key": "abc"})
	if st != 201 || r2["id"] != r1["id"] {
		t.Fatalf("replay %d %v", st, r2)
	}
	if st, b, _ := ed.doRaw("POST", rows, strings.Replace(body, "Widget", "Gadget", 1), map[string]string{"Idempotency-Key": "abc"}); st != 409 || errCode(b) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("idempotency conflict %d %v", st, b)
	}
	id := fmt.Sprint(int(r1["id"].(float64)))
	// OCC over HTTP.
	if st, b, h := ed.doH("PATCH", rows+"/"+id, map[string]any{"fields": map[string]any{nameID: "Widget 2"}}, map[string]string{"If-Match": `"1"`}); st != 200 || h.Get("ETag") != `"2"` {
		t.Fatalf("patch %d %v", st, b)
	}
	st, b, _ := ed.doH("PATCH", rows+"/"+id, map[string]any{"fields": map[string]any{nameID: "stale"}}, map[string]string{"If-Match": `"1"`})
	if st != 409 || errCode(b) != "CONCURRENT_MODIFICATION" {
		t.Fatalf("stale patch %d %v", st, b)
	}
	cur := b["error"].(map[string]any)["details"].(map[string]any)["current"].(map[string]any)
	if cur["_version"].(float64) != 2 {
		t.Fatalf("details.current %v", cur)
	}
	if st, b, _ := ed.do("PATCH", rows+"/"+id, map[string]any{"fields": map[string]any{}}); st != 400 && st != 422 {
		t.Fatalf("missing If-Match: %d %v", st, b)
	}
	// Required field error names the field.
	if st, b, _ := ed.do("POST", rows, map[string]any{"fields": map[string]any{priceID: "1"}}); st != 422 || errCode(b) != "REQUIRED_FIELD" ||
		!strings.Contains(b["error"].(map[string]any)["message"].(string), `"Name"`) {
		t.Fatalf("required %d %v", st, b)
	}
	// List, filter, sort, count.
	for _, n := range []string{"Apple", "banana"} {
		ed.do("POST", rows, map[string]any{"fields": map[string]any{nameID: n}})
	}
	st, page, _ := ed.do("GET", rows+"?sort="+nameID+"&limit=2", nil)
	items := page["items"].([]any)
	if st != 200 || len(items) != 2 || items[0].(map[string]any)["fields"].(map[string]any)[nameID] != "Apple" || page["next_offset"].(float64) != 2 {
		t.Fatalf("list %d %v", st, page)
	}
	filter := fmt.Sprintf(`{"col":%q,"op":"starts","value":"b"}`, nameID)
	st, cnt, _ := ed.do("GET", rows+"/count?filter="+urlEscape(filter), nil)
	if st != 200 || cnt["count"].(float64) != 1 {
		t.Fatalf("count %d %v", st, cnt)
	}
	// Delete with If-Match.
	if st, _, _ := ed.doH("DELETE", rows+"/"+id, nil, map[string]string{"If-Match": `"2"`}); st != 204 {
		t.Fatalf("delete %d", st)
	}
}

func (c *client) doRaw(method, path, body string, hdr map[string]string) (int, map[string]any, http.Header) {
	c.e.t.Helper()
	var raw map[string]any
	st, out, h := c.do(method, path, rawJSON(body), func(r *http.Request) {
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
	})
	raw = out
	return st, raw, h
}

func TestEventsStreamDeliversRowChanges(t *testing.T) {
	e, root, ws := setupWorkspace(t)
	base := "/api/v1/w/" + ws
	_, res, _ := root.do("POST", base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Notes", "columns": []map[string]any{{"display_name": "Text", "field_type": "long_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	tid := tbl["id"].(string)
	textID := tbl["columns"].([]any)[1].(map[string]any)["id"].(string)

	// Client B listens; client A writes; B sees it in < 1 s.
	b := e.login("ed@example.com")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+base+"/events", nil)
	resp, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events %d %v", resp.StatusCode, resp.Header)
	}
	got := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: ") {
				got <- strings.TrimPrefix(sc.Text(), "event: ")
			}
		}
	}()
	if ev := <-got; ev != "hello" {
		t.Fatalf("first event %q", ev)
	}
	start := time.Now()
	root.do("POST", base+"/tables/"+tid+"/rows", map[string]any{"fields": map[string]any{textID: "hi"}})
	select {
	case ev := <-got:
		if ev != "rows.changed" {
			t.Fatalf("got %q", ev)
		}
		if el := time.Since(start); el > time.Second {
			t.Fatalf("took %v", el)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
	// Non-members cannot subscribe.
	e.user("x@example.com", false)
	x := e.login("x@example.com")
	resp2, err := x.http.Get(e.srv.URL + base + "/events")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("non-member events: %d", resp2.StatusCode)
	}
}

func TestQueriesOverHTTP(t *testing.T) {
	e, root, ws := setupWorkspace(t)
	base := "/api/v1/w/" + ws
	_, res, _ := root.do("POST", base+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "create_table", "table": map[string]any{
		"display_name": "Pets", "columns": []map[string]any{{"display_name": "Kind", "field_type": "short_text"}}}}}})
	tbl := res["schema"].(map[string]any)["tables"].([]any)[0].(map[string]any)
	tid := tbl["id"].(string)
	kind := tbl["columns"].([]any)[1].(map[string]any)["id"].(string)
	for _, k := range []string{"cat", "dog", "cat"} {
		root.do("POST", base+"/tables/"+tid+"/rows", map[string]any{"fields": map[string]any{kind: k}})
	}
	model := map[string]any{"sources": []any{map[string]any{"alias": "p", "table_id": tid, "x": 0, "y": 0}},
		"fields": []any{map[string]any{"alias": "p", "column_id": kind, "group": "group_by"},
			map[string]any{"alias": "p", "column_id": "id", "group": "count", "output_name": "N"}}}
	ed := e.login("ed@example.com")
	// Editors can run but not save.
	st, out, _ := ed.do("POST", base+"/queries/run", map[string]any{"model": model})
	if st != 200 || len(out["rows"].([]any)) != 2 {
		t.Fatalf("editor run %d %v", st, out)
	}
	if st, _, _ := ed.do("POST", base+"/queries", map[string]any{"name": "Counts", "kind": "visual", "visual_model": model}); st != 403 {
		t.Fatalf("editor saved query: %d", st)
	}
	if st, _, _ := ed.do("POST", base+"/sql/run", map[string]any{"sql": "SELECT 1"}); st != 403 {
		t.Fatalf("editor used SQL console: %d", st)
	}
	st, q, h := root.do("POST", base+"/queries", map[string]any{"name": "Counts", "kind": "visual", "visual_model": model})
	if st != 201 || h.Get("ETag") != `"1"` {
		t.Fatalf("save %d %v", st, q)
	}
	st, out, _ = ed.do("POST", base+"/queries/"+q["id"].(string)+"/run", map[string]any{})
	if st != 200 || len(out["rows"].([]any)) != 2 {
		t.Fatalf("editor run saved %d %v", st, out)
	}
	st, out, _ = root.do("POST", base+"/sql/run", map[string]any{"sql": "SELECT count(*) AS n FROM Pets WHERE Kind = :k", "params": map[string]any{"k": "cat"}})
	if st != 200 || out["rows"].([]any)[0].([]any)[0].(float64) != 2 {
		t.Fatalf("sql console %d %v", st, out)
	}
	st, out, _ = root.do("POST", base+"/sql/run", map[string]any{"sql": "ATTACH 'x.db' AS x"})
	if st != 403 || errCode(out) != "SQL_NOT_ALLOWED" {
		t.Fatalf("attach via console %d %v", st, out)
	}
}

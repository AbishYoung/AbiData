package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"abidata/internal/blogs"
)

func TestBlogModerationRoutes(t *testing.T) {
	e, ann, ws, root := publicBlog(t)
	base := "/api/v1/w/" + ws + "/blog"
	_, info, _ := ann.do("GET", base, nil)
	blogID := info["id"].(string)
	adm := "/api/v1/admin/blogs/" + blogID

	// Only administrators moderate.
	if st, _, _ := ann.do("GET", "/api/v1/admin/blogs", nil); st != 403 {
		t.Fatalf("owner lists blogs: %d", st)
	}
	st, list, _ := root.do("GET", "/api/v1/admin/blogs?q=an", nil)
	if items, _ := list["items"].([]any); st != 200 || len(items) != 1 || items[0].(map[string]any)["username"] != "ann" {
		t.Fatalf("list %d %v", st, list)
	}
	// The admin view shows published and unlisted posts, never drafts.
	st, view, _ := root.do("GET", adm, nil)
	if posts, _ := view["posts"].([]any); st != 200 || len(posts) != 2 {
		t.Fatalf("view %d %v", st, view)
	}

	// A post take-down: the page is the plain NOT_FOUND, the owner sees why.
	if st, body, _ := root.do("POST", adm+"/take-down", map[string]any{"post_slug": "hello-world", "reason_code": "spam", "note": "Write to abuse@example.com."}); st != 200 || body["active_takedowns"] != float64(1) {
		t.Fatalf("take down %d %v", st, body)
	}
	if st, _, _ := root.do("POST", adm+"/take-down", map[string]any{"post_slug": "hello-world", "reason_code": "spam"}); st != 409 {
		t.Fatalf("twice: %d", st)
	}
	if st, body, _ := e.visit("/b/ann/hello-world", nil); st != 404 || !bytes.Equal([]byte(body), blogs.NotFoundPage) {
		t.Fatalf("taken-down post served: %d", st)
	}
	if st, home, _ := e.visit("/b/ann", nil); st != 200 || bytes.Contains([]byte(home), []byte("Hello World")) {
		t.Fatalf("home lists the taken-down post: %d", st)
	}
	_, posts, _ := ann.do("GET", base+"/posts?status=taken_down", nil)
	items, _ := posts["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("owner's list %v", posts)
	}
	pid := items[0].(map[string]any)["id"]
	_, p, _ := ann.do("GET", fmt.Sprintf("%s/posts/%v", base, pid), nil)
	td, _ := p["takedown"].(map[string]any)
	if p["chip"] != "taken_down" || td["reason"] != "Spam" || td["note"] != "Write to abuse@example.com." {
		t.Fatalf("owner's post %v", p)
	}
	// Renaming does not lift the take-down. The blog API refuses a new
	// address; a rename through the generic rows API is stored, but neither
	// the new address nor the old one (which would redirect) serves the post.
	if st, body, _ := ann.do("PATCH", fmt.Sprintf("%s/posts/%v", base, pid), map[string]any{"_version": p["_version"], "slug": "fresh-start"}); st != 400 {
		t.Fatalf("rename under a take-down: %d %v", st, body)
	}
	_, sch, _ := ann.do("GET", "/api/v1/w/"+ws+"/schema", nil)
	var postsTable, slugCol string
	for _, tb := range sch["tables"].([]any) {
		tm := tb.(map[string]any)
		if tm["physical_name"] != "posts" {
			continue
		}
		postsTable = tm["id"].(string)
		for _, c := range tm["columns"].([]any) {
			if cm := c.(map[string]any); cm["physical_name"] == "slug" {
				slugCol = cm["id"].(string)
			}
		}
	}
	if postsTable == "" || slugCol == "" {
		t.Fatalf("posts table not found in %v", sch)
	}
	if st, body, _ := ann.doH("PATCH", fmt.Sprintf("/api/v1/w/%s/tables/%s/rows/%v", ws, postsTable, pid),
		map[string]any{"fields": map[string]any{slugCol: "fresh-start"}}, map[string]string{"If-Match": fmt.Sprintf(`"%v"`, p["_version"])}); st != 200 {
		t.Fatalf("generic rename: %d %v", st, body)
	}
	for _, path := range []string{"/b/ann/fresh-start", "/b/ann/hello-world"} {
		if st, _, _ := e.visit(path, nil); st != 404 {
			t.Fatalf("%s after renaming a taken-down post: %d", path, st)
		}
	}
	if st, home, _ := e.visit("/b/ann", nil); st != 200 || bytes.Contains([]byte(home), []byte("Hello World")) {
		t.Fatalf("home lists the renamed taken-down post: %d", st)
	}
	_, p, _ = ann.do("GET", fmt.Sprintf("%s/posts/%v", base, pid), nil)
	if p["chip"] != "taken_down" {
		t.Fatalf("owner's renamed post %v", p)
	}
	// The owner may unpublish and edit, but not publish again.
	st, p, _ = ann.do("PUT", fmt.Sprintf("%s/posts/%v/status", base, pid), map[string]any{"_version": p["_version"], "status": "draft"})
	if st != 200 {
		t.Fatalf("unpublish %d %v", st, p)
	}
	if st, body, _ := ann.do("PUT", fmt.Sprintf("%s/posts/%v/status", base, pid), map[string]any{"_version": p["_version"], "status": "published"}); st != 400 {
		t.Fatalf("republish under a take-down: %d %v", st, body)
	}

	// A blog take-down hides everything.
	if st, _, _ := root.do("POST", adm+"/take-down", map[string]any{"reason_code": "phishing"}); st != 200 {
		t.Fatal("blog take down")
	}
	if st, _, _ := e.visit("/b/ann/quiet-one", nil); st != 404 {
		t.Fatalf("post of a taken-down blog: %d", st)
	}
	if _, info, _ := ann.do("GET", base, nil); info["takedown"] == nil {
		t.Fatalf("owner's info %v", info)
	}
	if st, _, _ := root.do("POST", adm+"/restore", map[string]any{}); st != 200 {
		t.Fatal("restore blog")
	}
	if st, _, _ := e.visit("/b/ann/quiet-one", nil); st != 200 {
		t.Fatalf("after restore: %d", st)
	}
	if st, _, _ := root.do("POST", adm+"/restore", map[string]any{}); st != 404 {
		t.Fatalf("restore twice: %d", st)
	}

	// Readers report; only a hash of the address feeds the limiter.
	anon := e.client()
	if st, body, _ := anon.do("POST", "/api/v1/blogs/ann/report", map[string]any{"post_slug": "quiet-one", "reason_code": "phishing", "message": "Fake login."}); st != 202 {
		t.Fatalf("report %d %v", st, body)
	}
	if st, _, _ := anon.do("POST", "/api/v1/blogs/ann/report", map[string]any{"post_slug": "secret-draft", "reason_code": "spam"}); st != 404 {
		t.Fatalf("report of a draft: %d", st)
	}
	_, list, _ = root.do("GET", "/api/v1/admin/blogs?filter=reported", nil)
	if items, _ := list["items"].([]any); len(items) != 1 || items[0].(map[string]any)["open_reports"] != float64(1) {
		t.Fatalf("reported %v", list)
	}
	if st, view, _ := root.do("POST", adm+"/reports/resolve", nil); st != 200 || view["open_reports"] != float64(0) {
		t.Fatalf("resolve %d %v", st, view)
	}
	codes := []int{}
	for range 6 {
		st, _, _ := anon.do("POST", "/api/v1/blogs/ann/report", map[string]any{"reason_code": "other"})
		codes = append(codes, st)
	}
	if codes[len(codes)-1] != 429 {
		t.Fatalf("report limit %v", codes)
	}

	audit, _ := e.ad.ListAudit(context.Background(), 50)
	seen := map[string]bool{}
	for _, a := range audit {
		seen[a.Action] = true
	}
	if !seen["blog.takedown"] || !seen["blog.restore"] {
		t.Fatalf("audit %v", seen)
	}
}

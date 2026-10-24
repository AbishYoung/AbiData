package httpapi

import (
	"context"
	"testing"

	"abidata/internal/workspace/wstest"
)

func blogMembership(t *testing.T, me map[string]any) map[string]any {
	t.Helper()
	ms, _ := me["memberships"].([]any)
	for _, m := range ms {
		m := m.(map[string]any)
		if m["system_kind"] == "blog" {
			return m
		}
	}
	return nil
}

func TestBlogProvisioning(t *testing.T) {
	e := newEnv(t)
	e.user("root@example.com", true)
	// A user made before blogs (or by the CLI) gets one at sign-in.
	root := e.login("root@example.com")
	_, me, _ := root.do("GET", "/api/v1/auth/me", nil)
	rootBlog := blogMembership(t, me)
	if rootBlog == nil || rootBlog["role"] != "owner" || rootBlog["workspace_name"] != "Blog: root-1" {
		t.Fatalf("root blog at login: %v", me["memberships"])
	}

	// A user created through the API has one at once.
	st, body, _ := root.do("POST", "/api/v1/admin/users", map[string]any{"email": "ann@example.com", "display_name": "Ann", "password": "long-enough-pw"})
	if st != 201 {
		t.Fatalf("%d %v", st, body)
	}
	b, err := e.ad.GetBlogByUser(context.Background(), body["id"].(string))
	if err != nil || b.Username != "ann" {
		t.Fatalf("%+v %v", b, err)
	}
	ann := e.login("ann@example.com")
	_, me, _ = ann.do("GET", "/api/v1/auth/me", nil)
	m := blogMembership(t, me)
	if m == nil || m["workspace_id"] != string(b.WorkspaceID) {
		t.Fatalf("ann's memberships %v", me["memberships"])
	}
	ws := m["workspace_id"].(string)
	if st, body, _ := ann.do("GET", "/api/v1/w/"+ws, nil); st != 200 || body["system_kind"] != "blog" {
		t.Fatalf("workspace info %d %v", st, body)
	} else if want := map[bool]string{true: "postgres", false: "sqlite"}[wstest.Postgres()]; body["sql_dialect"] != want {
		t.Fatalf("sql_dialect = %v, want %s", body["sql_dialect"], want)
	}
	// Every user owns their blog workspace; it must not list the platform's users.
	if st, body, _ := ann.do("GET", "/api/v1/w/"+ws+"/members/candidates", nil); st != 400 {
		t.Fatalf("member candidates of a blog workspace: %d %v", st, body)
	}

	// The blog has one member, its user: no sharing, role changes or leaving.
	if st, body, _ := ann.do("POST", "/api/v1/w/"+ws+"/members", map[string]any{"email": "root@example.com", "role": "editor"}); st != 409 || errCode(body) != "WORKSPACE_PROTECTED" {
		t.Fatalf("add member %d %v", st, body)
	}
	if st, body, _ := ann.do("PATCH", "/api/v1/w/"+ws+"/members/"+b.UserID, map[string]any{"role": "viewer"}); st != 409 || errCode(body) != "WORKSPACE_PROTECTED" {
		t.Fatalf("change role %d %v", st, body)
	}
	if st, body, _ := ann.do("DELETE", "/api/v1/w/"+ws+"/members/"+b.UserID, nil); st != 409 || errCode(body) != "WORKSPACE_PROTECTED" {
		t.Fatalf("leave %d %v", st, body)
	}

	// Its pinned table cannot be dropped through the schema API.
	_, schema, _ := ann.do("GET", "/api/v1/w/"+ws+"/schema", nil)
	tables, _ := schema["tables"].([]any)
	var postsID string
	for _, tb := range tables {
		tb := tb.(map[string]any)
		if tb["physical_name"] == "posts" {
			postsID = tb["id"].(string)
		}
	}
	if postsID == "" {
		t.Fatalf("no posts table in %v", schema)
	}
	st, body, _ = ann.do("POST", "/api/v1/w/"+ws+"/schema/apply", map[string]any{"ops": []map[string]any{{"op": "drop_table", "table_id": postsID}}, "force": true})
	if st != 400 {
		t.Fatalf("drop posts %d %v", st, body)
	}
}

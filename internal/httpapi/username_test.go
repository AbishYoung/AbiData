package httpapi

import (
	"context"
	"testing"
)

func TestUsernameRoutes(t *testing.T) {
	e := newEnv(t)
	e.user("root@example.com", true)
	root := e.login("root@example.com")

	st, body, _ := root.do("POST", "/api/v1/admin/users", map[string]any{"email": "Dee.Dee@example.com", "display_name": "Dee", "password": "long-enough-pw"})
	if st != 201 || body["username"] != "dee-dee" {
		t.Fatalf("derived: %d %v", st, body)
	}
	dee := body["id"].(string)
	st, body, _ = root.do("POST", "/api/v1/admin/users", map[string]any{"email": "eve@example.com", "username": "Evie", "display_name": "Eve", "password": "long-enough-pw"})
	if st != 201 || body["username"] != "evie" {
		t.Fatalf("chosen: %d %v", st, body)
	}
	if st, body, _ := root.do("POST", "/api/v1/admin/users", map[string]any{"email": "f@example.com", "username": "evie", "display_name": "F", "password": "long-enough-pw"}); st != 409 {
		t.Fatalf("taken: %d %v", st, body)
	}
	if st, body, _ := root.do("POST", "/api/v1/admin/users", map[string]any{"email": "f@example.com", "username": "admin", "display_name": "F", "password": "long-enough-pw"}); st != 400 {
		t.Fatalf("reserved: %d %v", st, body)
	}

	// The user renames themself once; the second change within 30 days is refused.
	c := e.login("dee.dee@example.com")
	if st, body, _ := c.do("PUT", "/api/v1/auth/username", map[string]any{"username": "dee"}); st != 200 || body["username"] != "dee" {
		t.Fatalf("rename: %d %v", st, body)
	}
	if st, body, _ := c.do("GET", "/api/v1/auth/me", nil); st != 200 || body["user"].(map[string]any)["username"] != "dee" {
		t.Fatalf("me: %d %v", st, body)
	}
	if st, body, _ := c.do("PUT", "/api/v1/auth/username", map[string]any{"username": "dee-two"}); st != 400 {
		t.Fatalf("too soon: %d %v", st, body)
	}
	// An admin is not limited.
	if st, body, _ := root.do("PATCH", "/api/v1/admin/users/"+dee, map[string]any{"username": "deedee"}); st != 200 || body["username"] != "deedee" {
		t.Fatalf("admin rename: %d %v", st, body)
	}
	if st, body, _ := root.do("PATCH", "/api/v1/admin/users/"+dee, map[string]any{"username": "evie"}); st != 409 {
		t.Fatalf("admin rename to taken: %d %v", st, body)
	}

	audit, _ := e.ad.ListAudit(context.Background(), 50)
	renames := 0
	for _, a := range audit {
		if a.Action == "user.rename" {
			renames++
		}
	}
	if renames != 2 {
		t.Fatalf("%d user.rename audit rows, want 2", renames)
	}
	if u, err := e.ad.GetUserByUsername(context.Background(), "deedee"); err != nil || u.ID != dee {
		t.Fatalf("%+v %v", u, err)
	}
}

package httpapi

import (
	"context"
	"testing"
)

func TestDisplayNameCannotImitateAPIKey(t *testing.T) {
	e := newEnv(t)
	e.user("root@example.com", true)
	root := e.login("root@example.com")
	st, body, _ := root.do("POST", "/api/v1/admin/users", map[string]any{"email": "v@example.com", "display_name": "Vera", "password": "long-enough-pw"})
	if st != 201 {
		t.Fatalf("%d %v", st, body)
	}
	victim := body["id"].(string)

	for _, name := range []string{`API key "ci"`, "api key x", "  API KEY", "Api  Key  deploy", "API keys"} {
		st, body, _ := root.do("POST", "/api/v1/admin/users", map[string]any{"email": "n@example.com", "display_name": name, "password": "long-enough-pw"})
		if st != 400 {
			t.Errorf("create %q: %d %v", name, st, body)
		}
		st, body, _ = root.do("PATCH", "/api/v1/admin/users/"+victim, map[string]any{"display_name": name})
		if st != 400 {
			t.Errorf("update %q: %d %v", name, st, body)
		}
	}
	if st, body, _ := root.do("POST", "/api/v1/admin/users", map[string]any{"email": "ok@example.com", "display_name": "Apia Keyes", "password": "long-enough-pw"}); st != 201 {
		t.Fatalf("legit name refused: %d %v", st, body)
	}
	if st, body, _ := root.do("PATCH", "/api/v1/admin/users/"+victim, map[string]any{"display_name": "Vic"}); st != 200 {
		t.Fatalf("legit rename refused: %d %v", st, body)
	}
}

func TestSetupRejectsAPIKeyDisplayName(t *testing.T) {
	e := newEnv(t)
	tok, err := e.au.PrepareBootstrap(context.Background(), "first@example.com")
	if err != nil || tok == "" {
		t.Fatal(err)
	}
	c := e.client()
	if st, body, _ := c.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok, "display_name": "API key root", "password": "long-enough-pw"}); st != 400 {
		t.Fatalf("%d %v", st, body)
	}
	// The refusal does not burn the one-time token.
	if st, body, _ := c.do("POST", "/api/v1/auth/setup", map[string]string{"token": tok, "display_name": "Root", "password": "long-enough-pw"}); st != 200 {
		t.Fatalf("%d %v", st, body)
	}
}

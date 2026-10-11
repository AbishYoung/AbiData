package httpapi

import (
	"context"
	"testing"
)

func TestAdminResetUserPassword(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.user("root@example.com", true)
	e.user("plain@example.com", false)
	e.user("victim@example.com", false)
	root := e.login("root@example.com")
	plain := e.login("plain@example.com")
	victim := e.login("victim@example.com")
	victim2 := e.login("victim@example.com")
	vu, err := e.ad.GetUserByEmail(ctx, "victim@example.com")
	if err != nil {
		t.Fatal(err)
	}
	ru, _ := e.ad.GetUserByEmail(ctx, "root@example.com")
	path := "/api/v1/admin/users/" + vu.ID + "/password"
	body := map[string]string{"new_password": "brand-new-password"}

	// Non-admin, anonymous, and self-reset are refused.
	if st, _, _ := plain.do("POST", path, body); st != 403 {
		t.Errorf("non-admin: %d", st)
	}
	if st, _, _ := e.client().do("POST", path, body); st != 401 {
		t.Errorf("anonymous: %d", st)
	}
	if st, b, _ := root.do("POST", "/api/v1/admin/users/"+ru.ID+"/password", body); st != 400 {
		t.Errorf("self reset: %d %v", st, b)
	}
	// Policy, unknown user.
	if st, _, _ := root.do("POST", path, map[string]string{"new_password": "short"}); st != 400 {
		t.Errorf("short password: %d", st)
	}
	if st, _, _ := root.do("POST", "/api/v1/admin/users/nope/password", body); st != 404 {
		t.Errorf("unknown user: %d", st)
	}
	if st, _, _ := victim.do("GET", "/api/v1/auth/me", nil); st != 200 {
		t.Fatalf("victim session before: %d", st)
	}

	// Build up lockout counters.
	for i := 0; i < 5; i++ {
		e.client().do("POST", "/api/v1/auth/login", map[string]string{"email": "victim@example.com", "password": "wrong-wrong-wrong"})
	}
	if st, _, _ := e.client().do("POST", "/api/v1/auth/login", map[string]string{"email": "victim@example.com", "password": "long-enough-pw"}); st != 429 {
		t.Fatalf("expected lockout, got %d", st)
	}

	if st, b, _ := root.do("POST", path, body); st != 204 {
		t.Fatalf("reset: %d %v", st, b)
	}
	// Every session of the target ended; the admin's stays.
	for _, c := range []*client{victim, victim2} {
		if st, _, _ := c.do("GET", "/api/v1/auth/me", nil); st != 401 {
			t.Errorf("victim session survived: %d", st)
		}
	}
	if st, _, _ := root.do("GET", "/api/v1/auth/me", nil); st != 200 {
		t.Errorf("admin session ended: %d", st)
	}
	// Old password gone, new works (so the lockout was cleared).
	if st, _, _ := e.client().do("POST", "/api/v1/auth/login", map[string]string{"email": "victim@example.com", "password": "long-enough-pw"}); st != 401 {
		t.Errorf("old password: %d", st)
	}
	if st, b, _ := e.client().do("POST", "/api/v1/auth/login", map[string]string{"email": "victim@example.com", "password": "brand-new-password"}); st != 200 {
		t.Errorf("new password: %d %v", st, b)
	}

	// Audit entry names actor and target and never the password.
	log, err := e.ad.ListAudit(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range log {
		if a.Action != "user.password_reset" {
			continue
		}
		found = true
		if a.UserID != ru.ID || a.ObjectID != vu.ID || a.ObjectKind != "user" {
			t.Errorf("audit entry: %+v", a)
		}
		for _, v := range a.Details {
			if s, _ := v.(string); s == "brand-new-password" {
				t.Error("audit holds the password")
			}
		}
	}
	if !found {
		t.Error("no user.password_reset audit entry")
	}
}

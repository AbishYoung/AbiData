package httpapi

import (
	"testing"
)

func TestMePublishedApps(t *testing.T) {
	e := newEnv(t)
	e.user("admin@example.com", true)
	adminClient := e.login("admin@example.com")

	// Create a regular workspace owned by admin.
	st, body, _ := adminClient.do("POST", "/api/v1/admin/workspaces", map[string]any{"name": "App Hub"})
	if st != 201 {
		t.Fatalf("create workspace: %d %v", st, body)
	}
	wsID := body["id"].(string)

	// Create an app in wsID.
	st, appBody, _ := adminClient.do("POST", "/api/v1/w/"+wsID+"/apps", map[string]any{
		"name":  "Live Portal",
		"items": []any{},
	})
	if st != 201 {
		t.Fatalf("create app: %d %v", st, appBody)
	}
	appID := appBody["id"].(string)

	// Check /api/v1/auth/me: Live Portal should appear in published_apps.
	st, meBody, _ := adminClient.do("GET", "/api/v1/auth/me", nil)
	if st != 200 {
		t.Fatalf("me: %d %v", st, meBody)
	}
	memberships := meBody["memberships"].([]any)
	var foundWs map[string]any
	for _, m := range memberships {
		mm := m.(map[string]any)
		if mm["workspace_id"] == wsID {
			foundWs = mm
			break
		}
	}
	if foundWs == nil {
		t.Fatalf("workspace %s not in memberships: %v", wsID, memberships)
	}
	pubApps, ok := foundWs["published_apps"].([]any)
	if !ok || len(pubApps) != 1 {
		t.Fatalf("published_apps = %v, want 1 app", foundWs["published_apps"])
	}
	firstApp := pubApps[0].(map[string]any)
	if firstApp["name"] != "Live Portal" || firstApp["id"] != appID {
		t.Fatalf("unexpected app: %v", firstApp)
	}
	if firstApp["path"] == "" || firstApp["link"] == "" {
		t.Fatalf("missing path or link: %v", firstApp)
	}

	// Disable the app: it should now be omitted from published_apps.
	st, _, _ = adminClient.do("PUT", "/api/v1/w/"+wsID+"/apps/"+appID, map[string]any{
		"name":    "Live Portal",
		"enabled": false,
		"items":   []any{},
	})
	if st != 200 {
		t.Fatalf("disable app: %d", st)
	}

	st, meBody, _ = adminClient.do("GET", "/api/v1/auth/me", nil)
	if st != 200 {
		t.Fatalf("me: %d", st)
	}
	memberships = meBody["memberships"].([]any)
	for _, m := range memberships {
		mm := m.(map[string]any)
		if mm["workspace_id"] == wsID {
			if apps, ok := mm["published_apps"]; ok && apps != nil && len(apps.([]any)) > 0 {
				t.Fatalf("expected no published_apps when disabled, got %v", apps)
			}
		}
	}

	// Re-enable app, but take it down as platform admin.
	st, _, _ = adminClient.do("PUT", "/api/v1/w/"+wsID+"/apps/"+appID, map[string]any{
		"name":    "Live Portal",
		"enabled": true,
		"items":   []any{},
	})
	if st != 200 {
		t.Fatalf("re-enable app: %d", st)
	}
	st, _, _ = adminClient.do("POST", "/api/v1/admin/apps/"+appID+"/take-down", nil)
	if st != 204 {
		t.Fatalf("takedown app: %d", st)
	}

	st, meBody, _ = adminClient.do("GET", "/api/v1/auth/me", nil)
	if st != 200 {
		t.Fatalf("me: %d", st)
	}
	memberships = meBody["memberships"].([]any)
	for _, m := range memberships {
		mm := m.(map[string]any)
		if mm["workspace_id"] == wsID {
			if apps, ok := mm["published_apps"]; ok && apps != nil && len(apps.([]any)) > 0 {
				t.Fatalf("expected no published_apps when taken down, got %v", apps)
			}
		}
	}

	// Restore takedown, but turn off public apps globally.
	st, _, _ = adminClient.do("POST", "/api/v1/admin/apps/"+appID+"/restore", nil)
	if st != 204 {
		t.Fatalf("restore app: %d", st)
	}
	st, _, _ = adminClient.do("PUT", "/api/v1/admin/settings/public-apps", map[string]any{"enabled": false})
	if st != 200 {
		t.Fatalf("disable public apps: %d", st)
	}

	st, meBody, _ = adminClient.do("GET", "/api/v1/auth/me", nil)
	if st != 200 {
		t.Fatalf("me: %d", st)
	}
	memberships = meBody["memberships"].([]any)
	for _, m := range memberships {
		mm := m.(map[string]any)
		if mm["workspace_id"] == wsID {
			if apps, ok := mm["published_apps"]; ok && apps != nil && len(apps.([]any)) > 0 {
				t.Fatalf("expected no published_apps when public apps off, got %v", apps)
			}
		}
	}
}

func TestWorkspaceMembersCandidatesAndAdd(t *testing.T) {
	e := newEnv(t)
	e.user("admin@example.com", true)
	adminClient := e.login("admin@example.com")

	// Create extra users.
	st, bAlice, _ := adminClient.do("POST", "/api/v1/admin/users", map[string]any{
		"email": "alice@example.com", "username": "alice", "display_name": "Alice Wonder", "password": "long-enough-pw",
	})
	if st != 201 {
		t.Fatalf("create alice: %d %v", st, bAlice)
	}
	aliceID := bAlice["id"].(string)

	st, bBob, _ := adminClient.do("POST", "/api/v1/admin/users", map[string]any{
		"email": "bob@example.com", "username": "bob", "display_name": "Bob Builder", "password": "long-enough-pw",
	})
	if st != 201 {
		t.Fatalf("create bob: %d %v", st, bBob)
	}

	st, bDisabled, _ := adminClient.do("POST", "/api/v1/admin/users", map[string]any{
		"email": "disabled@example.com", "username": "disabled", "display_name": "Disabled User", "password": "long-enough-pw",
	})
	if st != 201 {
		t.Fatalf("create disabled: %d %v", st, bDisabled)
	}
	disabledID := bDisabled["id"].(string)
	// Disable that user.
	if st, _, _ := adminClient.do("PATCH", "/api/v1/admin/users/"+disabledID, map[string]any{"disabled": true}); st != 200 {
		t.Fatalf("disable user: %d", st)
	}

	// Create workspace.
	st, wsBody, _ := adminClient.do("POST", "/api/v1/admin/workspaces", map[string]any{"name": "Team Space"})
	if st != 201 {
		t.Fatalf("create workspace: %d %v", st, wsBody)
	}
	wsID := wsBody["id"].(string)

	// List candidates as owner: Alice and Bob should be candidates; admin (already owner) and disabled user should not.
	st, candsBody, _ := adminClient.do("GET", "/api/v1/w/"+wsID+"/members/candidates", nil)
	if st != 200 {
		t.Fatalf("candidates: %d %v", st, candsBody)
	}
	items := candsBody["items"].([]any)
	candEmails := []string{}
	for _, it := range items {
		m := it.(map[string]any)
		candEmails = append(candEmails, m["email"].(string))
		if m["email"] == "alice@example.com" {
			if m["username"] != "alice" || m["display_name"] != "Alice Wonder" || m["user_id"] != aliceID {
				t.Fatalf("unexpected alice candidate fields: %v", m)
			}
		}
	}
	hasAlice := false
	hasBob := false
	for _, email := range candEmails {
		if email == "alice@example.com" {
			hasAlice = true
		}
		if email == "bob@example.com" {
			hasBob = true
		}
		if email == "admin@example.com" {
			t.Fatalf("admin should not be a candidate (already member)")
		}
		if email == "disabled@example.com" {
			t.Fatalf("disabled user should not be a candidate")
		}
	}
	if !hasAlice || !hasBob {
		t.Fatalf("candidates missing alice or bob: %v", candEmails)
	}

	// Add Alice by username.
	st, addAlice, _ := adminClient.do("POST", "/api/v1/w/"+wsID+"/members", map[string]any{
		"username": "alice",
		"role":     "builder",
	})
	if st != 201 {
		t.Fatalf("add alice by username: %d %v", st, addAlice)
	}
	if addAlice["user_id"] != aliceID || addAlice["role"] != "builder" || addAlice["username"] != "alice" {
		t.Fatalf("unexpected add member response: %v", addAlice)
	}

	// Duplicate add must fail with 409.
	if st, _, _ := adminClient.do("POST", "/api/v1/w/"+wsID+"/members", map[string]any{"username": "alice", "role": "editor"}); st != 409 {
		t.Fatalf("expected 409 for duplicate member, got %d", st)
	}

	// Candidates list should now no longer have Alice.
	st, candsBody, _ = adminClient.do("GET", "/api/v1/w/"+wsID+"/members/candidates", nil)
	if st != 200 {
		t.Fatalf("candidates: %d %v", st, candsBody)
	}
	items = candsBody["items"].([]any)
	for _, it := range items {
		m := it.(map[string]any)
		if m["email"] == "alice@example.com" {
			t.Fatalf("alice should no longer be a candidate after being added")
		}
	}

	// Non-owner cannot list candidates or members.
	aliceClient := e.login("alice@example.com")
	if st, _, _ := aliceClient.do("GET", "/api/v1/w/"+wsID+"/members/candidates", nil); st != 403 {
		t.Fatalf("expected 403 for non-owner candidates, got %d", st)
	}
	if st, _, _ := aliceClient.do("GET", "/api/v1/w/"+wsID+"/members", nil); st != 403 {
		t.Fatalf("expected 403 for non-owner members, got %d", st)
	}

	// List members as owner: includes username.
	st, membersBody, _ := adminClient.do("GET", "/api/v1/w/"+wsID+"/members", nil)
	if st != 200 {
		t.Fatalf("list members: %d %v", st, membersBody)
	}
	mItems := membersBody["items"].([]any)
	foundAlice := false
	for _, it := range mItems {
		m := it.(map[string]any)
		if m["email"] == "alice@example.com" {
			foundAlice = true
			if m["username"] != "alice" {
				t.Fatalf("expected username alice on member DTO, got %v", m["username"])
			}
		}
	}
	if !foundAlice {
		t.Fatalf("alice not found in members: %v", mItems)
	}

	// Adding non-existent email returns 400 with expected error message.
	st, errBody, _ := adminClient.do("POST", "/api/v1/w/"+wsID+"/members", map[string]any{
		"email": "nobody@example.com",
		"role":  "viewer",
	})
	if st != 400 {
		t.Fatalf("expected 400 for unknown email, got %d", st)
	}
	msg := ""
	if errObj, ok := errBody["error"].(map[string]any); ok {
		msg, _ = errObj["message"].(string)
	}
	if msg != "No user has that email. Ask a platform administrator to create the account first." {
		t.Fatalf("unexpected error detail: %v", errBody)
	}
}

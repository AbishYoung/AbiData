package httpapi

import (
	"testing"
)

func TestMessagesRoutes(t *testing.T) {
	e := newEnv(t)
	e.user("admin@example.com", true)
	e.user("priya@example.com", false)
	e.user("sam@example.com", false)
	e.user("outsider@example.com", false)

	adminClient := e.login("admin@example.com")
	priya := e.login("priya@example.com")
	sam := e.login("sam@example.com")
	outsider := e.login("outsider@example.com")

	// Create a shared workspace and add Priya and Sam.
	st, wsBody, _ := adminClient.do("POST", "/api/v1/admin/workspaces", map[string]any{"name": "Northwind Traders"})
	if st != 201 {
		t.Fatalf("create workspace: %d %v", st, wsBody)
	}
	wsID := wsBody["id"].(string)
	if st, _, _ := adminClient.do("POST", "/api/v1/w/"+wsID+"/members", map[string]any{
		"email": "priya@example.com", "role": "editor",
	}); st != 201 {
		t.Fatalf("add priya: %d", st)
	}
	if st, _, _ := adminClient.do("POST", "/api/v1/w/"+wsID+"/members", map[string]any{
		"email": "sam@example.com", "role": "viewer",
	}); st != 201 {
		t.Fatalf("add sam: %d", st)
	}

	// Recipient directory lists active users and Priya's non-blog workspaces.
	st, dirBody, _ := priya.do("GET", "/api/v1/messages/recipients", nil)
	if st != 200 {
		t.Fatalf("recipients: %d %v", st, dirBody)
	}
	users := dirBody["users"].([]any)
	workspaces := dirBody["workspaces"].([]any)
	if len(users) != 4 || len(workspaces) != 1 {
		t.Fatalf("directory: users=%d workspaces=%d (%v)", len(users), len(workspaces), dirBody)
	}

	// Priya sends a direct message to @sam.
	st, created, _ := priya.do("POST", "/api/v1/messages/threads", map[string]any{
		"kind":       "direct",
		"recipients": []string{"@sam"},
		"subject":    "Supplier reconciliation",
		"body":       "Hi Sam, did the Q3 shipment numbers match?",
	})
	if st != 201 {
		t.Fatalf("create direct thread: %d %v", st, created)
	}
	directID := created["id"].(string)
	if created["kind"] != "direct" || created["unread"] != false {
		t.Fatalf("created thread: %v", created)
	}

	// Sam has 1 unread thread; Priya and outsider have 0.
	if st, body, _ := sam.do("GET", "/api/v1/messages/unread-count", nil); st != 200 || body["unread_count"] != float64(1) {
		t.Fatalf("sam unread: %d %v", st, body)
	}
	if st, body, _ := priya.do("GET", "/api/v1/messages/unread-count", nil); st != 200 || body["unread_count"] != float64(0) {
		t.Fatalf("priya unread: %d %v", st, body)
	}

	// Outsider cannot read, reply to, or patch the direct thread (404).
	if st, _, _ := outsider.do("GET", "/api/v1/messages/threads/"+directID, nil); st != 404 {
		t.Fatalf("outsider get direct thread: %d, want 404", st)
	}
	if st, _, _ := outsider.do("POST", "/api/v1/messages/threads/"+directID+"/replies", map[string]any{"body": "nope"}); st != 404 {
		t.Fatalf("outsider reply direct thread: %d, want 404", st)
	}
	if st, _, _ := outsider.do("PATCH", "/api/v1/messages/threads/"+directID, map[string]any{"read": true}); st != 404 {
		t.Fatalf("outsider patch direct thread: %d, want 404", st)
	}

	// Sam replies to the direct thread; Priya now sees it as unread.
	st, afterReply, _ := sam.do("POST", "/api/v1/messages/threads/"+directID+"/replies", map[string]any{
		"body": "Yes, all 42 pallets matched the manifest.",
	})
	if st != 201 {
		t.Fatalf("sam reply: %d %v", st, afterReply)
	}
	msgs := afterReply["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if st, body, _ := priya.do("GET", "/api/v1/messages/unread-count", nil); st != 200 || body["unread_count"] != float64(1) {
		t.Fatalf("priya unread after reply: %d %v", st, body)
	}

	// Priya marks the thread read and archives it.
	if st, body, _ := priya.do("PATCH", "/api/v1/messages/threads/"+directID, map[string]any{
		"read": true, "archived": true,
	}); st != 200 || body["unread"] != false || body["archived"] != true {
		t.Fatalf("priya patch: %d %v", st, body)
	}
	if st, body, _ := priya.do("GET", "/api/v1/messages/threads?box=archived", nil); st != 200 || len(body["items"].([]any)) != 1 {
		t.Fatalf("priya archived box: %d %v", st, body)
	}

	// Workspace messaging: outsider cannot post to or filter by Northwind Traders (404).
	if st, _, _ := outsider.do("POST", "/api/v1/messages/threads", map[string]any{
		"kind": "workspace", "workspace_id": wsID, "subject": "Intrusion", "body": "Nope",
	}); st != 404 {
		t.Fatalf("outsider create workspace thread: %d, want 404", st)
	}
	if st, _, _ := outsider.do("GET", "/api/v1/messages/threads?workspace_id="+wsID, nil); st != 404 {
		t.Fatalf("outsider list workspace threads: %d, want 404", st)
	}

	// Priya posts a workspace thread to Northwind Traders.
	st, wsThread, _ := priya.do("POST", "/api/v1/messages/threads", map[string]any{
		"kind":         "workspace",
		"workspace_id": wsID,
		"subject":      "Monthly close schedule",
		"body":         "Please finalize all Northwind order edits by Friday 18:00.",
	})
	if st != 201 {
		t.Fatalf("create workspace thread: %d %v", st, wsThread)
	}
	wsThreadID := wsThread["id"].(string)

	// Sam sees the workspace thread in the workspace box and unread count = 1.
	st, samWSList, _ := sam.do("GET", "/api/v1/messages/threads?box=workspace&workspace_id="+wsID, nil)
	if st != 200 || len(samWSList["items"].([]any)) != 1 || samWSList["unread_count"] != float64(1) {
		t.Fatalf("sam workspace list: %d %v", st, samWSList)
	}

	// Outsider cannot view the workspace thread (404).
	if st, _, _ := outsider.do("GET", "/api/v1/messages/threads/"+wsThreadID, nil); st != 404 {
		t.Fatalf("outsider get workspace thread: %d, want 404", st)
	}
}

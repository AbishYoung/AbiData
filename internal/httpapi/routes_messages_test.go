package httpapi

import (
	"net/http"
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

	// API key cannot access internal messages (metaSessionOnly: true -> 403 Forbidden).
	st, keyRes, _ := adminClient.do("POST", "/api/v1/w/"+wsID+"/api-keys", map[string]any{
		"name": "bot-key", "role": "editor",
	})
	if st != 201 {
		t.Fatalf("create api key: %d %v", st, keyRes)
	}
	keyTok := keyRes["token"].(string)
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/messages/threads", nil)
	req.Header.Set("Authorization", "Bearer "+keyTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("api key get messages st = %d, want 403", resp.StatusCode)
	}

	// Recipient resolution:
	// 1. Duplicate recipients are deduplicated
	st, dupCreated, _ := priya.do("POST", "/api/v1/messages/threads", map[string]any{
		"kind":       "direct",
		"recipients": []string{"@sam", "sam@example.com"},
		"subject":    "Dup test",
		"body":       "Hello Sam again",
	})
	if st != 201 {
		t.Fatalf("create direct thread with dups: %d %v", st, dupCreated)
	}
	parts := dupCreated["participants"].([]any)
	if len(parts) != 2 {
		t.Fatalf("expected 2 participants after dedup, got %d", len(parts))
	}

	// 2. Messaging oneself (note to self)
	st, selfCreated, _ := priya.do("POST", "/api/v1/messages/threads", map[string]any{
		"kind":       "direct",
		"recipients": []string{"@priya"},
		"subject":    "Self notes",
		"body":       "Todo item 1",
	})
	if st != 201 {
		t.Fatalf("create self thread: %d %v", st, selfCreated)
	}
	if len(selfCreated["participants"].([]any)) != 1 {
		t.Fatalf("expected 1 participant for self thread: %v", selfCreated)
	}

	// 3. Unknown recipient -> 400 Bad Request
	if st, badRes, _ := priya.do("POST", "/api/v1/messages/threads", map[string]any{
		"kind":       "direct",
		"recipients": []string{"@nobody_ever"},
		"subject":    "Unknown",
		"body":       "Fail",
	}); st != 400 {
		t.Fatalf("unknown recipient st = %d, want 400 (%v)", st, badRes)
	}

	// 4. Disabled user -> 400 Bad Request
	_, usersList, _ := adminClient.do("GET", "/api/v1/admin/users", nil)
	var outsiderID string
	for _, item := range usersList["items"].([]any) {
		m := item.(map[string]any)
		if m["email"] == "outsider@example.com" {
			outsiderID = m["id"].(string)
			break
		}
	}
	if st, _, _ := adminClient.do("PATCH", "/api/v1/admin/users/"+outsiderID, map[string]any{
		"disabled": true,
	}); st != 200 {
		t.Fatalf("disable outsider: %d", st)
	}
	if st, disRes, _ := priya.do("POST", "/api/v1/messages/threads", map[string]any{
		"kind":       "direct",
		"recipients": []string{"outsider@example.com"},
		"subject":    "Disabled",
		"body":       "Fail",
	}); st != 400 {
		t.Fatalf("disabled recipient st = %d, want 400 (%v)", st, disRes)
	}

	// Reply threading:
	// 1. Reply referencing a specific parent message in the thread
	st, directDetail, _ := priya.do("GET", "/api/v1/messages/threads/"+directID, nil)
	if st != 200 {
		t.Fatalf("get direct thread: %d", st)
	}
	firstMsgID := directDetail["messages"].([]any)[0].(map[string]any)["id"].(string)
	st, targetReply, _ := priya.do("POST", "/api/v1/messages/threads/"+directID+"/replies", map[string]any{
		"parent_id": firstMsgID,
		"body":      "Got the numbers.",
	})
	if st != 201 {
		t.Fatalf("reply with parent_id: %d %v", st, targetReply)
	}
	replies := targetReply["messages"].([]any)
	lastMsg := replies[len(replies)-1].(map[string]any)
	if lastMsg["parent_id"] != firstMsgID {
		t.Fatalf("reply parent_id = %v, want %v", lastMsg["parent_id"], firstMsgID)
	}

	// 2. Reply with bogus parent_id -> 400 Bad Request
	if st, badParent, _ := priya.do("POST", "/api/v1/messages/threads/"+directID+"/replies", map[string]any{
		"parent_id": "msg_00000000000000000000000000",
		"body":      "Invalid parent",
	}); st != 400 {
		t.Fatalf("bogus parent_id st = %d, want 400 (%v)", st, badParent)
	}

	// 3. Search by @username and filter box=workspaces
	st, searchRes, _ := priya.do("GET", "/api/v1/messages/threads?q=@sam", nil)
	if st != 200 || len(searchRes["items"].([]any)) == 0 {
		t.Fatalf("search q=@sam st=%d, want items (%v)", st, searchRes)
	}
	st, wsBoxRes, _ := priya.do("GET", "/api/v1/messages/threads?box=workspaces", nil)
	if st != 200 || len(wsBoxRes["items"].([]any)) != 1 {
		t.Fatalf("box=workspaces st=%d, want 1 item (%v)", st, wsBoxRes)
	}
}

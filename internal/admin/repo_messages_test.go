package admin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDirectMessageThreads(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	mkUser(t, s, "u2", "bob@example.com")
	mkUser(t, s, "u3", "cara@example.com")
	_ = s.InitUsername(ctx, "u1", "ann")
	_ = s.InitUsername(ctx, "u2", "bob")
	_ = s.InitUsername(ctx, "u3", "cara")

	thr, err := s.CreateMessageThread(ctx, CreateThreadParams{
		Kind:         KindDirectThread,
		SenderID:     "u1",
		RecipientIDs: []string{"u2"},
		Subject:      "Quarterly inventory check",
		Body:         "Hi Bob, can you verify the warehouse counts?",
		Now:          t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if thr.Kind != KindDirectThread || thr.MessageCount != 1 || thr.Unread || thr.Archived {
		t.Fatalf("sender view of created thread: %+v", thr)
	}
	if len(thr.Messages) != 1 || thr.Messages[0].Body != "Hi Bob, can you verify the warehouse counts?" || thr.Messages[0].ParentID != "" {
		t.Fatalf("initial message: %+v", thr.Messages)
	}
	if len(thr.Participants) != 2 {
		t.Fatalf("expected 2 participants, got %+v", thr.Participants)
	}

	// Non-participant u3 cannot view or reply.
	if _, err := s.GetMessageThread(ctx, thr.ID, "u3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("u3 GetMessageThread = %v, want ErrNotFound", err)
	}
	if _, err := s.ReplyMessageThread(ctx, ReplyThreadParams{
		ThreadID: thr.ID, SenderID: "u3", Body: "intruder", Now: t0.Add(time.Minute),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("u3 ReplyMessageThread = %v, want ErrNotFound", err)
	}

	// Recipient u2 sees the thread as unread in inbox and unread boxes.
	if n, err := s.CountUnreadThreads(ctx, "u2"); err != nil || n != 1 {
		t.Fatalf("u2 unread count = %d, %v; want 1", n, err)
	}
	if n, err := s.CountUnreadThreads(ctx, "u1"); err != nil || n != 0 {
		t.Fatalf("u1 unread count = %d, %v; want 0", n, err)
	}
	u2Threads, u2Unread, err := s.ListMessageThreads(ctx, "u2", MessageThreadFilter{Box: "unread"})
	if err != nil || u2Unread != 1 || len(u2Threads) != 1 || !u2Threads[0].Unread {
		t.Fatalf("u2 unread list: %+v, unread=%d, err=%v", u2Threads, u2Unread, err)
	}

	// Sender u1 archives the thread; it moves from inbox to archived.
	archived := true
	if _, err := s.PatchMessageThreadState(ctx, thr.ID, "u1", ThreadStatePatch{Archived: &archived, Now: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	u1Inbox, _, err := s.ListMessageThreads(ctx, "u1", MessageThreadFilter{Box: "inbox"})
	if err != nil || len(u1Inbox) != 0 {
		t.Fatalf("u1 inbox after archive: %+v, %v", u1Inbox, err)
	}
	u1Arch, _, err := s.ListMessageThreads(ctx, "u1", MessageThreadFilter{Box: "archived"})
	if err != nil || len(u1Arch) != 1 {
		t.Fatalf("u1 archived: %+v, %v", u1Arch, err)
	}

	// Recipient u2 replies to thread as a whole -> unarchives for u1, marks unread for u1, marks read for u2!
	reply, err := s.ReplyMessageThread(ctx, ReplyThreadParams{
		ThreadID: thr.ID,
		SenderID: "u2",
		Body:     "Counts are verified and updated.",
		Now:      t0.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.ParentID != "" {
		t.Fatalf("general thread reply parent = %q, want empty", reply.ParentID)
	}

	// Targeted reply to a specific message sets ParentID.
	targetReply, err := s.ReplyMessageThread(ctx, ReplyThreadParams{
		ThreadID: thr.ID,
		SenderID: "u1",
		ParentID: reply.ID,
		Body:     "Thanks Bob, appreciate the quick turnaround.",
		Now:      t0.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if targetReply.ParentID != reply.ID {
		t.Fatalf("targeted reply parent = %q, want %q", targetReply.ParentID, reply.ID)
	}

	// Reply referencing non-existent parent message fails with ErrNotFound.
	if _, err := s.ReplyMessageThread(ctx, ReplyThreadParams{
		ThreadID: thr.ID,
		SenderID: "u1",
		ParentID: "msg_nonexistent",
		Body:     "Should fail",
		Now:      t0.Add(4 * time.Minute),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reply with bogus parent = %v, want ErrNotFound", err)
	}

	// u1 now has 0 unread (replied); u2 has 1 unread.
	if n, _ := s.CountUnreadThreads(ctx, "u1"); n != 0 {
		t.Fatalf("u1 unread after replying = %d, want 0", n)
	}
	if n, _ := s.CountUnreadThreads(ctx, "u2"); n != 1 {
		t.Fatalf("u2 unread after u1 reply = %d, want 1", n)
	}

	// u2 marks it read, then marks it unread again.
	readTrue := true
	afterRead, err := s.PatchMessageThreadState(ctx, thr.ID, "u2", ThreadStatePatch{Read: &readTrue, Now: t0.Add(5 * time.Minute)})
	if err != nil || afterRead.Unread {
		t.Fatalf("after mark read: %+v, %v", afterRead, err)
	}
	readFalse := false
	afterUnread, err := s.PatchMessageThreadState(ctx, thr.ID, "u2", ThreadStatePatch{Read: &readFalse, Now: t0.Add(6 * time.Minute)})
	if err != nil || !afterUnread.Unread {
		t.Fatalf("after mark unread: %+v, %v", afterUnread, err)
	}

	// Search by message body and by participant username (both with and without @).
	foundBody, _, err := s.ListMessageThreads(ctx, "u1", MessageThreadFilter{Box: "inbox", Query: "verified"})
	if err != nil || len(foundBody) != 1 {
		t.Fatalf("search body: %+v, %v", foundBody, err)
	}
	foundUser, _, err := s.ListMessageThreads(ctx, "u1", MessageThreadFilter{Box: "inbox", Query: "bob"})
	if err != nil || len(foundUser) != 1 {
		t.Fatalf("search user: %+v, %v", foundUser, err)
	}
	foundAtUser, _, err := s.ListMessageThreads(ctx, "u1", MessageThreadFilter{Box: "inbox", Query: "@bob"})
	if err != nil || len(foundAtUser) != 1 {
		t.Fatalf("search @user: %+v, %v", foundAtUser, err)
	}
	none, _, err := s.ListMessageThreads(ctx, "u1", MessageThreadFilter{Box: "inbox", Query: "nonexistent-term"})
	if err != nil || len(none) != 0 {
		t.Fatalf("search none: %+v, %v", none, err)
	}

	// Self-messaging: user can create a direct thread to themselves (note to self).
	selfThr, err := s.CreateMessageThread(ctx, CreateThreadParams{
		Kind:         KindDirectThread,
		SenderID:     "u1",
		RecipientIDs: []string{"u1"},
		Subject:      "Personal task list",
		Body:         "Remember to deploy on Friday.",
		Now:          t0.Add(7 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create self thread: %v", err)
	}
	if len(selfThr.Participants) != 1 || selfThr.Participants[0].UserID != "u1" || selfThr.Unread {
		t.Fatalf("self thread: %+v", selfThr)
	}

	// Duplicate recipients in recipient list are cleanly deduplicated.
	dupThr, err := s.CreateMessageThread(ctx, CreateThreadParams{
		Kind:         KindDirectThread,
		SenderID:     "u1",
		RecipientIDs: []string{"u2", "u2", "u1"},
		Subject:      "Deduplication test",
		Body:         "Testing duplicate recipient IDs.",
		Now:          t0.Add(8 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create dup thread: %v", err)
	}
	if len(dupThr.Participants) != 2 {
		t.Fatalf("expected 2 distinct participants in dup thread, got %+v", dupThr.Participants)
	}
}

func TestWorkspaceMessageThreads(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	mkUser(t, s, "u1", "ann@example.com")
	mkUser(t, s, "u2", "bob@example.com")
	mkUser(t, s, "u3", "cara@example.com")
	mkUser(t, s, "u4", "dan@example.com")
	ws := mkWS(t, s, "Northwind Operations", "u1")
	if err := s.SetMembership(ctx, Membership{WorkspaceID: ws.ID, UserID: "u2", Role: RoleEditor}); err != nil {
		t.Fatal(err)
	}

	// Non-member u4 cannot create a workspace thread in ws.
	if _, err := s.CreateMessageThread(ctx, CreateThreadParams{
		Kind:        KindWorkspaceThread,
		WorkspaceID: ws.ID,
		SenderID:    "u4",
		Subject:     "Unauthorized",
		Body:        "Should fail",
		Now:         t0,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member CreateMessageThread = %v, want ErrNotFound", err)
	}

	// Member u1 creates a workspace thread.
	thr, err := s.CreateMessageThread(ctx, CreateThreadParams{
		Kind:        KindWorkspaceThread,
		WorkspaceID: ws.ID,
		SenderID:    "u1",
		Subject:     "Schema migration window",
		Body:        "We will add the new Suppliers table at 17:00 UTC.",
		Now:         t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if thr.WorkspaceID != ws.ID || thr.WorkspaceName != "Northwind Operations" {
		t.Fatalf("workspace thread metadata: %+v", thr)
	}

	// Member u2 sees it as unread in workspace box and filtered by workspace_id.
	wsThreads, unread, err := s.ListMessageThreads(ctx, "u2", MessageThreadFilter{Box: "workspace", WorkspaceID: ws.ID})
	if err != nil || unread != 1 || len(wsThreads) != 1 || wsThreads[0].ID != thr.ID {
		t.Fatalf("u2 workspace threads: %+v, unread=%d, err=%v", wsThreads, unread, err)
	}

	// u3 joins the workspace AFTER the thread was created and can immediately view and reply.
	if err := s.SetMembership(ctx, Membership{WorkspaceID: ws.ID, UserID: "u3", Role: RoleViewer}); err != nil {
		t.Fatal(err)
	}
	gotU3, err := s.GetMessageThread(ctx, thr.ID, "u3")
	if err != nil || !gotU3.Unread {
		t.Fatalf("late-joining member u3 GetMessageThread: %+v, %v", gotU3, err)
	}
	if _, err := s.ReplyMessageThread(ctx, ReplyThreadParams{
		ThreadID: thr.ID,
		SenderID: "u3",
		ParentID: thr.Messages[0].ID,
		Body:     "Thanks for the heads up!",
		Now:      t0.Add(time.Minute),
	}); err != nil {
		t.Fatalf("late-joining member u3 ReplyMessageThread: %v", err)
	}

	// Non-member u4 still cannot see the workspace thread.
	if _, err := s.GetMessageThread(ctx, thr.ID, "u4"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-member u4 GetMessageThread = %v, want ErrNotFound", err)
	}

	// When u3 is removed from the workspace, u3 immediately loses access to the thread.
	if err := s.DeleteMembership(ctx, ws.ID, "u3"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMessageThread(ctx, thr.ID, "u3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed member u3 GetMessageThread = %v, want ErrNotFound", err)
	}
}

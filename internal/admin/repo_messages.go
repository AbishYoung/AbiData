package admin

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"abidata/internal/dbx"
	"abidata/internal/ids"
)

// MessageThreadKind distinguishes direct user-to-user threads from workspace threads.
type MessageThreadKind string

const (
	KindDirectThread    MessageThreadKind = "direct"
	KindWorkspaceThread MessageThreadKind = "workspace"
)

// Limits for internal messaging.
const (
	MaxMessageSubjectRunes = 200
	MaxMessageBodyBytes    = 32 * 1024
	MaxDirectRecipients    = 50
	snippetMaxRunes        = 160
)

// MessageParticipant is a user participating in a thread.
type MessageParticipant struct {
	UserID      string
	Username    string
	DisplayName string
	Email       string
}

// Message is one message (initial or reply) inside a thread.
type Message struct {
	ID                ids.MessageID
	ThreadID          ids.ThreadID
	ParentID          ids.MessageID
	SenderID          string
	SenderUsername    string
	SenderDisplayName string
	Body              string
	CreatedAt         time.Time
}

// MessageThread is a thread summary or full detail (with Messages populated).
type MessageThread struct {
	ID                    ids.ThreadID
	Kind                  MessageThreadKind
	WorkspaceID           ids.WorkspaceID
	WorkspaceName         string
	Subject               string
	CreatedBy             string
	CreatedAt             time.Time
	LastMessageAt         time.Time
	MessageCount          int
	Unread                bool
	Archived              bool
	Participants          []MessageParticipant
	Snippet               string
	LastSenderID          string
	LastSenderUsername    string
	LastSenderDisplayName string
	Messages              []Message
}

// MessageThreadFilter selects which threads ListMessageThreads returns.
type MessageThreadFilter struct {
	Box         string // "inbox" (default), "unread", "workspace", "sent", "archived"
	WorkspaceID ids.WorkspaceID
	Query       string
}

// CreateThreadParams holds the inputs for creating a new thread and its initial message.
type CreateThreadParams struct {
	Kind         MessageThreadKind
	WorkspaceID  ids.WorkspaceID
	SenderID     string
	RecipientIDs []string
	Subject      string
	Body         string
	Now          time.Time
}

// ReplyThreadParams holds the inputs for appending a reply to an existing thread.
type ReplyThreadParams struct {
	ThreadID ids.ThreadID
	SenderID string
	ParentID ids.MessageID
	Body     string
	Now      time.Time
}

// ThreadStatePatch updates a caller's read and/or archived status on a thread.
type ThreadStatePatch struct {
	Read     *bool
	Archived *bool
	Now      time.Time
}

type rawThreadRow struct {
	thread   MessageThread
	lastRead *string
	archived *string
	sentCnt  int64
}

func scanThreadRow(r scanner, withSent bool) (rawThreadRow, error) {
	var out rawThreadRow
	var tid, kind, created, lastMsg string
	var wsID, wsName *string
	var msgCount int64
	dest := []any{
		&tid, &kind, &wsID, &wsName, &out.thread.Subject, &out.thread.CreatedBy,
		&created, &lastMsg, &msgCount, &out.lastRead, &out.archived,
	}
	if withSent {
		dest = append(dest, &out.sentCnt)
	}
	if err := r.Scan(dest...); err != nil {
		return rawThreadRow{}, err
	}
	out.thread.ID = ids.ThreadID(tid)
	out.thread.Kind = MessageThreadKind(kind)
	if wsID != nil {
		out.thread.WorkspaceID = ids.WorkspaceID(*wsID)
	}
	if wsName != nil {
		out.thread.WorkspaceName = *wsName
	}
	out.thread.MessageCount = int(msgCount)
	var err error
	if out.thread.CreatedAt, err = parseTime(created); err != nil {
		return rawThreadRow{}, err
	}
	if out.thread.LastMessageAt, err = parseTime(lastMsg); err != nil {
		return rawThreadRow{}, err
	}
	out.thread.Archived = out.archived != nil
	out.thread.Unread = out.lastRead == nil || *out.lastRead < lastMsg
	return out, nil
}

func makeSnippet(body string) string {
	collapsed := strings.Join(strings.Fields(body), " ")
	if utf8.RuneCountInString(collapsed) <= snippetMaxRunes {
		return collapsed
	}
	runes := []rune(collapsed)
	return string(runes[:snippetMaxRunes]) + "…"
}

func loadThreadSummaries(ctx context.Context, c dbx.Querier, threads []MessageThread) error {
	for i := range threads {
		t := &threads[i]
		pRows, err := c.Query(ctx, sqlLoadThreadParticipants.SQL, string(t.ID))
		if err != nil {
			return err
		}
		participants := []MessageParticipant{}
		for pRows.Next() {
			var p MessageParticipant
			var uname *string
			if err := pRows.Scan(&p.UserID, &uname, &p.DisplayName, &p.Email); err != nil {
				pRows.Close()
				return err
			}
			if uname != nil {
				p.Username = *uname
			}
			participants = append(participants, p)
		}
		err = pRows.Err()
		pRows.Close()
		if err != nil {
			return err
		}
		t.Participants = participants

		var lastMsgID, lastSenderID, lastDisplay, lastBody string
		var lastUname *string
		err = c.QueryRow(ctx, sqlLoadLastMessage.SQL, string(t.ID)).Scan(
			&lastMsgID, &lastSenderID, &lastUname, &lastDisplay, &lastBody,
		)
		if err != nil && !dbx.IsNoRows(err) {
			return err
		}
		if err == nil {
			t.LastSenderID = lastSenderID
			if lastUname != nil {
				t.LastSenderUsername = *lastUname
			}
			t.LastSenderDisplayName = lastDisplay
			t.Snippet = makeSnippet(lastBody)
		}
	}
	return nil
}

func loadThreadMessages(ctx context.Context, c dbx.Querier, threadID ids.ThreadID) ([]Message, error) {
	rows, err := c.Query(ctx, sqlListThreadMessages.SQL, string(threadID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var m Message
		var mid, tid, created string
		var parentID, uname *string
		if err := rows.Scan(&mid, &tid, &parentID, &m.SenderID, &uname, &m.SenderDisplayName, &m.Body, &created); err != nil {
			return nil, err
		}
		m.ID = ids.MessageID(mid)
		m.ThreadID = ids.ThreadID(tid)
		if parentID != nil {
			m.ParentID = ids.MessageID(*parentID)
		}
		if uname != nil {
			m.SenderUsername = *uname
		}
		if m.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMessageThread creates a direct or workspace thread, inserts its first
// message, and populates participant rows.
func (s *Store) CreateMessageThread(ctx context.Context, p CreateThreadParams) (MessageThread, error) {
	now := p.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowStr := dbx.FormatTime(now)
	tid := ids.NewThreadID()
	mid := ids.NewMessageID()

	err := s.Write(ctx, func(tx dbx.Tx) error {
		var wsArg any
		if p.Kind == KindWorkspaceThread {
			wsArg = string(p.WorkspaceID)
			// Verify the sender is a member of the active workspace.
			var role string
			if err := tx.QueryRow(ctx, sqlChangeMembership.SQL, string(p.WorkspaceID), p.SenderID).Scan(&role); err != nil {
				return notFoundIfNoRows(err, "membership")
			}
		}
		if _, err := tx.Exec(ctx, sqlCreateMessageThread.SQL,
			string(tid), string(p.Kind), wsArg, p.Subject, p.SenderID, nowStr, nowStr); err != nil {
			return wrapWrite(err, "create message thread")
		}
		if _, err := tx.Exec(ctx, sqlCreateMessage.SQL,
			string(mid), string(tid), nil, p.SenderID, p.Body, nowStr); err != nil {
			return wrapWrite(err, "create initial message")
		}
		if p.Kind == KindWorkspaceThread {
			if _, err := tx.Exec(ctx, sqlEnsureWorkspaceParticipants.SQL, string(tid), string(p.WorkspaceID)); err != nil {
				return err
			}
		} else {
			seen := map[string]bool{p.SenderID: true}
			for _, uid := range p.RecipientIDs {
				if uid == "" || seen[uid] {
					continue
				}
				seen[uid] = true
				if _, err := tx.Exec(ctx, sqlUpsertMessageParticipant.SQL, string(tid), uid, nil, nil); err != nil {
					return wrapWrite(err, "add recipient participant")
				}
			}
		}
		// Mark read for the sender.
		if _, err := tx.Exec(ctx, sqlUpsertMessageParticipant.SQL, string(tid), p.SenderID, nowStr, nil); err != nil {
			return wrapWrite(err, "upsert sender participant")
		}
		return nil
	})
	if err != nil {
		return MessageThread{}, err
	}
	return s.GetMessageThread(ctx, tid, p.SenderID)
}

// GetMessageThread returns a thread with its participants and chronological messages
// if userID is allowed to access it, or ErrNotFound otherwise.
func (s *Store) GetMessageThread(ctx context.Context, threadID ids.ThreadID, userID string) (MessageThread, error) {
	var out MessageThread
	err := s.Read(ctx, func(c dbx.Querier) error {
		raw, err := scanThreadRow(c.QueryRow(ctx, sqlGetMessageThread.SQL,
			dbx.Named{Name: "uid", Value: userID},
			dbx.Named{Name: "tid", Value: string(threadID)},
		), false)
		if err != nil {
			return err
		}
		threads := []MessageThread{raw.thread}
		if err := loadThreadSummaries(ctx, c, threads); err != nil {
			return err
		}
		msgs, err := loadThreadMessages(ctx, c, threadID)
		if err != nil {
			return err
		}
		out = threads[0]
		out.Messages = msgs
		return nil
	})
	if err != nil {
		return MessageThread{}, notFoundIfNoRows(err, "message thread")
	}
	return out, nil
}

// CountUnreadThreads returns the number of unarchived unread threads visible to userID.
func (s *Store) CountUnreadThreads(ctx context.Context, userID string) (int, error) {
	var n int64
	err := s.Read(ctx, func(c dbx.Querier) error {
		return c.QueryRow(ctx, sqlCountUnreadThreads.SQL, dbx.Named{Name: "uid", Value: userID}).Scan(&n)
	})
	return int(n), err
}

// ListMessageThreads returns threads matching f for userID, plus userID's total unread count.
func (s *Store) ListMessageThreads(ctx context.Context, userID string, f MessageThreadFilter) ([]MessageThread, int, error) {
	box := f.Box
	if box == "" {
		box = "inbox"
	}
	qLower := strings.ToLower(strings.TrimSpace(f.Query))
	qClean := strings.TrimPrefix(qLower, "@")
	likePattern := "%" + strings.NewReplacer(`\`, "", "%", "", "_", "").Replace(qClean) + "%"

	var matched []MessageThread
	var unreadCount int
	err := s.Read(ctx, func(c dbx.Querier) error {
		rows, err := c.Query(ctx, sqlListMessageThreads.SQL, dbx.Named{Name: "uid", Value: userID})
		if err != nil {
			return err
		}
		var rawRows []rawThreadRow
		for rows.Next() {
			r, err := scanThreadRow(rows, true)
			if err != nil {
				rows.Close()
				return err
			}
			rawRows = append(rawRows, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}

		candidates := make([]MessageThread, 0, len(rawRows))
		for _, r := range rawRows {
			if !r.thread.Archived && r.thread.Unread {
				unreadCount++
			}
			if f.WorkspaceID != "" && (r.thread.Kind != KindWorkspaceThread || r.thread.WorkspaceID != f.WorkspaceID) {
				continue
			}
			switch box {
			case "inbox":
				if r.thread.Archived {
					continue
				}
			case "unread":
				if r.thread.Archived || !r.thread.Unread {
					continue
				}
			case "workspace", "workspaces":
				if r.thread.Archived || r.thread.Kind != KindWorkspaceThread {
					continue
				}
			case "sent":
				if r.thread.CreatedBy != userID && r.sentCnt == 0 {
					continue
				}
			case "archived":
				if !r.thread.Archived {
					continue
				}
			}
			candidates = append(candidates, r.thread)
		}

		if err := loadThreadSummaries(ctx, c, candidates); err != nil {
			return err
		}

		if qLower == "" {
			matched = candidates
			return nil
		}
		matched = make([]MessageThread, 0, len(candidates))
		for _, t := range candidates {
			if threadMatchesMetadata(t, qLower) {
				matched = append(matched, t)
				continue
			}
			var n int64
			if err := c.QueryRow(ctx, sqlThreadMatchesBody.SQL, string(t.ID), likePattern).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				matched = append(matched, t)
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return matched, unreadCount, nil
}

func threadMatchesMetadata(t MessageThread, qLower string) bool {
	qClean := strings.TrimPrefix(qLower, "@")
	if strings.Contains(strings.ToLower(t.Subject), qLower) ||
		strings.Contains(strings.ToLower(t.WorkspaceName), qLower) ||
		strings.Contains(strings.ToLower(t.Snippet), qLower) {
		return true
	}
	return slices.ContainsFunc(t.Participants, func(p MessageParticipant) bool {
		return strings.Contains(strings.ToLower(p.Username), qClean) ||
			strings.Contains(strings.ToLower(p.DisplayName), qLower) ||
			strings.Contains(strings.ToLower(p.Email), qLower)
	})
}

// ReplyMessageThread appends a reply to a thread, bumps last_message_at and message_count,
// marks the thread read for the replier, and unarchives it for all participants.
func (s *Store) ReplyMessageThread(ctx context.Context, p ReplyThreadParams) (Message, error) {
	now := p.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	mid := ids.NewMessageID()
	var parentID ids.MessageID
	var replyAt time.Time

	err := s.Write(ctx, func(tx dbx.Tx) error {
		raw, err := scanThreadRow(tx.QueryRow(ctx, sqlGetMessageThread.SQL,
			dbx.Named{Name: "uid", Value: p.SenderID},
			dbx.Named{Name: "tid", Value: string(p.ThreadID)},
		), false)
		if err != nil {
			return notFoundIfNoRows(err, "message thread")
		}
		replyAt = now.UTC()
		if !replyAt.Truncate(time.Millisecond).After(raw.thread.LastMessageAt.Truncate(time.Millisecond)) {
			replyAt = raw.thread.LastMessageAt.Truncate(time.Millisecond).Add(time.Millisecond)
		}
		nowStr := dbx.FormatTime(replyAt)

		parentID = p.ParentID
		if parentID != "" {
			var cnt int64
			if err := tx.QueryRow(ctx, sqlCheckMessageInThread.SQL, string(parentID), string(p.ThreadID)).Scan(&cnt); err != nil {
				return err
			}
			if cnt == 0 {
				return fmt.Errorf("parent message: %w", ErrNotFound)
			}
		}

		var parentArg any
		if parentID != "" {
			parentArg = string(parentID)
		}
		if _, err := tx.Exec(ctx, sqlCreateMessage.SQL,
			string(mid), string(p.ThreadID), parentArg, p.SenderID, p.Body, nowStr); err != nil {
			return wrapWrite(err, "create reply")
		}
		if _, err := tx.Exec(ctx, sqlBumpMessageThread.SQL, nowStr, string(p.ThreadID)); err != nil {
			return err
		}
		if raw.thread.Kind == KindWorkspaceThread {
			if _, err := tx.Exec(ctx, sqlEnsureWorkspaceParticipants.SQL, string(p.ThreadID), string(raw.thread.WorkspaceID)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, sqlUnarchiveThreadAll.SQL, string(p.ThreadID)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sqlUpsertMessageParticipant.SQL, string(p.ThreadID), p.SenderID, nowStr, nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	u, err := s.GetUser(ctx, p.SenderID)
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID:                mid,
		ThreadID:          p.ThreadID,
		ParentID:          parentID,
		SenderID:          p.SenderID,
		SenderUsername:    u.Username,
		SenderDisplayName: u.DisplayName,
		Body:              p.Body,
		CreatedAt:         replyAt.Truncate(time.Millisecond),
	}, nil
}

// PatchMessageThreadState updates the caller's read and/or archived state on threadID.
func (s *Store) PatchMessageThreadState(ctx context.Context, threadID ids.ThreadID, userID string, p ThreadStatePatch) (MessageThread, error) {
	now := p.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	err := s.Write(ctx, func(tx dbx.Tx) error {
		raw, err := scanThreadRow(tx.QueryRow(ctx, sqlGetMessageThread.SQL,
			dbx.Named{Name: "uid", Value: userID},
			dbx.Named{Name: "tid", Value: string(threadID)},
		), false)
		if err != nil {
			return notFoundIfNoRows(err, "message thread")
		}
		if raw.thread.Kind == KindWorkspaceThread {
			if _, err := tx.Exec(ctx, sqlEnsureWorkspaceParticipants.SQL, string(threadID), string(raw.thread.WorkspaceID)); err != nil {
				return err
			}
		}
		var lastRead any
		if raw.lastRead != nil {
			lastRead = *raw.lastRead
		}
		if p.Read != nil {
			if *p.Read {
				readAt := now.UTC()
				if readAt.Before(raw.thread.LastMessageAt) {
					readAt = raw.thread.LastMessageAt
				}
				lastRead = dbx.FormatTime(readAt)
			} else {
				lastRead = nil
			}
		}
		var archived any
		if raw.archived != nil {
			archived = *raw.archived
		}
		if p.Archived != nil {
			if *p.Archived {
				archived = dbx.FormatTime(now)
			} else {
				archived = nil
			}
		}
		_, err = tx.Exec(ctx, sqlUpsertMessageParticipant.SQL, string(threadID), userID, lastRead, archived)
		return err
	})
	if err != nil {
		return MessageThread{}, err
	}
	return s.GetMessageThread(ctx, threadID, userID)
}

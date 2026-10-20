package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"abidata/internal/admin"
	"abidata/internal/auth"
	"abidata/internal/fault"
	"abidata/internal/ids"
)

// MessageParticipantDTO describes a user participating in a message thread.
type MessageParticipantDTO struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// MessageDTO is one message inside a thread.
type MessageDTO struct {
	ID                string    `json:"id"`
	ThreadID          string    `json:"thread_id"`
	ParentID          string    `json:"parent_id,omitempty"`
	SenderID          string    `json:"sender_id"`
	SenderUsername    string    `json:"sender_username"`
	SenderDisplayName string    `json:"sender_display_name"`
	Body              string    `json:"body"`
	CreatedAt         time.Time `json:"created_at"`
}

// MessageThreadSummaryDTO is a thread row in a mailbox list.
type MessageThreadSummaryDTO struct {
	ID                    string                  `json:"id"`
	Kind                  string                  `json:"kind" enum:"direct,workspace"`
	WorkspaceID           string                  `json:"workspace_id,omitempty"`
	WorkspaceName         string                  `json:"workspace_name,omitempty"`
	Subject               string                  `json:"subject"`
	CreatedBy             string                  `json:"created_by"`
	CreatedAt             time.Time               `json:"created_at"`
	LastMessageAt         time.Time               `json:"last_message_at"`
	MessageCount          int                     `json:"message_count"`
	Unread                bool                    `json:"unread"`
	Archived              bool                    `json:"archived"`
	Participants          []MessageParticipantDTO `json:"participants"`
	Snippet               string                  `json:"snippet"`
	LastSenderID          string                  `json:"last_sender_id"`
	LastSenderUsername    string                  `json:"last_sender_username"`
	LastSenderDisplayName string                  `json:"last_sender_display_name"`
}

// MessageThreadDetailDTO is a full thread with its chronological messages.
type MessageThreadDetailDTO struct {
	MessageThreadSummaryDTO
	Messages []MessageDTO `json:"messages"`
}

// MessageRecipientUserDTO is an active user entry in the messaging directory.
type MessageRecipientUserDTO struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// MessageRecipientWorkspaceDTO is a workspace the caller can message.
type MessageRecipientWorkspaceDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role" enum:"owner,builder,editor,viewer"`
}

func messageSummaryDTO(t admin.MessageThread) MessageThreadSummaryDTO {
	ps := make([]MessageParticipantDTO, 0, len(t.Participants))
	for _, p := range t.Participants {
		ps = append(ps, MessageParticipantDTO{
			ID:          p.UserID,
			Username:    p.Username,
			DisplayName: p.DisplayName,
			Email:       p.Email,
		})
	}
	return MessageThreadSummaryDTO{
		ID:                    string(t.ID),
		Kind:                  string(t.Kind),
		WorkspaceID:           string(t.WorkspaceID),
		WorkspaceName:         t.WorkspaceName,
		Subject:               t.Subject,
		CreatedBy:             t.CreatedBy,
		CreatedAt:             t.CreatedAt,
		LastMessageAt:         t.LastMessageAt,
		MessageCount:          t.MessageCount,
		Unread:                t.Unread,
		Archived:              t.Archived,
		Participants:          ps,
		Snippet:               t.Snippet,
		LastSenderID:          t.LastSenderID,
		LastSenderUsername:    t.LastSenderUsername,
		LastSenderDisplayName: t.LastSenderDisplayName,
	}
}

func messageDTO(m admin.Message) MessageDTO {
	return MessageDTO{
		ID:                string(m.ID),
		ThreadID:          string(m.ThreadID),
		ParentID:          string(m.ParentID),
		SenderID:          m.SenderID,
		SenderUsername:    m.SenderUsername,
		SenderDisplayName: m.SenderDisplayName,
		Body:              m.Body,
		CreatedAt:         m.CreatedAt,
	}
}

func messageDetailDTO(t admin.MessageThread) MessageThreadDetailDTO {
	msgs := make([]MessageDTO, 0, len(t.Messages))
	for _, m := range t.Messages {
		msgs = append(msgs, messageDTO(m))
	}
	return MessageThreadDetailDTO{
		MessageThreadSummaryDTO: messageSummaryDTO(t),
		Messages:                msgs,
	}
}

func (s *Server) resolveRecipientUser(ctx context.Context, raw string) (admin.User, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return admin.User{}, fault.New(fault.BadRequest, "Recipient cannot be empty.")
	}
	lookup := strings.TrimPrefix(trimmed, "@")
	if u, err := s.deps.Admin.GetUser(ctx, lookup); err == nil {
		if u.Disabled {
			return admin.User{}, fault.New(fault.BadRequest, fmt.Sprintf("Recipient %q is disabled.", raw))
		}
		return u, nil
	} else if !errors.Is(err, admin.ErrNotFound) {
		return admin.User{}, err
	}
	if u, err := s.deps.Admin.GetUserByUsername(ctx, lookup); err == nil {
		if u.Disabled {
			return admin.User{}, fault.New(fault.BadRequest, fmt.Sprintf("Recipient %q is disabled.", raw))
		}
		return u, nil
	} else if !errors.Is(err, admin.ErrNotFound) {
		return admin.User{}, err
	}
	if u, err := s.deps.Admin.GetUserByEmail(ctx, trimmed); err == nil {
		if u.Disabled {
			return admin.User{}, fault.New(fault.BadRequest, fmt.Sprintf("Recipient %q is disabled.", raw))
		}
		return u, nil
	} else if !errors.Is(err, admin.ErrNotFound) {
		return admin.User{}, err
	}
	return admin.User{}, fault.New(fault.BadRequest, fmt.Sprintf("No active user matches %q.", raw))
}

func (s *Server) registerMessages(api huma.API) {
	tags := []string{"messages"}
	sessionOnly := map[string]any{metaSessionOnly: true}

	huma.Register(api, huma.Operation{
		OperationID: "listMessageRecipients", Method: http.MethodGet, Path: "/api/v1/messages/recipients", Tags: tags, Metadata: sessionOnly,
		Summary: "Directory of active users and the caller's workspaces for composing messages", Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Users      []MessageRecipientUserDTO      `json:"users"`
			Workspaces []MessageRecipientWorkspaceDTO `json:"workspaces"`
		}
	}, error) {
		p, _ := PrincipalFrom(ctx)
		users, err := s.deps.Admin.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		acc, err := s.deps.Admin.ListWorkspacesForUser(ctx, p.UserID)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Users      []MessageRecipientUserDTO      `json:"users"`
				Workspaces []MessageRecipientWorkspaceDTO `json:"workspaces"`
			}
		}{}
		out.Body.Users = make([]MessageRecipientUserDTO, 0, len(users))
		for _, u := range users {
			if u.Disabled {
				continue
			}
			out.Body.Users = append(out.Body.Users, MessageRecipientUserDTO{
				ID:          u.ID,
				Username:    u.Username,
				DisplayName: u.DisplayName,
				Email:       u.Email,
			})
		}
		out.Body.Workspaces = make([]MessageRecipientWorkspaceDTO, 0, len(acc))
		for _, a := range acc {
			if a.Workspace.SystemKind != "" || a.Workspace.Status != admin.StatusActive {
				continue
			}
			out.Body.Workspaces = append(out.Body.Workspaces, MessageRecipientWorkspaceDTO{
				ID:   string(a.Workspace.ID),
				Name: a.Workspace.Name,
				Role: string(a.Role),
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getUnreadMessageCount", Method: http.MethodGet, Path: "/api/v1/messages/unread-count", Tags: tags, Metadata: sessionOnly,
		Summary: "Count of unarchived unread message threads for the signed-in user", Errors: []int{401, 403},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			UnreadCount int `json:"unread_count"`
		}
	}, error) {
		p, _ := PrincipalFrom(ctx)
		n, err := s.deps.Admin.CountUnreadThreads(ctx, p.UserID)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				UnreadCount int `json:"unread_count"`
			}
		}{}
		out.Body.UnreadCount = n
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listMessageThreads", Method: http.MethodGet, Path: "/api/v1/messages/threads", Tags: tags, Metadata: sessionOnly,
		Summary: "List message threads for the signed-in user", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		Box         string `query:"box" enum:"inbox,unread,workspace,sent,archived," doc:"Mailbox filter; defaults to inbox."`
		WorkspaceID string `query:"workspace_id" maxLength:"64" doc:"Filter to a single workspace's threads."`
		Q           string `query:"q" maxLength:"200" doc:"Search subject, message body or participants."`
	}) (*struct {
		Body struct {
			Items       []MessageThreadSummaryDTO `json:"items"`
			UnreadCount int                       `json:"unread_count"`
		}
	}, error) {
		p, _ := PrincipalFrom(ctx)
		var wsID ids.WorkspaceID
		if in.WorkspaceID != "" {
			m, err := s.resolveMembership(ctx, p, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			wsID = m.Workspace.ID
		}
		threads, unread, err := s.deps.Admin.ListMessageThreads(ctx, p.UserID, admin.MessageThreadFilter{
			Box:         in.Box,
			WorkspaceID: wsID,
			Query:       in.Q,
		})
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Items       []MessageThreadSummaryDTO `json:"items"`
				UnreadCount int                       `json:"unread_count"`
			}
		}{}
		out.Body.Items = make([]MessageThreadSummaryDTO, 0, len(threads))
		for _, t := range threads {
			out.Body.Items = append(out.Body.Items, messageSummaryDTO(t))
		}
		out.Body.UnreadCount = unread
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "createMessageThread", Method: http.MethodPost, Path: "/api/v1/messages/threads", Tags: tags, Metadata: sessionOnly,
		Summary: "Start a new direct or workspace message thread", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Kind        string   `json:"kind" enum:"direct,workspace"`
			WorkspaceID string   `json:"workspace_id,omitempty" maxLength:"64"`
			Recipients  []string `json:"recipients,omitempty" maxItems:"50" doc:"User IDs, usernames or emails (for direct threads)."`
			Subject     string   `json:"subject" maxLength:"400"`
			Body        string   `json:"body" maxLength:"32768"`
		}
	}) (*struct{ Body MessageThreadDetailDTO }, error) {
		p, _ := PrincipalFrom(ctx)
		subject := strings.TrimSpace(in.Body.Subject)
		if subject == "" {
			return nil, fault.New(fault.BadRequest, "Subject is required.")
		}
		if utf8.RuneCountInString(subject) > admin.MaxMessageSubjectRunes {
			return nil, fault.New(fault.BadRequest, fmt.Sprintf("Subject must be at most %d characters.", admin.MaxMessageSubjectRunes))
		}
		body := strings.TrimSpace(in.Body.Body)
		if body == "" {
			return nil, fault.New(fault.BadRequest, "Message body is required.")
		}
		if len(body) > admin.MaxMessageBodyBytes {
			return nil, fault.New(fault.BadRequest, "Message body is too long.")
		}

		params := admin.CreateThreadParams{
			SenderID: p.UserID,
			Subject:  subject,
			Body:     body,
		}
		switch admin.MessageThreadKind(in.Body.Kind) {
		case admin.KindDirectThread:
			params.Kind = admin.KindDirectThread
			if len(in.Body.Recipients) == 0 {
				return nil, fault.New(fault.BadRequest, "Select at least one recipient.")
			}
			if len(in.Body.Recipients) > admin.MaxDirectRecipients {
				return nil, fault.New(fault.BadRequest, "Too many recipients.")
			}
			recipientIDs := make([]string, 0, len(in.Body.Recipients))
			for _, r := range in.Body.Recipients {
				u, err := s.resolveRecipientUser(ctx, r)
				if err != nil {
					return nil, err
				}
				recipientIDs = append(recipientIDs, u.ID)
			}
			params.RecipientIDs = recipientIDs
		case admin.KindWorkspaceThread:
			params.Kind = admin.KindWorkspaceThread
			if strings.TrimSpace(in.Body.WorkspaceID) == "" {
				return nil, fault.New(fault.BadRequest, "Select a workspace.")
			}
			m, err := s.resolveMembership(ctx, p, in.Body.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if m.Workspace.SystemKind != "" {
				return nil, fault.New(fault.BadRequest, "System workspaces do not support workspace message threads.")
			}
			params.WorkspaceID = m.Workspace.ID
		default:
			return nil, fault.New(fault.BadRequest, "Thread kind must be 'direct' or 'workspace'.")
		}

		thr, err := s.deps.Admin.CreateMessageThread(ctx, params)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		return &struct{ Body MessageThreadDetailDTO }{Body: messageDetailDTO(thr)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getMessageThread", Method: http.MethodGet, Path: "/api/v1/messages/threads/{threadId}", Tags: tags, Metadata: sessionOnly,
		Summary: "Get a message thread with all its messages", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *struct {
		ThreadID string `path:"threadId" maxLength:"64"`
	}) (*struct{ Body MessageThreadDetailDTO }, error) {
		p, _ := PrincipalFrom(ctx)
		tid, err := s.parseAndAuthorizeThread(ctx, p, in.ThreadID)
		if err != nil {
			return nil, err
		}
		thr, err := s.deps.Admin.GetMessageThread(ctx, tid, p.UserID)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		return &struct{ Body MessageThreadDetailDTO }{Body: messageDetailDTO(thr)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "replyMessageThread", Method: http.MethodPost, Path: "/api/v1/messages/threads/{threadId}/replies", Tags: tags, Metadata: sessionOnly,
		Summary: "Reply to a message thread", DefaultStatus: http.StatusCreated, Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		ThreadID string `path:"threadId" maxLength:"64"`
		Body     struct {
			ParentID string `json:"parent_id,omitempty" maxLength:"64" doc:"Optional message ID being replied to; defaults to the latest message."`
			Body     string `json:"body" maxLength:"32768"`
		}
	}) (*struct{ Body MessageThreadDetailDTO }, error) {
		p, _ := PrincipalFrom(ctx)
		tid, err := s.parseAndAuthorizeThread(ctx, p, in.ThreadID)
		if err != nil {
			return nil, err
		}
		body := strings.TrimSpace(in.Body.Body)
		if body == "" {
			return nil, fault.New(fault.BadRequest, "Reply body is required.")
		}
		if len(body) > admin.MaxMessageBodyBytes {
			return nil, fault.New(fault.BadRequest, "Reply body is too long.")
		}
		var parentID ids.MessageID
		if strings.TrimSpace(in.Body.ParentID) != "" {
			pid, err := ids.ParseMessageID(strings.TrimSpace(in.Body.ParentID))
			if err != nil {
				return nil, fault.New(fault.BadRequest, "Invalid parent message ID.")
			}
			parentID = pid
		}
		if _, err := s.deps.Admin.ReplyMessageThread(ctx, admin.ReplyThreadParams{
			ThreadID: tid,
			SenderID: p.UserID,
			ParentID: parentID,
			Body:     body,
		}); err != nil {
			if errors.Is(err, admin.ErrNotFound) && parentID != "" {
				return nil, fault.New(fault.BadRequest, "Parent message does not belong to this thread.")
			}
			return nil, mapAdminErr(err)
		}
		thr, err := s.deps.Admin.GetMessageThread(ctx, tid, p.UserID)
		if err != nil {
			return nil, mapAdminErr(err)
		}
		return &struct{ Body MessageThreadDetailDTO }{Body: messageDetailDTO(thr)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "patchMessageThread", Method: http.MethodPatch, Path: "/api/v1/messages/threads/{threadId}", Tags: tags, Metadata: sessionOnly,
		Summary: "Mark a thread read/unread or archive/unarchive it", Errors: []int{400, 401, 403, 404},
	}, func(ctx context.Context, in *struct {
		ThreadID string `path:"threadId" maxLength:"64"`
		Body     struct {
			Read     *bool `json:"read,omitempty"`
			Archived *bool `json:"archived,omitempty"`
		}
	}) (*struct{ Body MessageThreadDetailDTO }, error) {
		p, _ := PrincipalFrom(ctx)
		tid, err := s.parseAndAuthorizeThread(ctx, p, in.ThreadID)
		if err != nil {
			return nil, err
		}
		if in.Body.Read == nil && in.Body.Archived == nil {
			return nil, fault.New(fault.BadRequest, "Specify read and/or archived.")
		}
		thr, err := s.deps.Admin.PatchMessageThreadState(ctx, tid, p.UserID, admin.ThreadStatePatch{
			Read:     in.Body.Read,
			Archived: in.Body.Archived,
		})
		if err != nil {
			return nil, mapAdminErr(err)
		}
		return &struct{ Body MessageThreadDetailDTO }{Body: messageDetailDTO(thr)}, nil
	})
}

func (s *Server) parseAndAuthorizeThread(ctx context.Context, _ auth.Principal, raw string) (ids.ThreadID, error) {
	tid, err := ids.ParseThreadID(raw)
	if err != nil {
		return "", errNotFound
	}
	return tid, nil
}

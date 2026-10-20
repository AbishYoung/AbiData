import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type MessageParticipant = components["schemas"]["MessageParticipantDTO"];
export type MessageItem = components["schemas"]["MessageDTO"];
export type MessageThreadSummary = components["schemas"]["MessageThreadSummaryDTO"];
export type MessageThreadDetail = components["schemas"]["MessageThreadDetailDTO"];
export type MessageRecipientUser = components["schemas"]["MessageRecipientUserDTO"];
export type MessageRecipientWorkspace = components["schemas"]["MessageRecipientWorkspaceDTO"];

export type MailboxKind = "inbox" | "unread" | "workspace" | "sent" | "archived";

export interface MessageThreadListResult {
  items: MessageThreadSummary[];
  unread_count: number;
}

export interface MessageRecipientsDirectory {
  users: MessageRecipientUser[];
  workspaces: MessageRecipientWorkspace[];
}

export interface CreateThreadInput {
  kind: "direct" | "workspace";
  workspace_id?: string;
  recipients?: string[];
  subject: string;
  body: string;
}

export const messageKeys = {
  all: ["messages"] as const,
  unreadCount: ["messages", "unread-count"] as const,
  recipients: ["messages", "recipients"] as const,
  threads: (box: MailboxKind, workspaceId: string, q: string) =>
    ["messages", "threads", box, workspaceId, q] as const,
  thread: (threadId: string) => ["messages", "thread", threadId] as const,
};

export async function getUnreadMessageCount(): Promise<number> {
  const res = await api.requestData(
    (c, init) => c.GET("/api/v1/messages/unread-count", init),
    { label: "Checking messages", class: "background" },
  );
  return res.unread_count;
}

export async function listMessageRecipients(): Promise<MessageRecipientsDirectory> {
  const res = await api.requestData(
    (c, init) => c.GET("/api/v1/messages/recipients", init),
    { label: "Loading recipients" },
  );
  return {
    users: res.users ?? [],
    workspaces: res.workspaces ?? [],
  };
}

export async function listMessageThreads(
  box: MailboxKind,
  workspaceId = "",
  q = "",
): Promise<MessageThreadListResult> {
  const res = await api.requestData(
    (c, init) =>
      c.GET("/api/v1/messages/threads", {
        ...init,
        params: {
          query: {
            box,
            ...(workspaceId ? { workspace_id: workspaceId } : {}),
            ...(q ? { q } : {}),
          },
        },
      }),
    { label: "Loading messages" },
  );
  return {
    items: res.items ?? [],
    unread_count: res.unread_count,
  };
}

export function getMessageThread(threadId: string): Promise<MessageThreadDetail> {
  return api.requestData(
    (c, init) =>
      c.GET("/api/v1/messages/threads/{threadId}", {
        ...init,
        params: { path: { threadId } },
      }),
    { label: "Loading conversation" },
  );
}

export function createMessageThread(body: CreateThreadInput): Promise<MessageThreadDetail> {
  return api.requestData(
    (c, init) => c.POST("/api/v1/messages/threads", { ...init, body }),
    { label: "Sending message" },
  );
}

export function replyMessageThread(
  threadId: string,
  body: { body: string; parent_id?: string },
): Promise<MessageThreadDetail> {
  return api.requestData(
    (c, init) =>
      c.POST("/api/v1/messages/threads/{threadId}/replies", {
        ...init,
        params: { path: { threadId } },
        body,
      }),
    { label: "Sending reply" },
  );
}

export function patchMessageThread(
  threadId: string,
  body: { read?: boolean; archived?: boolean },
): Promise<MessageThreadDetail> {
  return api.requestData(
    (c, init) =>
      c.PATCH("/api/v1/messages/threads/{threadId}", {
        ...init,
        params: { path: { threadId } },
        body,
      }),
    { label: "Updating thread" },
  );
}

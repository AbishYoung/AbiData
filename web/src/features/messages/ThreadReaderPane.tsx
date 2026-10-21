import { useEffect, useRef, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArchiveRestore,
  ArrowLeft,
  Building2,
  CornerUpLeft,
  Mail,
  MailOpen,
  Send,
  X,
} from "lucide-react";
import { Badge, Button, Field, Skeleton, Textarea } from "@/components/ui";
import { Tip } from "@/components/Tip";
import { alertBox, card } from "@/components/styles";
import {
  getMessageThread,
  messageKeys,
  patchMessageThread,
  replyMessageThread,
  type MessageItem,
} from "./messagesApi";
import { formatTimestamp } from "./ThreadListRow";
import { MarkdownBody } from "./MarkdownBody";

function initialsOf(name: string): string {
  const parts = name.split(/[\s@.]+/).filter(Boolean);
  return (
    parts
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase())
      .join("") || "?"
  ).slice(0, 2);
}

export function ThreadReaderPane({
  threadId,
  compact,
  onBack,
  onUpdated,
}: {
  threadId: string;
  compact: boolean;
  onBack: () => void;
  onUpdated: () => void;
}) {
  const queryClient = useQueryClient();
  const [replyBody, setReplyBody] = useState("");
  const [replyToMsg, setReplyToMsg] = useState<MessageItem | null>(null);
  const [replyError, setReplyError] = useState<string | null>(null);
  const autoMarkedRef = useRef<string | null>(null);
  const replyInputRef = useRef<HTMLTextAreaElement>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const detailQuery = useQuery({
    queryKey: messageKeys.thread(threadId),
    queryFn: () => getMessageThread(threadId),
  });

  const patchMutation = useMutation({
    mutationFn: (body: { read?: boolean; archived?: boolean }) =>
      patchMessageThread(threadId, body),
    onSuccess: (updated) => {
      queryClient.setQueryData(messageKeys.thread(threadId), updated);
      onUpdated();
    },
  });

  // Automatically mark unread thread as read when opened.
  useEffect(() => {
    if (
      detailQuery.data?.unread &&
      autoMarkedRef.current !== detailQuery.data.id
    ) {
      autoMarkedRef.current = detailQuery.data.id;
      patchMutation.mutate({ read: true });
    }
  }, [detailQuery.data?.id, detailQuery.data?.unread, patchMutation]);

  const replyMutation = useMutation({
    mutationFn: () =>
      replyMessageThread(threadId, {
        body: replyBody.trim(),
        ...(replyToMsg ? { parent_id: replyToMsg.id } : {}),
      }),
    onSuccess: (updated) => {
      setReplyBody("");
      setReplyToMsg(null);
      setReplyError(null);
      queryClient.setQueryData(messageKeys.thread(threadId), updated);
      onUpdated();
      setTimeout(() => {
        if (typeof messagesEndRef.current?.scrollIntoView === "function") {
          messagesEndRef.current.scrollIntoView({ behavior: "smooth" });
        }
      }, 50);
    },
    onError: (err) => {
      setReplyError(err instanceof Error ? err.message : "Could not send reply.");
    },
  });

  function handleReplySubmit(e?: FormEvent) {
    e?.preventDefault();
    if (!replyBody.trim()) {
      setReplyError("Write a reply before sending.");
      return;
    }
    setReplyError(null);
    replyMutation.mutate();
  }

  if (detailQuery.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4`}>
        {detailQuery.error instanceof Error
          ? detailQuery.error.message
          : "Could not load conversation."}
      </div>
    );
  }
  if (!detailQuery.data) {
    return <Skeleton rows={8} label="Loading conversation…" />;
  }

  const thread = detailQuery.data;
  const threadMessages = thread.messages ?? [];
  const threadParticipants = thread.participants ?? [];
  const messagesById = new Map(threadMessages.map((m) => [m.id, m]));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border bg-surface px-4 py-3">
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            {compact && (
              <Button size="sm" variant="ghost" onClick={onBack}>
                <ArrowLeft size={14} aria-hidden="true" />
                Back to threads
              </Button>
            )}
            <h2 className="m-0 text-base font-semibold text-fg">
              {thread.subject}
            </h2>
            {thread.kind === "workspace" ? (
              <Badge tone="accent">
                <Building2 size={12} aria-hidden="true" />
                Workspace: {thread.workspace_name || thread.workspace_id}
              </Badge>
            ) : (
              <Badge tone="neutral">Direct</Badge>
            )}
            {thread.archived && <Badge tone="warning">Archived</Badge>}
          </div>
          <div className="flex flex-wrap items-center gap-1.5 text-xs text-fg-muted">
            <span className="font-medium">Participants:</span>
            {threadParticipants.map((p) => (
              <span
                key={p.id}
                className="inline-flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 text-xs text-fg"
              >
                <span>{p.display_name || p.email}</span>
                {p.username && (
                  <span className="text-fg-muted">@{p.username}</span>
                )}
              </span>
            ))}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-1.5">
          <Tip label={thread.unread ? "Mark as read" : "Mark as unread"}>
            <Button
              size="sm"
              variant="secondary"
              disabled={patchMutation.isPending}
              onClick={() => patchMutation.mutate({ read: thread.unread })}
            >
              {thread.unread ? (
                <>
                  <MailOpen size={14} aria-hidden="true" />
                  Mark read
                </>
              ) : (
                <>
                  <Mail size={14} aria-hidden="true" />
                  Mark unread
                </>
              )}
            </Button>
          </Tip>
          <Tip label={thread.archived ? "Restore conversation to inbox" : "Archive conversation"}>
            <Button
              size="sm"
              variant="secondary"
              disabled={patchMutation.isPending}
              onClick={() => patchMutation.mutate({ archived: !thread.archived })}
            >
              {thread.archived ? (
                <>
                  <ArchiveRestore size={14} aria-hidden="true" />
                  Unarchive
                </>
              ) : (
                <>
                  <Archive size={14} aria-hidden="true" />
                  Archive
                </>
              )}
            </Button>
          </Tip>
        </div>
      </div>

      <ol
        aria-label="Messages in thread"
        className="m-0 min-h-0 flex-1 space-y-3 overflow-y-auto list-none p-4"
      >
        {threadMessages.map((m, idx) => {
          const parent = m.parent_id ? messagesById.get(m.parent_id) : undefined;
          const senderName = m.sender_display_name || m.sender_username || m.sender_id;
          return (
            <li key={m.id} className={`${card} p-4`}>
              <div className="flex items-center justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2.5">
                  <span
                    aria-hidden="true"
                    className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold text-accent"
                  >
                    {initialsOf(senderName)}
                  </span>
                  <div className="min-w-0">
                    <span className="text-sm font-semibold text-fg">
                      {senderName}
                    </span>
                    {m.sender_username && (
                      <span className="ml-1.5 text-xs text-fg-muted">
                        @{m.sender_username}
                      </span>
                    )}
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <span className="text-xs text-fg-subtle">
                    {formatTimestamp(m.created_at)}
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={`Reply to message ${idx + 1} from ${senderName}`}
                    onClick={() => {
                      setReplyToMsg(m);
                      replyInputRef.current?.focus();
                    }}
                  >
                    <CornerUpLeft size={13} aria-hidden="true" />
                    Reply
                  </Button>
                </div>
              </div>

              {parent && (
                <div className="mt-2 flex items-center gap-1.5 rounded-md border-l-2 border-accent bg-surface-2/70 px-2.5 py-1 text-xs text-fg-muted">
                  <CornerUpLeft size={12} aria-hidden="true" className="shrink-0" />
                  <span className="truncate">
                    In reply to{" "}
                    <strong className="font-medium text-fg">
                      {parent.sender_display_name || `@${parent.sender_username}`}
                    </strong>
                    : “{parent.body.slice(0, 80)}”
                  </span>
                </div>
              )}

              <div className="mt-2.5 text-sm leading-relaxed text-fg">
                <MarkdownBody content={m.body} workspaceId={thread.workspace_id} />
              </div>
            </li>
          );
        })}
        <div ref={messagesEndRef} aria-hidden="true" />
      </ol>

      <form
        onSubmit={handleReplySubmit}
        className="border-t border-border bg-surface p-4 space-y-2.5"
      >
        {replyToMsg && (
          <div className="flex items-center justify-between gap-2 rounded-md bg-surface-2 px-3 py-1.5 text-xs text-fg-muted">
            <span className="truncate">
              Replying to{" "}
              <strong className="font-medium text-fg">
                {replyToMsg.sender_display_name || `@${replyToMsg.sender_username}`}
              </strong>
              : “{replyToMsg.body.slice(0, 60)}”
            </span>
            <Tip label="Clear reply target">
              <button
                type="button"
                aria-label="Cancel specific reply target"
                onClick={() => setReplyToMsg(null)}
                className="inline-flex h-5 w-5 items-center justify-center rounded hover:bg-surface-3 focus-visible:outline-focus"
              >
                <X size={12} aria-hidden="true" />
              </button>
            </Tip>
          </div>
        )}

        <Field label="Reply to thread" error={replyError} hint="Press Ctrl+Enter or ⌘+Enter to send">
          <Textarea
            ref={replyInputRef}
            rows={3}
            value={replyBody}
            onChange={(e) => setReplyBody(e.target.value)}
            placeholder="Write a reply…"
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault();
                handleReplySubmit();
              }
            }}
          />
        </Field>
        <div className="flex justify-end">
          <Button
            type="submit"
            variant="primary"
            disabled={replyMutation.isPending}
          >
            <Send size={15} aria-hidden="true" />
            Send reply
          </Button>
        </div>
      </form>
    </div>
  );
}

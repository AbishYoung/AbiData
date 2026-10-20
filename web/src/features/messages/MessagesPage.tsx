import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArchiveRestore,
  ArrowLeft,
  Building2,
  ChevronRight,
  CornerUpLeft,
  Inbox,
  Mail,
  MailOpen,
  MailPlus,
  MessageSquare,
  Search,
  Send,
  Users,
  X,
} from "lucide-react";
import type { Me } from "@/lib/session";
import { useIsCompact } from "@/lib/useMediaQuery";
import { LogoMark } from "@/components/brand/Logo";
import { WorkspaceAvatar } from "@/components/WorkspaceAvatar";
import { HelpButton } from "@/features/docs/HelpButton";
import { UserMenu } from "@/features/nav/UserMenu";
import {
  Badge,
  Button,
  EmptyState,
  Field,
  Select,
  Skeleton,
  Textarea,
} from "@/components/ui";
import { alertBox, card, input } from "@/components/styles";
import {
  getMessageThread,
  listMessageThreads,
  messageKeys,
  patchMessageThread,
  replyMessageThread,
  type MailboxKind,
  type MessageItem,
  type MessageParticipant,
  type MessageThreadSummary,
} from "./messagesApi";
import { ComposeMessageDialog } from "./ComposeMessageDialog";

const BOXES: { id: MailboxKind; label: string }[] = [
  { id: "inbox", label: "Inbox" },
  { id: "unread", label: "Unread" },
  { id: "workspace", label: "Workspaces" },
  { id: "sent", label: "Sent" },
  { id: "archived", label: "Archived" },
];

export function parseMailbox(raw: unknown): MailboxKind {
  if (
    raw === "inbox" ||
    raw === "unread" ||
    raw === "workspace" ||
    raw === "sent" ||
    raw === "archived"
  ) {
    return raw;
  }
  return "inbox";
}

function initialsOf(name: string): string {
  const parts = name.split(/[\s@.]+/).filter(Boolean);
  return (
    parts
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase())
      .join("") || "?"
  ).slice(0, 2);
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatParticipantSummary(
  participants: MessageParticipant[],
  myUserId: string,
): string {
  const others = participants.filter((p) => p.id !== myUserId);
  const target = others.length > 0 ? others : participants;
  return target
    .map((p) => (p.username ? `@${p.username}` : p.display_name || p.email))
    .join(", ");
}

export function MessagesPage({
  me,
  fixedWorkspaceId,
  initialBox = "inbox",
  initialWorkspaceId = "",
  initialThreadId = "",
}: {
  me: Me;
  fixedWorkspaceId?: string;
  initialBox?: MailboxKind;
  initialWorkspaceId?: string;
  initialThreadId?: string;
}) {
  const compact = useIsCompact();
  const queryClient = useQueryClient();
  const [box, setBox] = useState<MailboxKind>(initialBox);
  const [workspaceFilter, setWorkspaceFilter] = useState<string>(
    fixedWorkspaceId ?? initialWorkspaceId,
  );
  const [search, setSearch] = useState("");
  const [selectedThreadId, setSelectedThreadId] = useState<string>(initialThreadId);
  const [composing, setComposing] = useState(false);

  const effectiveWs = fixedWorkspaceId ?? workspaceFilter;
  const activeWorkspaces = (me.memberships ?? []).filter(
    (m) => !m.system_kind && m.status === "active",
  );
  const fixedWs = fixedWorkspaceId
    ? activeWorkspaces.find((m) => m.workspace_id === fixedWorkspaceId)
    : undefined;

  const threadsQuery = useQuery({
    queryKey: messageKeys.threads(box, effectiveWs, search.trim()),
    queryFn: () => listMessageThreads(box, effectiveWs, search.trim()),
  });

  const threads = threadsQuery.data?.items ?? [];
  const unreadCount = threadsQuery.data?.unread_count ?? 0;

  // On desktop, auto-select the first thread when none is selected.
  const activeThreadId =
    selectedThreadId || (!compact && threads[0] ? threads[0].id : "");

  return (
    <div className="flex h-full flex-col bg-bg">
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-surface px-3">
        <Link
          to="/"
          className="group inline-flex items-center gap-2 rounded-md px-1 py-1 text-sm font-medium text-fg-muted hover:text-fg"
        >
          <LogoMark size={24} />
          <span className="max-sm:hidden">Workspaces</span>
        </Link>
        {fixedWorkspaceId && (
          <>
            <ChevronRight size={14} aria-hidden="true" className="text-fg-subtle" />
            <Link
              to="/w/$ws"
              params={{ ws: fixedWorkspaceId }}
              className="inline-flex items-center gap-2 rounded-md px-1.5 py-1 text-sm font-medium text-fg-muted hover:bg-surface-2 hover:text-fg"
            >
              {fixedWs && <WorkspaceAvatar name={fixedWs.workspace_name} />}
              <span className="max-w-44 truncate">
                {fixedWs?.workspace_name ?? "Workspace"}
              </span>
            </Link>
          </>
        )}
        <ChevronRight size={14} aria-hidden="true" className="text-fg-subtle" />
        <div className="flex items-center gap-2">
          <Mail size={16} aria-hidden="true" className="text-accent" />
          <h1 className="m-0 text-sm font-semibold text-fg">Messages</h1>
          {unreadCount > 0 && <Badge tone="accent">{unreadCount} unread</Badge>}
        </div>
        <div className="ml-auto flex items-center gap-1.5">
          <Button
            variant="primary"
            size="sm"
            onClick={() => setComposing(true)}
          >
            <MailPlus size={15} aria-hidden="true" />
            New message
          </Button>
          {!compact && <HelpButton />}
          <UserMenu me={me} />
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        {(!compact || !activeThreadId) && (
          <aside
            aria-label="Message threads"
            className="flex w-full shrink-0 flex-col border-r border-border bg-surface md:w-88 lg:w-96"
          >
            <div className="space-y-2 border-b border-border p-3">
              <div
                role="tablist"
                aria-label="Mailbox filter"
                className="flex flex-wrap gap-1"
              >
                {BOXES.map((b) => {
                  const active = box === b.id;
                  return (
                    <button
                      key={b.id}
                      type="button"
                      role="tab"
                      aria-selected={active}
                      onClick={() => setBox(b.id)}
                      className={`inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors ${
                        active
                          ? "bg-accent text-accent-fg shadow-xs"
                          : "bg-surface-2 text-fg-muted hover:bg-surface-3 hover:text-fg"
                      }`}
                    >
                      <span>{b.label}</span>
                      {b.id === "inbox" && unreadCount > 0 && (
                        <span
                          className={`rounded-full px-1.5 py-0.2 text-xs font-semibold ${
                            active
                              ? "bg-surface/20 text-accent-fg"
                              : "bg-accent-soft text-accent"
                          }`}
                        >
                          {unreadCount}
                        </span>
                      )}
                    </button>
                  );
                })}
              </div>

              <div className="flex flex-col gap-2 sm:flex-row">
                <div className="relative flex-1">
                  <Search
                    size={14}
                    aria-hidden="true"
                    className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-subtle"
                  />
                  <input
                    type="search"
                    aria-label="Search messages"
                    placeholder="Search subject, body or @user…"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className={`${input} pl-8`}
                  />
                </div>
                {!fixedWorkspaceId && activeWorkspaces.length > 0 && (
                  <Select
                    aria-label="Filter by workspace"
                    value={workspaceFilter}
                    onChange={(e) => setWorkspaceFilter(e.target.value)}
                    className="sm:w-44"
                  >
                    <option value="">All workspaces</option>
                    {activeWorkspaces.map((w) => (
                      <option key={w.workspace_id} value={w.workspace_id}>
                        {w.workspace_name}
                      </option>
                    ))}
                  </Select>
                )}
              </div>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto">
              {threadsQuery.isError ? (
                <div role="alert" className={`${alertBox} m-3`}>
                  {threadsQuery.error instanceof Error
                    ? threadsQuery.error.message
                    : "Could not load threads."}
                </div>
              ) : threadsQuery.isPending ? (
                <Skeleton rows={6} label="Loading threads…" />
              ) : threads.length === 0 ? (
                <EmptyState
                  icon={Inbox}
                  title="No messages in this view"
                  hint="Start a direct user-to-user message or post a workspace thread to collaborate with your team."
                  compact
                  action={
                    <Button variant="primary" onClick={() => setComposing(true)}>
                      <MailPlus size={15} aria-hidden="true" />
                      New message
                    </Button>
                  }
                />
              ) : (
                <ul className="m-0 divide-y divide-border list-none p-0">
                  {threads.map((t) => (
                    <ThreadListRow
                      key={t.id}
                      thread={t}
                      myUserId={me.user.id}
                      selected={t.id === activeThreadId}
                      onSelect={() => setSelectedThreadId(t.id)}
                    />
                  ))}
                </ul>
              )}
            </div>
          </aside>
        )}

        {(!compact || activeThreadId) && (
          <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-bg">
            {activeThreadId ? (
              <ThreadReaderPane
                key={activeThreadId}
                threadId={activeThreadId}
                compact={compact}
                onBack={() => setSelectedThreadId("")}
                onUpdated={() => {
                  void queryClient.invalidateQueries({
                    queryKey: ["messages", "threads"],
                  });
                  void queryClient.invalidateQueries({
                    queryKey: messageKeys.unreadCount,
                  });
                }}
              />
            ) : (
              <EmptyState
                icon={MessageSquare}
                title="Select a conversation"
                hint="Choose a thread on the left to read its messages and reply, or start a new conversation."
                action={
                  <Button variant="primary" onClick={() => setComposing(true)}>
                    <MailPlus size={15} aria-hidden="true" />
                    New message
                  </Button>
                }
              />
            )}
          </main>
        )}
      </div>

      {composing && (
        <ComposeMessageDialog
          me={me}
          {...(effectiveWs ? { defaultWorkspaceId: effectiveWs } : {})}
          onClose={() => setComposing(false)}
          onCreated={(created) => {
            setSelectedThreadId(created.id);
          }}
        />
      )}
    </div>
  );
}

function ThreadListRow({
  thread,
  myUserId,
  selected,
  onSelect,
}: {
  thread: MessageThreadSummary;
  myUserId: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        aria-current={selected ? "true" : undefined}
        onClick={onSelect}
        className={`flex w-full flex-col gap-1.5 px-3.5 py-3 text-left transition-colors ${
          selected
            ? "bg-accent-soft/50"
            : "hover:bg-surface-2/70"
        }`}
      >
        <div className="flex items-center gap-2">
          {thread.unread && (
            <>
              <span
                aria-hidden="true"
                className="h-2 w-2 shrink-0 rounded-full bg-accent"
              />
              <span className="sr-only">Unread: </span>
            </>
          )}
          <span
            className={`min-w-0 flex-1 truncate text-sm ${
              thread.unread ? "font-semibold text-fg" : "font-medium text-fg"
            }`}
          >
            {thread.subject}
          </span>
          {thread.message_count > 1 && (
            <Badge tone="neutral">{thread.message_count}</Badge>
          )}
          <span className="shrink-0 text-xs text-fg-subtle">
            {formatTimestamp(thread.last_message_at)}
          </span>
        </div>

        <div className="flex items-center gap-1.5 text-xs text-fg-muted">
          {thread.kind === "workspace" ? (
            <Badge tone="accent">
              <Building2 size={11} aria-hidden="true" />
              {thread.workspace_name || "Workspace"}
            </Badge>
          ) : (
            <span className="inline-flex min-w-0 items-center gap-1 truncate font-medium text-fg-muted">
              <Users size={12} aria-hidden="true" className="shrink-0" />
              <span className="truncate">
                {formatParticipantSummary(thread.participants ?? [], myUserId)}
              </span>
            </span>
          )}
        </div>

        {thread.snippet && (
          <p className="m-0 line-clamp-2 text-xs text-fg-muted">
            {thread.last_sender_display_name
              ? `${thread.last_sender_display_name}: `
              : ""}
            {thread.snippet}
          </p>
        )}
      </button>
    </li>
  );
}

function ThreadReaderPane({
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
                    onClick={() => setReplyToMsg(m)}
                  >
                    <CornerUpLeft size={13} aria-hidden="true" />
                    Reply
                  </Button>
                </div>
              </div>

              {parent && idx > 0 && (
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

              <div className="mt-2.5 whitespace-pre-wrap text-sm leading-relaxed text-fg">
                {m.body}
              </div>
            </li>
          );
        })}
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
            <button
              type="button"
              aria-label="Cancel specific reply target"
              onClick={() => setReplyToMsg(null)}
              className="inline-flex h-5 w-5 items-center justify-center rounded hover:bg-surface-3"
            >
              <X size={12} aria-hidden="true" />
            </button>
          </div>
        )}

        <Field label="Reply to thread" error={replyError}>
          <Textarea
            rows={3}
            value={replyBody}
            onChange={(e) => setReplyBody(e.target.value)}
            placeholder="Write a reply… (Ctrl+Enter or Cmd+Enter to send)"
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

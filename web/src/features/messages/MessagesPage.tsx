import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronRight,
  Inbox,
  Mail,
  MailPlus,
  MessageSquare,
  Search,
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
  Select,
  Skeleton,
} from "@/components/ui";
import { alertBox, input } from "@/components/styles";
import {
  listMessageThreads,
  messageKeys,
  type MailboxKind,
} from "./messagesApi";
import { ComposeMessageDialog } from "./ComposeMessageDialog";
import { ThreadListRow } from "./ThreadListRow";
import { ThreadReaderPane } from "./ThreadReaderPane";

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
          className="group inline-flex items-center gap-2 rounded-md px-1 py-1 text-sm font-medium text-fg-muted hover:text-fg focus-visible:outline-focus"
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
              className="inline-flex items-center gap-2 rounded-md px-1.5 py-1 text-sm font-medium text-fg-muted hover:bg-surface-2 hover:text-fg focus-visible:outline-focus"
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
                      className={`inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors focus-visible:outline-focus ${
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

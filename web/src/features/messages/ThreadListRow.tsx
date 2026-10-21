import { Badge } from "@/components/ui";
import { Building2, Users } from "lucide-react";
import type { MessageParticipant, MessageThreadSummary } from "./messagesApi";

export function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatParticipantSummary(
  participants: MessageParticipant[],
  myUserId: string,
): string {
  const others = participants.filter((p) => p.id !== myUserId);
  const target = others.length > 0 ? others : participants;
  return target
    .map((p) => {
      const name = p.username ? `@${p.username}` : p.display_name || p.email;
      if (others.length === 0 && p.id === myUserId) {
        return `${name} (you)`;
      }
      return name;
    })
    .join(", ");
}

export function ThreadListRow({
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
        className={`flex w-full flex-col gap-1.5 px-3.5 py-3 text-left transition-colors focus-visible:outline-focus ${
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

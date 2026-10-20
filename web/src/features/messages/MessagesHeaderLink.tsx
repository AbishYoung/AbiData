import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Mail } from "lucide-react";
import { Badge, buttonClass } from "@/components/ui";
import { iconBtn } from "@/components/styles";
import { Tip } from "@/components/Tip";
import { getUnreadMessageCount, messageKeys } from "./messagesApi";

export function MessagesHeaderLink({
  ws,
  compact = false,
}: {
  ws?: string;
  compact?: boolean;
}) {
  const unread = useQuery({
    queryKey: messageKeys.unreadCount,
    queryFn: getUnreadMessageCount,
    staleTime: 30_000,
    retry: false,
  });
  const count = unread.data ?? 0;
  const label = count > 0 ? `Messages (${count} unread)` : "Messages";

  if (compact) {
    return (
      <Tip label={label}>
        {ws ? (
          <Link
            to="/w/$ws/messages"
            params={{ ws }}
            aria-label={label}
            className={`${iconBtn} relative`}
          >
            <Mail size={18} aria-hidden="true" />
            {count > 0 && (
              <span
                aria-hidden="true"
                className="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-accent"
              />
            )}
          </Link>
        ) : (
          <Link to="/messages" aria-label={label} className={`${iconBtn} relative`}>
            <Mail size={18} aria-hidden="true" />
            {count > 0 && (
              <span
                aria-hidden="true"
                className="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-accent"
              />
            )}
          </Link>
        )}
      </Tip>
    );
  }

  return (
    <Tip label={label}>
      {ws ? (
        <Link
          to="/w/$ws/messages"
          params={{ ws }}
          aria-label={label}
          className={buttonClass("ghost", "md")}
        >
          <Mail size={16} aria-hidden="true" />
          <span>Messages</span>
          {count > 0 && <Badge tone="accent">{count}</Badge>}
        </Link>
      ) : (
        <Link
          to="/messages"
          aria-label={label}
          className={buttonClass("ghost", "md")}
        >
          <Mail size={16} aria-hidden="true" />
          <span>Messages</span>
          {count > 0 && <Badge tone="accent">{count}</Badge>}
        </Link>
      )}
    </Tip>
  );
}

import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { DropdownMenu } from "radix-ui";
import { ArrowLeft, Compass, ExternalLink, FileText, MoreHorizontal, Settings, Table2 } from "lucide-react";
import type { Me } from "@/lib/session";
import { ApiError } from "@/lib/api";
import { useIsCompact } from "@/lib/useMediaQuery";
import { LogoMark } from "@/components/brand/Logo";
import { Badge, buttonClass, Skeleton, StatusPage } from "@/components/ui";
import { alertBox, menu, menuItem, menuSeparator } from "@/components/styles";
import { UserMenu } from "@/features/nav/UserMenu";
import { blogKeys, getBlog, type BlogInfo } from "./blogApi";
import { PostList } from "./PostList";
import { PostEditor } from "./PostEditor";
import { BlogSettings } from "./BlogSettings";

export type BlogScreen =
  | { kind: "list"; status: string; q: string }
  | { kind: "new" }
  | { kind: "post"; id: number }
  | { kind: "settings" }
  | { kind: "missing" };

/** Whether the public blog is actually served: the owner's switch and the server's install switch. */
export function isLive(info: BlogInfo): boolean {
  return info.published && info.public_blogs_enabled;
}

export function BlogNotFound() {
  return (
    <StatusPage
      icon={Compass}
      title="Page not found"
      actions={
        <a href="/" className={buttonClass("primary")}>
          Back to your workspaces
        </a>
      }
    >
      <p className="m-0">The address may be mistyped, or the page may have moved. Your workspaces are one click away.</p>
    </StatusPage>
  );
}

/** The blog authoring UI (ADR 0017): its own top bar and one screen below it. */
export function BlogApp({ ws, me, screen, onSearch }: { ws: string; me: Me; screen: BlogScreen; onSearch?: (status: string, q: string) => void }) {
  const info = useQuery({ queryKey: blogKeys.info(ws), queryFn: () => getBlog(ws), retry: false });
  if (screen.kind === "missing" || (info.error instanceof ApiError && (info.error.status === 404 || info.error.status === 403))) return <BlogNotFound />;
  let body: ReactNode;
  if (info.isError) {
    body = (
      <div role="alert" className={`${alertBox} m-4`}>
        {info.error.message}
      </div>
    );
  } else if (!info.data) {
    body = <Skeleton label="Loading blog…" />;
  } else if (screen.kind === "list") {
    body = <PostList ws={ws} info={info.data} status={screen.status} q={screen.q} onSearch={onSearch ?? (() => undefined)} />;
  } else if (screen.kind === "settings") {
    body = <BlogSettings ws={ws} info={info.data} />;
  } else {
    body = <PostEditor key={screen.kind === "post" ? screen.id : "new"} ws={ws} info={info.data} id={screen.kind === "post" ? screen.id : null} />;
  }
  return (
    <div className="flex h-full flex-col">
      <BlogTopBar ws={ws} me={me} info={info.data} current={screen.kind === "settings" ? "settings" : "posts"} />
      <main className="min-h-0 flex-1 overflow-auto bg-bg">{body}</main>
    </div>
  );
}

function BlogTopBar({ ws, me, info, current }: { ws: string; me: Me; info: BlogInfo | undefined; current: "posts" | "settings" }) {
  const compact = useIsCompact();
  const live = info ? isLive(info) : false;
  const navLink = (active: boolean) =>
    `inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-sm font-medium ${active ? "bg-surface-3 text-fg" : "text-fg-muted hover:bg-surface-2 hover:text-fg"}`;
  return (
    <header className="flex min-h-12 shrink-0 items-center gap-2 border-b border-border bg-surface px-3">
      <Link to="/" aria-label="Workspaces" className="inline-flex shrink-0 items-center rounded-md px-1">
        <LogoMark size={24} />
      </Link>
      <h1 className="min-w-0 truncate text-sm font-semibold text-fg">{info ? info.title || `@${info.username}` : "Blog"}</h1>
      {info && <Badge tone={live ? "success" : "neutral"}>{live ? "Live" : "Off"}</Badge>}
      {!compact && (
        <nav aria-label="Blog" className="ml-2 flex items-center gap-1">
          <Link to="/w/$ws/blog" params={{ ws }} className={navLink(current === "posts")} aria-current={current === "posts" ? "page" : undefined}>
            Posts
          </Link>
          <Link to="/w/$ws/blog/settings" params={{ ws }} className={navLink(current === "settings")} aria-current={current === "settings" ? "page" : undefined}>
            Settings
          </Link>
        </nav>
      )}
      <div className="ml-auto flex items-center gap-1.5">
        {info && live && !compact && (
          <a href={info.public_url} target="_blank" rel="noopener noreferrer" className={buttonClass()}>
            <ExternalLink size={14} aria-hidden="true" />
            View blog
          </a>
        )}
        <DropdownMenu.Root>
          <DropdownMenu.Trigger className={buttonClass("ghost")} aria-label="More">
            <MoreHorizontal size={16} aria-hidden="true" />
            {!compact && "More"}
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content align="end" sideOffset={4} className={`ui-menu z-50 min-w-56 ${menu} text-fg`}>
              {compact && (
                <>
                  <DropdownMenu.Item asChild className={menuItem}>
                    <Link to="/w/$ws/blog" params={{ ws }}>
                      <FileText size={16} aria-hidden="true" />
                      Posts
                    </Link>
                  </DropdownMenu.Item>
                  <DropdownMenu.Item asChild className={menuItem}>
                    <Link to="/w/$ws/blog/settings" params={{ ws }}>
                      <Settings size={16} aria-hidden="true" />
                      Settings
                    </Link>
                  </DropdownMenu.Item>
                  {info && live && (
                    <DropdownMenu.Item asChild className={menuItem}>
                      <a href={info.public_url} target="_blank" rel="noopener noreferrer">
                        <ExternalLink size={16} aria-hidden="true" />
                        View blog
                      </a>
                    </DropdownMenu.Item>
                  )}
                  <DropdownMenu.Separator className={menuSeparator} />
                </>
              )}
              <DropdownMenu.Item asChild className={menuItem}>
                <Link to="/w/$ws" params={{ ws }}>
                  <Table2 size={16} aria-hidden="true" />
                  Open as workspace
                </Link>
              </DropdownMenu.Item>
              <DropdownMenu.Item asChild className={menuItem}>
                <Link to="/">
                  <ArrowLeft size={16} aria-hidden="true" />
                  Back to workspaces
                </Link>
              </DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
        <UserMenu me={me} />
      </div>
    </header>
  );
}

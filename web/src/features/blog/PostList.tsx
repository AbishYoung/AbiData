import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { DropdownMenu, ToggleGroup } from "radix-ui";
import { MoreHorizontal, PenLine, Plus, Search, Trash2 } from "lucide-react";
import { askToConfirm, Badge, Button, buttonClass, EmptyState, Input, Skeleton } from "@/components/ui";
import { alertBox, infoBox, menu, menuItem } from "@/components/styles";
import { blogKeys, deletePost, getPost, listPosts, type BlogInfo, type BlogPostSummary } from "./blogApi";
import { CHIP_LABEL, CHIP_TONE } from "./postModel";
import { TakedownBanner } from "./TakedownBanner";

const FILTERS = [
  { value: "", label: "All" },
  { value: "draft", label: "Drafts" },
  { value: "published", label: "Published" },
  { value: "unlisted", label: "Unlisted" },
  { value: "needs_attention", label: "Needs attention" },
] as const;

export function formatDate(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

/** The post list (ADR 0017): chips, a status filter and title search kept in the URL, Load more. */
export function PostList({ ws, info, status, q, onSearch }: { ws: string; info: BlogInfo; status: string; q: string; onSearch: (status: string, q: string) => void }) {
  const qc = useQueryClient();
  const [text, setText] = useState(q);
  const [lastQ, setLastQ] = useState(q);
  if (q !== lastQ) {
    // The URL changed (back button, a filter click): show its search.
    setLastQ(q);
    setText(q);
  }
  useEffect(() => {
    if (text.trim() === q) return;
    const t = setTimeout(() => onSearch(status, text.trim()), 300);
    return () => clearTimeout(t);
  }, [text, q, status, onSearch]);

  const posts = useInfiniteQuery({
    queryKey: blogKeys.posts(ws, status, q),
    queryFn: ({ pageParam }) => listPosts(ws, status, q, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const items = posts.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const filtered = status !== "" || q !== "";

  const remove = useMutation({
    mutationFn: async (p: BlogPostSummary) => {
      const fresh = await getPost(ws, p.id);
      await deletePost(ws, p.id, fresh._version);
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: blogKeys.allPosts(ws) }),
  });

  async function onDelete(p: BlogPostSummary) {
    const ok = await askToConfirm({
      title: `Delete “${p.title || "Untitled"}”?`,
      message: p.status === "draft" ? "The draft is deleted." : "The post is deleted and its address stops working.",
      confirmLabel: "Delete post",
      danger: true,
    });
    if (ok) remove.mutate(p);
  }

  const filterItem =
    "h-7 cursor-pointer whitespace-nowrap rounded px-3 text-xs font-medium text-fg-muted transition-colors hover:text-fg " +
    "data-[state=on]:bg-surface data-[state=on]:text-fg data-[state=on]:shadow-xs";
  const filters = status === "taken_down" || items.some((p) => p.chip === "taken_down") ? [...FILTERS, { value: "taken_down", label: "Taken down" } as const] : FILTERS;

  return (
    <div className="mx-auto w-full max-w-4xl space-y-4 p-4 sm:p-6">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="m-0 text-xl font-semibold tracking-tight text-fg">Posts</h2>
        <Link to="/w/$ws/blog/new" params={{ ws }} className={`${buttonClass("primary")} ml-auto`}>
          <Plus size={16} aria-hidden="true" />
          New post
        </Link>
      </div>
      {info.takedown && <TakedownBanner takedown={info.takedown} scope="blog" />}
      {!info.published && (
        <p className={`${infoBox} m-0`}>
          Your blog is off. Published posts are not visible until you turn it on in{" "}
          <Link to="/w/$ws/blog/settings" params={{ ws }} className="font-medium text-accent underline">
            Settings
          </Link>
          .
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <div className="max-w-full overflow-x-auto">
          <ToggleGroup.Root
            type="single"
            value={status}
            onValueChange={(v) => onSearch(v, q)}
            aria-label="Filter posts"
            className="inline-flex rounded-md border border-border bg-surface-2 p-0.5"
          >
            {filters.map((f) => (
              <ToggleGroup.Item key={f.value} value={f.value} className={filterItem}>
                {f.label}
              </ToggleGroup.Item>
            ))}
          </ToggleGroup.Root>
        </div>
        <label className="relative ml-auto w-full sm:w-64">
          <span className="sr-only">Search titles</span>
          <Search size={14} aria-hidden="true" className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-subtle" />
          <Input type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder="Search titles" className="pl-8" />
        </label>
      </div>
      {posts.isError ? (
        <div role="alert" className={alertBox}>
          {posts.error.message}
        </div>
      ) : posts.isPending ? (
        <Skeleton label="Loading posts…" />
      ) : items.length === 0 ? (
        filtered ? (
          <p role="status" className="py-12 text-center text-sm text-fg-muted">
            No posts match.
          </p>
        ) : (
          <EmptyState
            icon={PenLine}
            title="No posts yet"
            hint="Your blog is a public page of posts at your own address. Nothing is public until you publish it."
            action={
              <Link to="/w/$ws/blog/new" params={{ ws }} className={buttonClass("primary")}>
                Write your first post
              </Link>
            }
          />
        )
      ) : (
        <ul className="m-0 list-none divide-y divide-border rounded-xl border border-border bg-surface p-0 shadow-sm">
          {items.map((p) => (
            <li key={p.id} className="flex items-start gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <Link to="/w/$ws/blog/posts/$id" params={{ ws, id: String(p.id) }} className="block truncate text-sm font-semibold text-fg hover:text-accent hover:underline">
                  {p.title || "Untitled"}
                </Link>
                <p className="m-0 mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-muted">
                  <Badge tone={CHIP_TONE[p.chip]}>{CHIP_LABEL[p.chip]}</Badge>
                  <span>{p.published_at ? `Published ${formatDate(p.published_at)}` : "Not published"}</span>
                  {p.last_edited_at && <span>Edited {formatDate(p.last_edited_at)}</span>}
                </p>
                {p.problem && <p className="m-0 mt-1 text-xs text-warning">{p.problem}</p>}
              </div>
              <DropdownMenu.Root>
                <DropdownMenu.Trigger className={buttonClass("ghost", "sm")} aria-label={`Actions for ${p.title || "Untitled"}`}>
                  <MoreHorizontal size={16} aria-hidden="true" />
                </DropdownMenu.Trigger>
                <DropdownMenu.Portal>
                  <DropdownMenu.Content align="end" sideOffset={4} className={`ui-menu z-50 ${menu} text-fg`}>
                    <DropdownMenu.Item className={menuItem} onSelect={() => setTimeout(() => void onDelete(p), 0)}>
                      <Trash2 size={16} aria-hidden="true" />
                      Delete…
                    </DropdownMenu.Item>
                  </DropdownMenu.Content>
                </DropdownMenu.Portal>
              </DropdownMenu.Root>
            </li>
          ))}
        </ul>
      )}
      {posts.hasNextPage && (
        <div className="text-center">
          <Button onClick={() => void posts.fetchNextPage()} disabled={posts.isFetchingNextPage}>
            {posts.isFetchingNextPage ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}
    </div>
  );
}

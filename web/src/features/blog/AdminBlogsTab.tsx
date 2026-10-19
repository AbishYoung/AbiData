import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SkeletonLines } from "@/components/EditorParts";
import { Badge, Button, Card, Dialog, Field, Input, Select, Textarea } from "@/components/ui";
import { warningBox } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { ErrorBox, td, th } from "@/features/admin/ui";
import { absoluteTime } from "@/features/backups/backupLogic";
import {
  REASONS,
  adminBlogKeys,
  adminGetBlog,
  adminGetPublicBlogs,
  adminListBlogs,
  adminPutPublicBlogs,
  adminResolveReports,
  adminRestore,
  adminTakeDown,
  type AdminBlog,
  type TakedownReason,
} from "./blogApi";

const reasonLabel = (code: string) => REASONS.find((r) => r.value === code)?.label ?? code;

/** Admin console: the public-blogs switch, every blog, and take-down and restore (ADR 0017). */
export function AdminBlogsTab() {
  const qc = useQueryClient();
  const [q, setQ] = useState("");
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [status, setStatus] = useState("");
  const fail = (e: unknown) => setStatus(e instanceof ApiError ? e.message : "That did not work.");

  const setting = useQuery({ queryKey: adminBlogKeys.setting, queryFn: adminGetPublicBlogs });
  const blogs = useQuery({ queryKey: adminBlogKeys.list(q.trim(), filter), queryFn: () => adminListBlogs(q.trim(), filter) });
  const toggle = useMutation({
    meta: { inline: true },
    mutationFn: (on: boolean) => adminPutPublicBlogs(on),
    onSuccess: (s) => {
      qc.setQueryData(adminBlogKeys.setting, s);
      setStatus(s.enabled ? "Public blogs are on. Published blogs are served at /b/{username}." : "Public blogs are off. Every blog address now answers as unknown.");
    },
    onError: fail,
  });

  return (
    <section aria-label="Blogs" className="space-y-4">
      <p className="m-0 text-sm text-fg-muted">
        Every user has a blog. Nothing is public until the server allows public blogs and the owner publishes. Take down a blog or one post to stop it being
        served; its owner sees the reason and your note, and can edit but not publish until you restore it.
      </p>
      {setting.data &&
        (setting.data.forced_off ? (
          <p className={`${warningBox} m-0`}>ABIDATA_PUBLIC_BLOGS=false turns public blogs off on this server. Change the environment setting to allow them.</p>
        ) : (
          <label className="flex items-center gap-2 text-sm font-medium">
            <input type="checkbox" checked={setting.data.enabled} disabled={toggle.isPending} onChange={(e) => toggle.mutate(e.target.checked)} />
            Allow public blogs on this server
          </label>
        ))}
      <div role="status" aria-live="polite" className="min-h-5 text-sm text-fg-muted">
        {status}
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Search usernames" className="w-64">
          <Input type="search" value={q} onChange={(e) => setQ(e.target.value)} />
        </Field>
        <Field label="Show" className="w-48">
          <Select value={filter} onChange={(e) => setFilter(e.target.value)}>
            <option value="">All blogs</option>
            <option value="taken_down">Taken down</option>
            <option value="reported">With open reports</option>
          </Select>
        </Field>
      </div>
      {blogs.isError && <ErrorBox error={blogs.error} onRetry={() => void blogs.refetch()} />}
      {blogs.isPending && <SkeletonLines rows={3} label="Loading blogs…" />}
      {blogs.data && (
        <div className="scroll-shadow overflow-x-auto rounded-lg border border-border">
          <table className="w-full border-collapse text-sm">
            <caption className="sr-only">Blogs on this server</caption>
            <thead>
              <tr className="border-b border-border bg-surface-2">
                <th scope="col" className={th}>
                  Blog
                </th>
                <th scope="col" className={th}>
                  Status
                </th>
                <th scope="col" className={th}>
                  Moderation
                </th>
                <th scope="col" className={th}>
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {blogs.data.length === 0 && (
                <tr>
                  <td colSpan={4} className={`${td} text-fg-muted`}>
                    No blogs match.
                  </td>
                </tr>
              )}
              {blogs.data.map((b) => (
                <tr key={b.id} className="border-t border-border">
                  <th scope="row" className={`${td} text-left font-medium`}>
                    @{b.username}
                    {b.title && <span className="block text-xs font-normal text-fg-muted">{b.title}</span>}
                  </th>
                  <td className={td}>
                    <Badge tone={b.published ? "success" : "neutral"}>{b.published ? "Published" : "Off"}</Badge>
                  </td>
                  <td className={td}>
                    {b.active_takedowns > 0 && <Badge tone="danger">Taken down</Badge>}{" "}
                    {b.open_reports > 0 && <Badge tone="warning">{b.open_reports === 1 ? "1 report" : `${b.open_reports} reports`}</Badge>}
                  </td>
                  <td className={td}>
                    <Button size="sm" onClick={() => setSelected(b.id)} aria-label={`Open @${b.username}`}>
                      Open
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {selected && <BlogDetail id={selected} onStatus={setStatus} onClose={() => setSelected(null)} />}
    </section>
  );
}

interface DownTarget {
  slug: string;
  title: string;
}

function BlogDetail({ id, onStatus, onClose }: { id: string; onStatus: (s: string) => void; onClose: () => void }) {
  const qc = useQueryClient();
  const blog = useQuery({ queryKey: adminBlogKeys.one(id), queryFn: () => adminGetBlog(id) });
  const [target, setTarget] = useState<DownTarget | null>(null);
  const done = (b: AdminBlog, msg: string) => {
    qc.setQueryData(adminBlogKeys.one(id), b);
    void qc.invalidateQueries({ queryKey: adminBlogKeys.all });
    onStatus(msg);
  };
  const fail = (e: unknown) => onStatus(e instanceof ApiError ? e.message : "That did not work.");
  const restore = useMutation({
    meta: { inline: true },
    mutationFn: (slug: string) => adminRestore(id, slug || undefined),
    onSuccess: (b, slug) => done(b, slug ? `Restored the post “${slug}”.` : `Restored @${b.username}.`),
    onError: fail,
  });
  const resolve = useMutation({
    meta: { inline: true },
    mutationFn: () => adminResolveReports(id),
    onSuccess: (b) => done(b, "Marked the reports as handled."),
    onError: fail,
  });

  if (blog.isError) return <ErrorBox error={blog.error} onRetry={() => void blog.refetch()} />;
  if (!blog.data) return <SkeletonLines rows={4} label="Loading blog…" />;
  const b = blog.data;
  const active = (b.takedowns ?? []).filter((t) => !t.restored_at);
  const blogDown = active.find((t) => !t.post_slug);

  return (
    <Card
      title={`@${b.username}`}
      description={b.title || undefined}
      actions={
        <>
          {blogDown ? (
            <Button size="sm" disabled={restore.isPending} onClick={() => restore.mutate("")}>
              Restore blog
            </Button>
          ) : (
            <Button size="sm" onClick={() => setTarget({ slug: "", title: `@${b.username}` })}>
              Take down blog
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onClose}>
            Close
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="m-0 text-sm">
          <a href={b.public_url} target="_blank" rel="noopener noreferrer" className="text-accent underline">
            {b.public_url}
          </a>
          {b.damaged && <span className="ml-2 text-warning">The posts table is damaged, so nothing is served.</span>}
        </p>
        {blogDown && (
          <p className={`${warningBox} m-0`}>
            Taken down on {absoluteTime(blogDown.taken_down_at)}: {reasonLabel(blogDown.reason_code)}. {blogDown.note}
          </p>
        )}
        <div>
          <h3 className="m-0 mb-2 text-sm font-semibold">Published posts</h3>
          {(b.posts ?? []).length === 0 ? (
            <p className="m-0 text-sm text-fg-muted">No published or unlisted posts. Drafts are only reachable through workspace access.</p>
          ) : (
            <ul className="m-0 list-none divide-y divide-border rounded-lg border border-border p-0">
              {(b.posts ?? []).map((p) => (
                <li key={p.slug} className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
                  <a href={p.url} target="_blank" rel="noopener noreferrer" className="min-w-0 flex-1 truncate font-medium text-fg hover:underline">
                    {p.title || p.slug}
                  </a>
                  {p.status === "unlisted" && <Badge>Unlisted</Badge>}
                  {p.taken_down ? (
                    <>
                      <Badge tone="danger">Taken down</Badge>
                      <Button size="sm" disabled={restore.isPending} onClick={() => restore.mutate(p.slug)} aria-label={`Restore ${p.title || p.slug}`}>
                        Restore
                      </Button>
                    </>
                  ) : (
                    <Button size="sm" onClick={() => setTarget({ slug: p.slug, title: p.title || p.slug })} aria-label={`Take down ${p.title || p.slug}`}>
                      Take down
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
        <div>
          <div className="mb-2 flex items-center gap-2">
            <h3 className="m-0 text-sm font-semibold">Reports</h3>
            {(b.open_reports ?? 0) > 0 && (
              <Button size="sm" disabled={resolve.isPending} onClick={() => resolve.mutate()}>
                Mark handled
              </Button>
            )}
          </div>
          {(b.reports ?? []).length === 0 ? (
            <p className="m-0 text-sm text-fg-muted">No reports.</p>
          ) : (
            <ul className="m-0 list-none space-y-2 p-0 text-sm">
              {(b.reports ?? []).map((r) => (
                <li key={r.id}>
                  <span className="font-medium">{reasonLabel(r.reason_code)}</span> {r.post_slug ? `on “${r.post_slug}”` : "on the blog"},{" "}
                  {absoluteTime(r.created_at)}
                  {r.resolved_at && <span className="text-fg-muted"> (handled)</span>}
                  {r.message && <span className="block text-fg-muted">{r.message}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
        {(b.takedowns ?? []).length > 0 && (
          <div>
            <h3 className="m-0 mb-2 text-sm font-semibold">Take-down history</h3>
            <ul className="m-0 list-none space-y-1 p-0 text-sm">
              {(b.takedowns ?? []).map((t) => (
                <li key={t.id}>
                  {t.post_slug ? `Post “${t.post_slug}”` : "Whole blog"}: {reasonLabel(t.reason_code)}, {absoluteTime(t.taken_down_at)}
                  {t.restored_at ? `, restored ${absoluteTime(t.restored_at)}` : ", active"}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
      {target && (
        <TakeDownDialog
          target={target}
          onClose={() => setTarget(null)}
          onDone={(next) => {
            setTarget(null);
            done(next, target.slug ? `Took down the post “${target.title}”.` : `Took down ${target.title}.`);
          }}
          blogID={id}
        />
      )}
    </Card>
  );
}

function TakeDownDialog({ blogID, target, onClose, onDone }: { blogID: string; target: DownTarget; onClose: () => void; onDone: (b: AdminBlog) => void }) {
  const [reason, setReason] = useState<TakedownReason>("spam");
  const [note, setNote] = useState("");
  const go = useMutation({
    meta: { inline: true },
    mutationFn: () => adminTakeDown(blogID, { ...(target.slug ? { post_slug: target.slug } : {}), reason_code: reason, note }),
    onSuccess: onDone,
  });
  return (
    <Dialog
      title={`Take down ${target.slug ? `“${target.title}”` : target.title}?`}
      description={target.slug ? "The post stops being served at once." : "The blog's home and every post stop being served at once."}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="danger" disabled={go.isPending} onClick={() => go.mutate()}>
            Take down
          </Button>
        </>
      }
    >
      <Field label="Reason">
        <Select value={reason} onChange={(e) => setReason(e.target.value as TakedownReason)}>
          {REASONS.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </Select>
      </Field>
      <Field label="Note to the owner" hint="Shown to the owner with the reason. Say what to change and who to contact.">
        <Textarea value={note} maxLength={1000} rows={3} onChange={(e) => setNote(e.target.value)} />
      </Field>
      {go.isError && (
        <p role="alert" className="m-0 text-sm text-danger">
          {go.error instanceof ApiError ? go.error.message : "That did not work."}
        </p>
      )}
    </Dialog>
  );
}

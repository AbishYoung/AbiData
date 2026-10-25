import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import { useBlocker, useNavigate } from "@tanstack/react-router";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { Tabs } from "radix-ui";
import { Bold, Code, Heading2, Image, ImageUp, Italic, Link2, List, Quote } from "lucide-react";
import { ApiError } from "@/lib/api";
import { matchesShortcut } from "@/lib/shortcuts";
import { useThemeStore } from "@/lib/theme";
import { useIsCompact } from "@/lib/useMediaQuery";
import { askToConfirm, Badge, Button, Dialog, Field, IconButton, Input, Skeleton, Textarea, ToolbarSeparator } from "@/components/ui";
import { alertBox, segTray, warningBox } from "@/components/styles";
import { uploadFile, type UploadHandle } from "@/features/files/filesApi";
import { BlogNotFound } from "./BlogApp";
import { CoverField } from "./CoverField";
import { TakedownBanner } from "./TakedownBanner";
import { blogKeys, checkSlug, createPost, getPost, previewPost, setPostStatus, updatePost, type BlogInfo, type BlogPost, type PostInput } from "./blogApi";
import {
  ACTION_LABEL,
  actionsFor,
  applyMarkdown,
  bodySizeNote,
  byteLength,
  CHIP_LABEL,
  CHIP_TONE,
  editSlug,
  followTitle,
  initialSlug,
  insertUploadedImage,
  MAX_BODY_BYTES,
  MAX_SUMMARY,
  MAX_TITLE,
  readingStats,
  slugify,
  targetStatus,
  type MarkdownKind,
  type PostAction,
  type SlugState,
  type TextEdit,
} from "./postModel";

const ACCEPT_IMAGE = "image/png,image/jpeg,image/gif,image/webp";

/** Loads the post (or starts a new one) and hands it to the form. */
export function PostEditor({ ws, info, id }: { ws: string; info: BlogInfo; id: number | null }) {
  const post = useQuery({ queryKey: blogKeys.post(ws, id ?? 0), queryFn: () => getPost(ws, id ?? 0), enabled: id !== null, retry: false });
  if (id !== null) {
    if (post.error instanceof ApiError && post.error.status === 404) return <BlogNotFound />;
    if (post.isError)
      return (
        <div role="alert" className={`${alertBox} m-4`}>
          {post.error.message}
        </div>
      );
    if (!post.data) return <Skeleton label="Loading post…" />;
  }
  return <PostForm ws={ws} info={info} initial={id === null ? null : (post.data ?? null)} />;
}

interface Draft {
  title: string;
  slug: SlugState;
  summary: string;
  body: string;
  publishedAt: string;
  cover: string;
  coverAlt: string;
}

function toLocalInput(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function draftOf(p: BlogPost | null): Draft {
  return {
    title: p?.title ?? "",
    slug: initialSlug(p?.slug ?? "", !!p?.published_at, p === null),
    summary: p?.summary ?? "",
    body: p?.body_md ?? "",
    publishedAt: toLocalInput(p?.published_at),
    cover: p?.cover ?? "",
    coverAlt: p?.cover_alt ?? "",
  };
}

/** The fields of the draft that differ from the saved post; everything for a new post. */
export function changes(d: Draft, saved: BlogPost | null): PostInput {
  const out: PostInput = {};
  if (!saved || d.title !== saved.title) out.title = d.title;
  if (saved ? d.slug.slug !== saved.slug : d.slug.locked && d.slug.slug !== "") out.slug = d.slug.slug;
  if (!saved || d.summary !== saved.summary) out.summary = d.summary;
  if (!saved || d.body !== saved.body_md) out.body_md = d.body;
  if (saved && d.publishedAt !== toLocalInput(saved.published_at)) {
    out.published_at = d.publishedAt ? new Date(d.publishedAt).toISOString() : "";
  }
  if (saved ? d.cover !== (saved.cover ?? "") : d.cover !== "") out.cover = d.cover;
  if (saved ? d.coverAlt !== (saved.cover_alt ?? "") : d.coverAlt !== "") out.cover_alt = d.coverAlt;
  return out;
}

const FIELD_OF: Record<string, keyof Errors> = { title: "title", slug: "slug", summary: "summary", body_md: "body", published_at: "publishedAt", cover: "cover", cover_alt: "coverAlt" };
type Errors = Partial<Record<"title" | "slug" | "summary" | "body" | "publishedAt" | "cover" | "coverAlt", string>>;

const TOOLBAR_GROUPS: { kind: MarkdownKind; label: string; shortcut?: string; Icon: typeof Bold }[][] = [
  [
    { kind: "heading", label: "Heading", Icon: Heading2 },
    { kind: "bold", label: "Bold", shortcut: "Mod+B", Icon: Bold },
    { kind: "italic", label: "Italic", shortcut: "Mod+I", Icon: Italic },
    { kind: "link", label: "Link", shortcut: "Mod+K", Icon: Link2 },
  ],
  [
    { kind: "list", label: "List", Icon: List },
    { kind: "quote", label: "Quote", Icon: Quote },
    { kind: "code", label: "Code", Icon: Code },
  ],
  [{ kind: "image", label: "Image (workspace file)", Icon: Image }],
];

function PostForm({ ws, info, initial }: { ws: string; info: BlogInfo; initial: BlogPost | null }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const compact = useIsCompact();
  const themePref = useThemeStore((s) => s.pref);
  const [saved, setSaved] = useState<BlogPost | null>(initial);
  const [d, setD] = useState<Draft>(() => draftOf(initial));
  const [editingSlug, setEditingSlug] = useState(false);
  const [errors, setErrors] = useState<Errors>({});
  const [actionError, setActionError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState<PostAction | null>(null);
  const [inlineProgress, setInlineProgress] = useState<number | null>(null);
  const [inlineError, setInlineError] = useState<string | null>(null);
  const titleRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const inlineFileRef = useRef<HTMLInputElement>(null);
  const inlineUpload = useRef<UploadHandle | null>(null);
  const leaving = useRef(false);
  // Selection to restore in the body once the edit that produced it has been
  // committed. Applied synchronously in a layout effect (not a deferred
  // requestAnimationFrame) so a stale callback can never overwrite a caret the
  // user has moved since.
  const pendingSelection = useRef<{ start: number; end: number } | null>(null);

  useEffect(() => () => inlineUpload.current?.abort(), []);

  useLayoutEffect(() => {
    const sel = pendingSelection.current;
    if (!sel) return;
    pendingSelection.current = null;
    const el = bodyRef.current;
    el?.focus();
    el?.setSelectionRange(sel.start, sel.end);
  });

  const pending = changes(d, saved);
  const dirty = saved ? Object.keys(pending).length > 0 : d.title !== "" || d.summary !== "" || d.body !== "" || d.cover !== "";
  const takenDown = !!saved?.takedown || !!info.takedown;
  const status = saved?.status ?? "new";
  const actions = actionsFor(status, takenDown);
  const bytes = byteLength(d.body);

  useEffect(() => {
    if (!initial) titleRef.current?.focus();
  }, [initial]);

  useBlocker({
    shouldBlockFn: async () => {
      if (!dirty || leaving.current) return false;
      return !(await askToConfirm({ title: "Leave this post?", message: "It has unsaved changes. Leaving discards them.", confirmLabel: "Discard changes", danger: true }));
    },
    enableBeforeUnload: () => dirty,
  });

  // The address the server would use for the slug as it stands.
  const [slugToCheck, setSlugToCheck] = useState(d.slug.slug);
  useEffect(() => {
    const t = setTimeout(() => setSlugToCheck(d.slug.slug), 400);
    return () => clearTimeout(t);
  }, [d.slug.slug]);
  const slugCheck = useQuery({
    queryKey: ["blog", ws, "slug-check", slugToCheck, saved?.id ?? 0],
    queryFn: () => checkSlug(ws, slugToCheck, saved?.id ?? 0),
    enabled: slugToCheck !== "" && slugToCheck !== saved?.slug,
    staleTime: 10_000,
  });
  const shownSlug = slugCheck.data && slugToCheck === d.slug.slug && slugToCheck !== saved?.slug ? slugCheck.data.slug : d.slug.slug || slugify(d.title);
  const base = info.public_url.replace(/\/+$/, "");

  // Preview, debounced after typing stops.
  const [previewSrc, setPreviewSrc] = useState({ title: d.title, body: d.body });
  useEffect(() => {
    const t = setTimeout(() => setPreviewSrc({ title: d.title, body: d.body }), 500);
    return () => clearTimeout(t);
  }, [d.title, d.body]);
  const preview = useQuery({
    queryKey: ["blog", ws, "preview", previewSrc.title, previewSrc.body],
    queryFn: ({ signal }) => previewPost(ws, previewSrc.title, previewSrc.body, signal),
    placeholderData: keepPreviousData,
    enabled: bytes <= MAX_BODY_BYTES,
    staleTime: Infinity,
  });

  function set<K extends keyof Draft>(k: K, v: Draft[K]) {
    setD((cur) => ({ ...cur, [k]: v }));
    if (k in errors) setErrors((e) => ({ ...e, [k]: undefined }));
  }

  function onTitle(v: string) {
    setD((cur) => ({ ...cur, title: v, slug: followTitle(cur.slug, v) }));
    setErrors((e) => ({ ...e, title: undefined }));
  }

  function applyEdit(edit: TextEdit) {
    pendingSelection.current = { start: edit.selStart, end: edit.selEnd };
    set("body", edit.text);
  }

  function onToolbar(kind: MarkdownKind) {
    const el = bodyRef.current;
    const start = el?.selectionStart ?? d.body.length;
    const end = el?.selectionEnd ?? d.body.length;
    applyEdit(applyMarkdown(kind, d.body, start, end));
  }

  function onBodyKeyDown(e: ReactKeyboardEvent<HTMLTextAreaElement>) {
    if (matchesShortcut(e, "blog.bold")) {
      e.preventDefault();
      e.stopPropagation();
      onToolbar("bold");
    } else if (matchesShortcut(e, "blog.italic")) {
      e.preventDefault();
      e.stopPropagation();
      onToolbar("italic");
    } else if (matchesShortcut(e, "blog.link")) {
      e.preventDefault();
      e.stopPropagation();
      onToolbar("link");
    }
  }

  async function onInlineImage(file: File | undefined) {
    if (!file) return;
    setInlineError(null);
    setInlineProgress(0);
    const el = bodyRef.current;
    const start = el?.selectionStart ?? d.body.length;
    const end = el?.selectionEnd ?? d.body.length;
    const h = uploadFile(ws, file, (loaded, total) => setInlineProgress(total ? Math.round((loaded / total) * 100) : 0));
    inlineUpload.current = h;
    try {
      const meta = await h.promise;
      if (!/^image\/(png|jpeg|gif|webp)$/.test(meta.content_type)) {
        setInlineError("Inline images must be PNG, JPEG, GIF or WebP.");
        return;
      }
      setD((cur) => {
        const s = Math.min(start, cur.body.length);
        const e = Math.min(Math.max(s, end), cur.body.length);
        const edit = insertUploadedImage(cur.body, s, e, meta.id, file.name);
        pendingSelection.current = { start: edit.selStart, end: edit.selEnd };
        return { ...cur, body: edit.text };
      });
    } catch (e) {
      if (e instanceof ApiError && e.canceled) return;
      setInlineError(e instanceof Error ? e.message : "The image upload failed.");
    } finally {
      inlineUpload.current = null;
      setInlineProgress(null);
      if (inlineFileRef.current) inlineFileRef.current.value = "";
    }
  }

  function adopt(p: BlogPost) {
    const wasNew = !saved;
    setSaved(p);
    setD(draftOf(p));
    setEditingSlug(false);
    qc.setQueryData(blogKeys.post(ws, p.id), p);
    void qc.invalidateQueries({ queryKey: blogKeys.allPosts(ws) });
    if (wasNew) {
      leaving.current = true;
      void navigate({ to: "/w/$ws/blog/posts/$id", params: { ws, id: String(p.id) }, replace: true });
    }
  }

  function validate(): boolean {
    const e: Errors = {};
    if (d.title.trim() === "") e.title = "Give the post a title.";
    if (bytes > MAX_BODY_BYTES) e.body = "The post is longer than 200 KB.";
    if (d.cover && d.coverAlt.trim() === "") e.coverAlt = "Describe the cover image for readers who cannot see it.";
    setErrors(e);
    const first = Object.keys(e)[0];
    if (first === "title") titleRef.current?.focus();
    else if (first === "body") bodyRef.current?.focus();
    return !first;
  }

  async function run(action: PostAction, overwrite = false) {
    setActionError(null);
    if (!validate()) return;
    const target = targetStatus(action);
    if (target === "draft" && !(await askToConfirm({ title: "Unpublish this post?", message: "Its address stops working until you publish it again.", confirmLabel: "Unpublish", danger: true }))) return;
    if ((target === "published" || target === "unlisted") && !info.published && !(await askToConfirm({ title: "Your blog is off", message: "The post is published now and goes live when you turn the blog on in Settings.", confirmLabel: ACTION_LABEL[action] }))) return;
    setBusy(true);
    try {
      let p = saved;
      let base = saved;
      if (overwrite && saved) {
        base = await getPost(ws, saved.id);
      }
      const body = changes(d, base);
      if (!p || !base) p = await createPost(ws, body);
      else if (Object.keys(body).length > 0) p = await updatePost(ws, base.id, base._version, body);
      else p = base;
      if (target && target !== p.status) {
        try {
          p = await setPostStatus(ws, p.id, p._version, target);
        } catch (e) {
          adopt(p);
          throw e;
        }
      }
      adopt(p);
    } catch (e) {
      if (!(e instanceof ApiError) || e.canceled) {
        if (e instanceof ApiError) return;
        setActionError(e instanceof Error ? e.message : "Saving failed.");
        return;
      }
      if (e.code === "CONCURRENT_MODIFICATION") {
        setConflict(action);
        return;
      }
      const field = typeof e.details?.field === "string" ? FIELD_OF[e.details.field] : undefined;
      if (field) setErrors((cur) => ({ ...cur, [field]: e.message }));
      else setActionError(e.recovery ? `${e.message} ${e.recovery}` : e.message);
    } finally {
      setBusy(false);
    }
  }

  async function reload() {
    setConflict(null);
    if (!saved) return;
    const fresh = await getPost(ws, saved.id);
    setSaved(fresh);
    setD(draftOf(fresh));
    qc.setQueryData(blogKeys.post(ws, fresh.id), fresh);
  }

  // Cmd/Ctrl+S saves without changing the status.
  const primary = actions[0];
  const runRef = useRef(run);
  runRef.current = run;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (matchesShortcut(e, "design.save")) {
        e.preventDefault();
        if (!busy && primary) void runRef.current(primary);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [busy, primary]);

  const sizeNote = bodySizeNote(bytes);
  const stats = readingStats(d.body);
  const bodyHint = [sizeNote ?? "Markdown. Images use workspace files: attachment:FILE_ID.", stats].filter(Boolean).join(" · ");
  const previewHtml =
    preview.data && themePref !== "system" ? preview.data.replace('<html lang="en">', `<html lang="en" data-theme="${themePref}">`) : (preview.data ?? "");
  const write = (
    <Field label="Body" hint={bodyHint} error={errors.body ?? inlineError ?? undefined}>
      <div className="mb-1.5 flex flex-wrap items-center gap-0.5 rounded-md border border-border bg-surface-2 p-1" role="toolbar" aria-label="Formatting">
        {TOOLBAR_GROUPS.map((group, gi) => (
          <div key={gi} className="inline-flex items-center gap-0.5">
            {gi > 0 && <ToolbarSeparator />}
            {group.map(({ kind, label, shortcut, Icon }) => (
              <IconButton key={kind} label={label} shortcut={shortcut} size="sm" onClick={() => onToolbar(kind)}>
                <Icon size={14} aria-hidden="true" />
              </IconButton>
            ))}
          </div>
        ))}
        <IconButton label="Upload image into post" size="sm" disabled={inlineProgress !== null} onClick={() => inlineFileRef.current?.click()}>
          <ImageUp size={14} aria-hidden="true" />
        </IconButton>
        <input
          ref={inlineFileRef}
          type="file"
          accept={ACCEPT_IMAGE}
          data-testid="inline-image-input"
          className="sr-only"
          tabIndex={-1}
          aria-hidden="true"
          onChange={(e) => void onInlineImage(e.target.files?.[0])}
        />
        {inlineProgress !== null && (
          <span role="status" className="ml-1.5 text-xs text-fg-muted">
            Uploading… {inlineProgress}%
          </span>
        )}
      </div>
      <Textarea ref={bodyRef} value={d.body} onKeyDown={onBodyKeyDown} onChange={(e) => set("body", e.target.value)} className="min-h-96 font-mono text-sm" spellCheck />
    </Field>
  );
  const previewPane = (
    <div className="flex min-h-96 flex-col">
      <p className="mb-1.5 text-sm font-medium text-fg" id="preview-label">
        Preview
      </p>
      {bytes > MAX_BODY_BYTES ? (
        <p className={warningBox}>The post is too long to preview.</p>
      ) : preview.isError ? (
        <p className={alertBox}>{preview.error.message}</p>
      ) : (
        // No allow-scripts: a sanitiser bypass cannot run in the app. allow-same-origin lets the
        // preview load the draft's images from the authenticated workspace file route.
        <iframe
          // eslint-disable-next-line no-restricted-syntax -- an iframe's title is its accessible name, not a tooltip
          title="Preview"
          sandbox="allow-same-origin"
          srcDoc={previewHtml}
          className="min-h-96 w-full flex-1 rounded-lg border border-border bg-surface shadow-xs"
        />
      )}
    </div>
  );

  const actionBar = (
    <div className={`flex flex-wrap items-center gap-2 ${compact ? "sticky bottom-0 -mx-4 border-t border-border bg-surface px-4 py-2" : ""}`}>
      {actions.map((a, i) => (
        <Button key={a} variant={i === 0 ? "primary" : "secondary"} disabled={busy} onClick={() => void run(a)} className={compact ? "min-h-11" : ""}>
          {ACTION_LABEL[a]}
        </Button>
      ))}
      {takenDown && <span className="text-sm text-fg-muted">Publishing waits for an administrator's review.</span>}
      {dirty && !busy && <span className="text-xs text-fg-muted">Unsaved changes</span>}
    </div>
  );

  return (
    <div className="mx-auto w-full max-w-6xl space-y-4 p-4 sm:p-6">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="m-0 text-xl font-semibold tracking-tight text-fg">{saved ? "Edit post" : "New post"}</h2>
        {saved && <Badge tone={CHIP_TONE[saved.chip]}>{CHIP_LABEL[saved.chip]}</Badge>}
      </div>
      {info.takedown ? <TakedownBanner takedown={info.takedown} scope="blog" /> : saved?.takedown && <TakedownBanner takedown={saved.takedown} scope="post" />}
      {saved?.problem && (
        <p role="status" className={`${warningBox} m-0`}>
          Needs attention: {saved.problem}
        </p>
      )}
      {actionError && (
        <p role="alert" className={`${alertBox} m-0`}>
          {actionError}
        </p>
      )}
      {!compact && actionBar}
      <Field label="Title" required error={errors.title} hint={`${d.title.length} / ${MAX_TITLE}`}>
        <Input ref={titleRef} value={d.title} maxLength={MAX_TITLE} onChange={(e) => onTitle(e.target.value)} />
      </Field>
      <SlugRow
        base={base}
        slug={shownSlug}
        editing={editingSlug}
        value={d.slug.slug}
        error={errors.slug}
        wasPublic={!!saved?.published_at && saved.slug !== d.slug.slug}
        taken={slugCheck.data?.available === false && slugToCheck === d.slug.slug}
        onEdit={() => {
          setEditingSlug(true);
          setD((cur) => ({ ...cur, slug: editSlug(cur.slug.slug || slugify(cur.title)) }));
        }}
        onChange={(v) => {
          setD((cur) => ({ ...cur, slug: editSlug(v) }));
          setErrors((e) => ({ ...e, slug: undefined }));
        }}
      />
      <Field label="Summary" error={errors.summary} hint={`Shown in the post list and link previews. ${d.summary.length} / ${MAX_SUMMARY}`}>
        <Textarea value={d.summary} maxLength={MAX_SUMMARY} rows={2} onChange={(e) => set("summary", e.target.value)} />
      </Field>
      <CoverField
        ws={ws}
        cover={d.cover}
        alt={d.coverAlt}
        coverError={errors.cover}
        altError={errors.coverAlt}
        onCover={(id) => {
          setD((cur) => ({ ...cur, cover: id, coverAlt: id ? cur.coverAlt : "" }));
          setErrors((e) => ({ ...e, cover: undefined, coverAlt: undefined }));
        }}
        onAlt={(v) => set("coverAlt", v)}
      />
      {saved?.published_at && (
        <Field label="Publish date" error={errors.publishedAt} hint="Shown on the post. Change it to back-date the post.">
          <Input type="datetime-local" value={d.publishedAt} onChange={(e) => set("publishedAt", e.target.value)} className="w-auto" />
        </Field>
      )}
      {compact ? (
        <Tabs.Root defaultValue="write">
          <Tabs.List aria-label="Editor" className={`${segTray} mb-3`}>
            {(["write", "preview"] as const).map((v) => (
              <Tabs.Trigger
                key={v}
                value={v}
                className="h-9 min-w-20 rounded px-3 text-sm font-medium text-fg-muted data-[state=active]:bg-surface data-[state=active]:text-fg data-[state=active]:shadow-xs"
              >
                {v === "write" ? "Write" : "Preview"}
              </Tabs.Trigger>
            ))}
          </Tabs.List>
          <Tabs.Content value="write">{write}</Tabs.Content>
          <Tabs.Content value="preview">{previewPane}</Tabs.Content>
        </Tabs.Root>
      ) : (
        <div className="grid grid-cols-2 gap-4">
          <div>{write}</div>
          {previewPane}
        </div>
      )}
      {compact && actionBar}
      {conflict && (
        <Dialog
          title="This post was changed elsewhere"
          description="Someone saved it after you opened it, for example in the datasheet."
          onClose={() => setConflict(null)}
          footer={
            <>
              <Button onClick={() => void reload()}>Reload the post</Button>
              <Button
                variant="primary"
                onClick={() => {
                  const a = conflict;
                  setConflict(null);
                  void run(a, true);
                }}
              >
                Keep my version
              </Button>
            </>
          }
        >
          <p className="m-0 text-sm text-fg-muted">Reload to see the saved version and lose your edits, or keep editing and overwrite it with yours.</p>
        </Dialog>
      )}
    </div>
  );
}

function SlugRow({
  base,
  slug,
  editing,
  value,
  error,
  wasPublic,
  taken,
  onEdit,
  onChange,
}: {
  base: string;
  slug: string;
  editing: boolean;
  value: string;
  error: string | undefined;
  wasPublic: boolean;
  taken: boolean;
  onEdit: () => void;
  onChange: (v: string) => void;
}) {
  const notes: ReactNode[] = [];
  if (taken) notes.push(`That address is taken; it will be saved as “${slug}”.`);
  else if (editing && slug !== value && slug) notes.push(`It will be saved as “${slug}”.`);
  if (wasPublic) notes.push("The old address will redirect to the new one.");
  if (!editing) {
    return (
      <div className="text-sm">
        <span className="text-fg-muted">Address: </span>
        <span className="break-all font-mono text-xs text-fg">
          {base}/{slug || "…"}
        </span>
        <Button size="sm" variant="ghost" className="ml-2" onClick={onEdit}>
          Edit
        </Button>
        {error && <p className="mt-1 text-xs font-medium text-danger">{error}</p>}
      </div>
    );
  }
  return (
    <Field label="Address" error={error} hint={notes.length ? notes.join(" ") : `${base}/${slug}`}>
      <Input value={value} maxLength={80} onChange={(e) => onChange(e.target.value)} className="font-mono" />
    </Field>
  );
}

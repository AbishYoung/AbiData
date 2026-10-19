import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Switch } from "radix-ui";
import { Copy, ExternalLink } from "lucide-react";
import { ApiError } from "@/lib/api";
import { copyText } from "@/lib/clipboard";
import { matchesShortcut } from "@/lib/shortcuts";
import { askToConfirm, Button, buttonClass, Card, Field, Input, Textarea } from "@/components/ui";
import { alertBox, successBox, warningBox } from "@/components/styles";
import { openChangeUsername } from "@/features/account/ChangeUsernameDialog";
import { blogKeys, setBlogPublished, updateBlog, type BlogInfo } from "./blogApi";

/** Blog settings (ADR 0017): title and intro, the published switch, the public address. */
export function BlogSettings({ ws, info }: { ws: string; info: BlogInfo }) {
  const qc = useQueryClient();
  const [title, setTitle] = useState(info.title);
  const [intro, setIntro] = useState(info.intro);
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);
  const dirty = title !== info.title || intro !== info.intro;

  const save = useMutation({
    meta: { inline: true },
    mutationFn: () => updateBlog(ws, { title, intro }),
    onSuccess: (next) => {
      qc.setQueryData(blogKeys.info(ws), next);
      setTitle(next.title);
      setIntro(next.intro);
      setSaved(true);
    },
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (matchesShortcut(e, "design.save")) {
        e.preventDefault();
        if (dirty && !save.isPending) {
          setSaved(false);
          save.mutate();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [dirty, save]);

  const publish = useMutation({
    meta: { inline: true },
    mutationFn: (on: boolean) => setBlogPublished(ws, on),
    onSuccess: (next) => qc.setQueryData(blogKeys.info(ws), next),
  });

  async function onSwitch(on: boolean) {
    const ok = await askToConfirm(
      on
        ? {
            title: "Turn your blog on?",
            message: "Your blog's home page and every published post become public at your blog's address.",
            confirmLabel: "Turn on",
          }
        : {
            title: "Turn your blog off?",
            message: "Your blog's home page and all its posts stop being served until you turn it on again.",
            confirmLabel: "Turn off",
            danger: true,
          },
    );
    if (ok) publish.mutate(on);
  }

  const errorText = (e: unknown) => (e instanceof ApiError ? [e.message, e.recovery].filter(Boolean).join(" ") : "Something went wrong.");

  return (
    <div className="mx-auto w-full max-w-3xl space-y-4 p-4 sm:p-6">
      <h2 className="m-0 text-xl font-semibold tracking-tight text-fg">Blog settings</h2>
      {!info.public_blogs_enabled && (
        <p role="status" className={`${warningBox} m-0`}>
          Public blogs are turned off on this server, so nothing is served, whatever the switches below say. An administrator can turn them on.
        </p>
      )}
      <Card title="Publishing">
        <div className="flex items-start gap-3">
          <Switch.Root
            id="blog-published"
            checked={info.published}
            disabled={publish.isPending}
            onCheckedChange={(on) => void onSwitch(on)}
            className="relative mt-0.5 h-6 w-11 shrink-0 cursor-pointer rounded-full bg-surface-3 transition-colors data-[state=checked]:bg-accent disabled:opacity-60"
          >
            <Switch.Thumb className="block h-5 w-5 translate-x-0.5 rounded-full bg-surface shadow-sm transition-transform data-[state=checked]:translate-x-[22px]" />
          </Switch.Root>
          <div className="min-w-0">
            <label htmlFor="blog-published" className="text-sm font-medium text-fg">
              Blog is published
            </label>
            <p className="m-0 mt-0.5 text-xs text-fg-muted">
              {info.published ? "Your home page and published posts are public." : "Nothing is public. Your posts stay private until you turn this on."}
            </p>
          </div>
        </div>
        {publish.isError && (
          <p role="alert" className={`${alertBox} mt-3`}>
            {errorText(publish.error)}
          </p>
        )}
        <div className="mt-4">
          <Field label="Public address" hint={info.published ? undefined : "View works once the blog is on."}>
            <Input readOnly value={info.public_url} className="font-mono" />
          </Field>
          <div className="mt-2 flex flex-wrap gap-2">
            <Button
              size="sm"
              onClick={() =>
                void copyText(info.public_url).then((ok) => {
                  setCopied(ok);
                  if (ok) setTimeout(() => setCopied(false), 2000);
                })
              }
            >
              <Copy size={14} aria-hidden="true" />
              {copied ? "Copied" : "Copy"}
            </Button>
            {info.published && info.public_blogs_enabled ? (
              <a href={info.public_url} target="_blank" rel="noopener noreferrer" className={buttonClass("secondary", "sm")}>
                <ExternalLink size={14} aria-hidden="true" />
                View
              </a>
            ) : (
              <Button size="sm" disabled>
                <ExternalLink size={14} aria-hidden="true" />
                View
              </Button>
            )}
          </div>
        </div>
        <p className="m-0 mt-4 text-sm text-fg-muted">
          Username: <span className="font-medium text-fg">@{info.username}</span>{" "}
          <Button size="sm" variant="ghost" onClick={() => openChangeUsername()}>
            Change username…
          </Button>
        </p>
      </Card>
      <Card title="Home page">
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            setSaved(false);
            save.mutate();
          }}
        >
          <Field label="Title" hint={`Shown at the top of your blog. ${title.length} / 200`}>
            <Input value={title} maxLength={200} onChange={(e) => (setTitle(e.target.value), setSaved(false))} />
          </Field>
          <Field label="Intro" hint={`Plain text under the title. Blank lines start new paragraphs. ${intro.length} / 2000`}>
            <Textarea value={intro} maxLength={2000} rows={5} onChange={(e) => (setIntro(e.target.value), setSaved(false))} />
          </Field>
          {save.isError && (
            <p role="alert" className={`${alertBox} m-0`}>
              {errorText(save.error)}
            </p>
          )}
          {saved && !dirty && (
            <p role="status" className={`${successBox} m-0`}>
              Saved.
            </p>
          )}
          <Button type="submit" variant="primary" disabled={!dirty || save.isPending}>
            Save
          </Button>
        </form>
      </Card>
      <p className="m-0 text-sm">
        <Link to="/w/$ws" params={{ ws }} className="text-fg-muted underline hover:text-fg">
          Open as workspace (advanced)
        </Link>
      </p>
    </div>
  );
}

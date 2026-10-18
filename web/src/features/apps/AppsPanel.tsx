import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, BookOpen, Copy, ExternalLink, Globe, Plus, RefreshCw, Trash2 } from "lucide-react";
import { SkeletonLines } from "@/components/EditorParts";
import { alertBox, badge, badgeDanger, badgeSuccess, badgeWarning, btn, btnDanger, btnPrimary, btnSm, card, fieldError, iconBtn, input, label, warningBox } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { Modal, selectClass } from "@/features/queries/Modal";
import { openDocs } from "@/features/docs/docsStore";
import { formKeys, listForms } from "@/features/forms/formsApi";
import { listReports, reportKeys } from "@/features/reports/reportsApi";
import { APP_STATE_LABEL, ITEM_STATUS_LABEL, appState, move, validateApp, waiting } from "./appLogic";
import { appKeys, approveApp, createApp, deleteApp, inputFromApp, listApps, rotateAppLink, updateApp, type App, type AppItemInput } from "./appsApi";

function message(e: unknown, fallback: string): string {
  return e instanceof ApiError ? e.message : fallback;
}

const STATE_BADGE = { live: badgeSuccess, off: badge, "taken-down": badgeDanger, "server-off": badgeWarning } as const;

/** Public apps section of the workspace settings (owners only; the server enforces it). */
export function AppsPanel({ ws }: { ws: string }) {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: appKeys.list(ws), queryFn: () => listApps(ws) });
  const [editing, setEditing] = useState<App | "new" | null>(null);
  const [confirm, setConfirm] = useState<{ app: App; action: "rotate" | "delete" } | null>(null);
  const [status, setStatus] = useState("");
  const done = (text: string) => {
    setStatus(text);
    void qc.invalidateQueries({ queryKey: appKeys.list(ws) });
  };
  const act = useMutation({
    mutationFn: async ({ app, action }: { app: App; action: "approve" | "toggle" }) =>
      action === "approve" ? approveApp(ws, app.id) : updateApp(ws, app.id, inputFromApp(app, { enabled: !app.enabled })),
    onSuccess: (a, v) => done(v.action === "approve" ? `Published the changes to ${a.name}.` : `${a.name} is now ${a.enabled ? "on" : "off"}.`),
    onError: (e) => setStatus(message(e, "The app could not be changed.")),
  });
  const serverOn = list.data?.enabled ?? true;

  return (
    <section aria-labelledby="apps-heading" data-testid="apps-panel" className={`${card} mb-5 p-5`}>
      <div className="mb-3 flex items-center gap-2.5">
        <span aria-hidden="true" className="inline-flex h-9 w-9 items-center justify-center rounded-lg bg-accent-soft text-accent">
          <Globe size={18} />
        </span>
        <h3 id="apps-heading" className="m-0 text-base font-semibold tracking-tight">
          Public apps
        </h3>
      </div>
      <p className="m-0 mb-3 text-sm text-fg-muted">
        An app puts forms and reports on a page anyone with its secret link can open, without an account. Forms only accept new records. When someone edits a
        published form, report or query, its page is paused until you publish the changes.
      </p>
      {!serverOn && (
        <p className={`${warningBox} mb-3`}>An administrator has turned public apps off on this server. Links do not work until they turn them back on.</p>
      )}
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <button type="button" className={btnPrimary} onClick={() => setEditing("new")} disabled={!serverOn}>
          <Plus size={14} aria-hidden="true" />
          New app
        </button>
        <button type="button" className={btn} onClick={() => openDocs("guide/apps")}>
          <BookOpen size={14} aria-hidden="true" />
          Public apps guide
        </button>
        <span role="status" aria-live="polite" className="text-sm text-fg-muted">
          {status}
        </span>
      </div>
      {list.isError && (
        <div role="alert" className={alertBox}>
          {message(list.error, "The apps could not be loaded.")}
        </div>
      )}
      {list.isPending && <SkeletonLines rows={3} label="Loading apps…" />}
      {list.data && list.data.items.length === 0 && <p className="m-0 text-sm text-fg-muted">No apps yet.</p>}
      <ul className="m-0 flex list-none flex-col gap-3 p-0">
        {(list.data?.items ?? []).map((a) => {
          const st = appState(a, serverOn);
          const pending = waiting(a);
          return (
            <li key={a.id} className="rounded-lg border border-border p-4" data-app-id={a.id} aria-labelledby={`app-${a.id}`}>
              <div className="mb-2 flex flex-wrap items-center gap-2">
                <h4 id={`app-${a.id}`} className="m-0 text-sm font-semibold">
                  {a.name}
                </h4>
                <span className={STATE_BADGE[st]}>{APP_STATE_LABEL[st]}</span>
              </div>
              <LinkRow app={a} />
              <ul className="m-0 mt-3 flex list-none flex-col gap-1 p-0 text-sm" aria-label={`Pages of ${a.name}`}>
                {(a.items ?? []).map((it) => (
                  <li key={it.id} className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{it.title}</span>
                    <span className="text-fg-muted">({it.kind})</span>
                    {a.start_item === it.id && <span className={badge}>Start page</span>}
                    <span className={it.status === "live" ? badgeSuccess : it.status === "changed" ? badgeWarning : badgeDanger}>{ITEM_STATUS_LABEL[it.status]}</span>
                    {it.problem && <span className="text-xs text-danger">{it.problem}</span>}
                  </li>
                ))}
              </ul>
              <div className="mt-3 flex flex-wrap gap-2">
                {pending.length > 0 && (
                  <button type="button" className={btnPrimary} onClick={() => act.mutate({ app: a, action: "approve" })} disabled={act.isPending}>
                    Publish changes ({pending.length})
                  </button>
                )}
                <button type="button" className={btn} onClick={() => setEditing(a)} disabled={a.taken_down}>
                  Edit
                </button>
                <button type="button" className={btn} onClick={() => act.mutate({ app: a, action: "toggle" })} disabled={act.isPending || a.taken_down}>
                  {a.enabled ? "Turn off" : "Turn on"}
                </button>
                <button type="button" className={btn} onClick={() => setConfirm({ app: a, action: "rotate" })}>
                  <RefreshCw size={14} aria-hidden="true" />
                  Replace link
                </button>
                <button type="button" className={btn} onClick={() => setConfirm({ app: a, action: "delete" })} aria-label={`Delete app ${a.name}`}>
                  <Trash2 size={14} aria-hidden="true" />
                  Delete
                </button>
              </div>
            </li>
          );
        })}
      </ul>
      {editing && (
        <AppDialog
          ws={ws}
          app={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={(a, created) => {
            setEditing(null);
            done(created ? `Published ${a.name}. Copy its link to share it.` : `Saved ${a.name}.`);
          }}
        />
      )}
      {confirm && (
        <ConfirmDialog
          ws={ws}
          app={confirm.app}
          action={confirm.action}
          onClose={() => setConfirm(null)}
          onDone={(text) => {
            setConfirm(null);
            done(text);
          }}
        />
      )}
    </section>
  );
}

function LinkRow({ app }: { app: App }) {
  const [copied, setCopied] = useState("");
  const link = app.link ?? "";
  return (
    <div className="flex flex-wrap items-center gap-2">
      <label htmlFor={`link-${app.id}`} className="sr-only">
        Link to {app.name}
      </label>
      <input id={`link-${app.id}`} className={`${input} max-w-xl flex-1 font-mono text-xs`} value={link} readOnly onFocus={(e) => e.currentTarget.select()} />
      <button
        type="button"
        className={btnSm}
        onClick={() =>
          void navigator.clipboard.writeText(link).then(
            () => setCopied("Link copied."),
            () => setCopied("Copying failed. Select the link and copy it by hand."),
          )
        }
      >
        <Copy size={12} aria-hidden="true" />
        Copy link
      </button>
      <a className={btnSm} href={link} target="_blank" rel="noreferrer noopener">
        <ExternalLink size={12} aria-hidden="true" />
        Open
      </a>
      <span role="status" aria-live="polite" className="text-xs text-fg-muted">
        {copied}
      </span>
    </div>
  );
}

function AppDialog({ ws, app, onClose, onSaved }: { ws: string; app: App | null; onClose: () => void; onSaved: (a: App, created: boolean) => void }) {
  const forms = useQuery({ queryKey: formKeys.list(ws), queryFn: () => listForms(ws) });
  const reports = useQuery({ queryKey: reportKeys.list(ws), queryFn: () => listReports(ws) });
  const [name, setName] = useState(app?.name ?? "");
  const [items, setItems] = useState<AppItemInput[]>(app ? inputFromApp(app).items ?? [] : []);
  const [start, setStart] = useState(app?.start_item ?? "");
  const [pick, setPick] = useState("");
  const [errors, setErrors] = useState<{ name?: string; items?: string }>({});
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), items, start_item: items.some((i) => i.id === start) ? start : "" };
      return app ? updateApp(ws, app.id, { ...body, enabled: app.enabled }) : createApp(ws, body);
    },
    onSuccess: (a) => onSaved(a, app === null),
  });
  const choices = [
    ...(forms.data ?? []).map((f) => ({ key: `form:${f.id}`, kind: "form" as const, id: f.id, name: f.name })),
    ...(reports.data ?? []).map((r) => ({ key: `report:${r.id}`, kind: "report" as const, id: r.id, name: r.name })),
  ];

  function submit(e: FormEvent) {
    e.preventDefault();
    const v = validateApp(name, items);
    setErrors(v);
    if (v.name || v.items) return;
    save.mutate();
  }

  return (
    <Modal title={app ? `Edit ${app.name}` : "New public app"} description="Choose the forms and reports visitors see, in menu order." onClose={onClose} wide>
      <form onSubmit={submit} noValidate aria-label={app ? "Edit app" : "New app"}>
        <label htmlFor="app-name" className={label}>
          Name
        </label>
        <input
          id="app-name"
          className={input}
          value={name}
          maxLength={100}
          onChange={(e) => setName(e.target.value)}
          aria-invalid={errors.name ? true : undefined}
          aria-describedby={errors.name ? "app-name-error" : undefined}
        />
        {errors.name && (
          <p id="app-name-error" className={fieldError}>
            {errors.name}
          </p>
        )}
        <fieldset className="mt-4 rounded-lg border border-border p-3">
          <legend className="px-1 text-sm font-medium">Pages</legend>
          {items.length === 0 && <p className="m-0 mb-2 text-sm text-fg-muted">No pages yet. Add a form or a report below.</p>}
          <ol className="m-0 flex list-none flex-col gap-2 p-0">
            {items.map((it, i) => {
              const what = choices.find((c) => c.kind === it.kind && c.id === it.object_id)?.name ?? "(deleted)";
              const key = it.id ?? `${it.kind}:${it.object_id}`;
              return (
                <li key={key} className="flex flex-wrap items-center gap-2">
                  <label className="sr-only" htmlFor={`title-${i}`}>
                    Menu title for {what}
                  </label>
                  <input
                    id={`title-${i}`}
                    className={`${input} w-48`}
                    value={it.title}
                    maxLength={100}
                    onChange={(e) => setItems(items.map((x, j) => (j === i ? { ...x, title: e.target.value } : x)))}
                  />
                  <span className="text-sm text-fg-muted">
                    {it.kind === "form" ? "Form" : "Report"}: {what}
                  </span>
                  {it.id && (
                    <label className="flex items-center gap-1 text-sm">
                      <input type="radio" name="start" checked={start === it.id || (!start && i === 0)} onChange={() => setStart(it.id ?? "")} />
                      Start page
                    </label>
                  )}
                  <span className="ml-auto flex gap-1">
                    <button type="button" className={iconBtn} aria-label={`Move ${it.title || what} up`} disabled={i === 0} onClick={() => setItems(move(items, i, -1))}>
                      <ArrowUp size={14} aria-hidden="true" />
                    </button>
                    <button
                      type="button"
                      className={iconBtn}
                      aria-label={`Move ${it.title || what} down`}
                      disabled={i === items.length - 1}
                      onClick={() => setItems(move(items, i, 1))}
                    >
                      <ArrowDown size={14} aria-hidden="true" />
                    </button>
                    <button type="button" className={iconBtn} aria-label={`Remove ${it.title || what}`} onClick={() => setItems(items.filter((_, j) => j !== i))}>
                      <Trash2 size={14} aria-hidden="true" />
                    </button>
                  </span>
                </li>
              );
            })}
          </ol>
          <div className="mt-3 flex flex-wrap items-end gap-2">
            <div>
              <label htmlFor="app-add" className={label}>
                Add a form or report
              </label>
              <select id="app-add" className={`${selectClass} w-64`} value={pick} onChange={(e) => setPick(e.target.value)}>
                <option value="">Choose…</option>
                {choices.map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.kind === "form" ? "Form" : "Report"}: {c.name}
                  </option>
                ))}
              </select>
            </div>
            <button
              type="button"
              className={btn}
              disabled={!pick}
              onClick={() => {
                const c = choices.find((x) => x.key === pick);
                if (c) setItems([...items, { kind: c.kind, object_id: c.id, title: c.name }]);
                setPick("");
              }}
            >
              <Plus size={14} aria-hidden="true" />
              Add
            </button>
          </div>
          {errors.items && <p className={fieldError}>{errors.items}</p>}
        </fieldset>
        {save.isError && (
          <div role="alert" className={`${alertBox} mt-3`}>
            {message(save.error, "The app could not be saved.")}
          </div>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose} disabled={save.isPending}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={save.isPending} aria-busy={save.isPending}>
            {save.isPending ? "Saving…" : app ? "Save" : "Publish"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function ConfirmDialog({ ws, app, action, onClose, onDone }: { ws: string; app: App; action: "rotate" | "delete"; onClose: () => void; onDone: (text: string) => void }) {
  const run = useMutation({
    mutationFn: () => (action === "rotate" ? rotateAppLink(ws, app.id).then(() => undefined) : deleteApp(ws, app.id)),
    onSuccess: () => onDone(action === "rotate" ? `${app.name} has a new link. The old one no longer works.` : `Deleted ${app.name}.`),
  });
  const rotate = action === "rotate";
  return (
    <Modal
      title={rotate ? `Replace the link to ${app.name}?` : `Delete ${app.name}?`}
      description={
        rotate
          ? "The current link stops working at once. Share the new link with the people who should still have access."
          : "The link stops working. The forms, reports and the records people submitted stay in the workspace."
      }
      onClose={onClose}
    >
      {run.isError && (
        <div role="alert" className={`${alertBox} mb-3`}>
          {message(run.error, "That did not work.")}
        </div>
      )}
      <div className="flex justify-end gap-2">
        <button type="button" className={btn} onClick={onClose} disabled={run.isPending}>
          Cancel
        </button>
        <button type="button" className={rotate ? btnPrimary : btnDanger} onClick={() => run.mutate()} disabled={run.isPending} aria-busy={run.isPending}>
          {rotate ? "Replace link" : "Delete app"}
        </button>
      </div>
    </Modal>
  );
}

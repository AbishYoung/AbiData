import { EmptyState, SkeletonLines, ToolbarDivider, docToolbar } from "@/components/EditorParts";
import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertDialog } from "radix-ui";
import { ChevronFirst, ChevronLast, ChevronLeft, ChevronRight, History, Plus, Save, Trash2, Undo2 } from "lucide-react";
import { Tip } from "@/components/Tip";
import { badgeWarning, btn, btnGhost, btnPrimary, dialogOverlay, dialogPanel } from "@/components/styles";
import { formKeys, getForm, type FormControl } from "../formsApi";
import { ParentContext, useFormEnv, type ParentLink } from "./env";
import {
  type Fields,
} from "./model";
import { FormInstance, type Registry } from "./FormInstance";

// The form's confirmation dialog, record toolbar and subform host.
// Split from FormInstance.tsx (ADR 0015, O4).

export function ConfirmDialog({ open, title, message, yes, no, onAnswer }: { open: boolean; title: string; message: string; yes: string; no: string; onAnswer: (a: boolean) => void }) {
  return (
    <AlertDialog.Root open={open} onOpenChange={(o) => (o ? undefined : onAnswer(false))}>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className={dialogOverlay} />
        <AlertDialog.Content className={`${dialogPanel} w-[min(28rem,calc(100vw-2rem))] p-5`}>
          <AlertDialog.Title className="text-base font-semibold">{title}</AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-sm">{message}</AlertDialog.Description>
          <div className="mt-4 flex justify-end gap-2">
            <AlertDialog.Cancel asChild>
              <button type="button" className={btn}>
                {no}
              </button>
            </AlertDialog.Cancel>
            <AlertDialog.Action asChild>
              <button type="button" className={btnPrimary} onClick={() => onAnswer(true)}>
                {yes}
              </button>
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}

export function Toolbar(props: {
  top: boolean;
  label: string;
  navText: string;
  count: number;
  index: number;
  rec: { isNew: boolean } | null;
  busy: boolean;
  dirty: boolean;
  canAdd: boolean;
  canDelete: boolean;
  canSave: boolean;
  onNav: (t: "first" | "prev" | "next" | "last" | "new") => void;
  onSave: () => void;
  onUndo: () => void;
  onDelete: () => void;
  canHistory: boolean;
  onHistory: () => void;
}) {
  const { top, label, count, index, rec, busy, dirty } = props;
  const small = "!px-2";
  return (
    <div role="toolbar" aria-label={`${label} actions`} className={docToolbar}>
      {top && (
        <>
          <Tip label="First record">
            <span className="inline-flex">
              <button type="button" className={`${btnGhost} ${small}`} aria-label="First record" disabled={busy || count === 0 || (index === 0 && !rec?.isNew)} onClick={() => props.onNav("first")}>
                <ChevronFirst size={16} aria-hidden="true" />
              </button>
            </span>
          </Tip>
          <Tip label="Previous record" shortcut="Alt+↑">
            <span className="inline-flex">
              <button type="button" className={`${btnGhost} ${small}`} aria-label="Previous record" aria-keyshortcuts="Alt+ArrowUp" disabled={busy || index === 0} onClick={() => props.onNav("prev")}>
                <ChevronLeft size={16} aria-hidden="true" />
              </button>
            </span>
          </Tip>
          <span className="min-w-28 px-1 text-center text-sm">{props.navText}</span>
          <Tip label="Next record" shortcut="Alt+↓">
            <span className="inline-flex">
              <button type="button" className={`${btnGhost} ${small}`} aria-label="Next record" aria-keyshortcuts="Alt+ArrowDown" disabled={busy || index >= count - 1} onClick={() => props.onNav("next")}>
                <ChevronRight size={16} aria-hidden="true" />
              </button>
            </span>
          </Tip>
          <Tip label="Last record">
            <span className="inline-flex">
              <button type="button" className={`${btnGhost} ${small}`} aria-label="Last record" disabled={busy || count === 0 || (index >= count - 1 && !rec?.isNew)} onClick={() => props.onNav("last")}>
                <ChevronLast size={16} aria-hidden="true" />
              </button>
            </span>
          </Tip>
        </>
      )}
      {top && <ToolbarDivider />}
      {props.canAdd && (
        <button type="button" className={btnGhost} aria-keyshortcuts="Control+Shift+N Meta+Shift+N" disabled={busy || !!(rec?.isNew && !dirty)} onClick={() => props.onNav("new")}>
          <Plus size={16} aria-hidden="true" />
          New {top ? "record" : "line"}
        </button>
      )}
      <button type="button" className={dirty ? btnPrimary : btnGhost} aria-keyshortcuts="Shift+Enter" disabled={busy || !props.canSave} onClick={props.onSave}>
        <Save size={16} aria-hidden="true" />
        Save
      </button>
      <button type="button" className={btnGhost} aria-keyshortcuts="Escape" disabled={busy || !dirty} onClick={props.onUndo}>
        <Undo2 size={16} aria-hidden="true" />
        Undo
      </button>
      {props.canDelete && (
        <button type="button" className={btnGhost} disabled={busy} onClick={props.onDelete}>
          <Trash2 size={16} aria-hidden="true" />
          Delete
        </button>
      )}
      {props.canHistory && (
        <button type="button" className={btnGhost} onClick={props.onHistory}>
          <History size={16} aria-hidden="true" />
          History
        </button>
      )}
      {dirty && <span className={`${badgeWarning} ml-1`}>Unsaved changes</span>}
    </div>
  );
}

/** A subform control: the child form in continuous view, linked to the master's record. */
export function SubformHost({
  control,
  fields,
  recordId,
  parentGone,
  registry,
}: {
  control: FormControl;
  fields: Fields;
  recordId: number | null;
  /** The master record was deleted by someone else: its rows are gone with it. */
  parentGone: boolean;
  registry: Registry;
}) {
  const env = useFormEnv();
  const sub = control.subform;
  const child = useQuery({
    queryKey: formKeys.one(env.ws, sub?.form_id ?? ""),
    enabled: !!sub,
    queryFn: () => getForm(env.ws, sub?.form_id ?? ""),
  });
  const id = useRef(Symbol("child"));
  const parent: ParentLink = useMemo(
    () => ({
      register: (flush) => {
        registry.flushers.add(flush);
        return () => {
          registry.flushers.delete(flush);
        };
      },
      setDirty: (d) => {
        if (registry.dirty.get(id.current) === d) return;
        if (d) registry.dirty.set(id.current, true);
        else registry.dirty.delete(id.current);
        registry.onDirty();
      },
      changed: () => registry.changed.current(),
    }),
    [registry],
  );
  useEffect(() => {
    const key = id.current;
    return () => {
      registry.dirty.delete(key);
      registry.onDirty();
    };
  }, [registry]);

  const name = control.caption || control.name;
  if (!sub) return null;
  if (child.isError) return <p role="alert" className="p-2 text-sm">{child.error.message}</p>;
  if (!child.data) return <SkeletonLines rows={2} label={`Loading ${name}…`} />;
  if (parentGone) return <EmptyState kind="form" compact title={`The parent record was deleted, so ${name} is no longer available.`} />;
  const masterCol = sub.link_master_column_id;
  const value = masterCol === "" || masterCol === "id" ? recordId : fields[masterCol];
  const blocked = value === null || value === undefined;
  const linkValue = blocked ? null : value;
  return (
    <ParentContext.Provider value={parent}>
      <div className="h-full overflow-hidden rounded-lg border border-border bg-surface">
        <FormInstance
          key={String(linkValue)}
          form={child.data}
          link={blocked ? null : { columnId: sub.link_child_column_id, value: linkValue }}
          blocked={blocked}
          parent={parent}
          label={name}
        />
      </div>
    </ParentContext.Provider>
  );
}

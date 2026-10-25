import { useEffect, useId, useRef, useState } from "react";
import { Dialog, DropdownMenu } from "radix-ui";
import { ChevronLeft, ChevronRight, Ellipsis, History, Trash2, X } from "lucide-react";
import type { Column } from "@/lib/schema";
import { badge, btn, btnPrimary, iconBtn, menu, menuItem } from "@/components/styles";
import { KindIcon } from "@/components/objectKinds";
import { parseIds, toValue } from "@/features/files/filesLogic";
import type { LookupInfo } from "./Editors";
import { hasChanges, type RowBuffer } from "./buffer";
import { type Row } from "./rowsApi";
import { draftOf, isReadOnlyColumn, kindOf, parseDraft } from "./values";
import { askToConfirm } from "@/components/ui";
import { Field } from "./RecordSheetFields";
import { dismissActiveInput, useSwipeGesture } from "./useSwipeGesture";
import { useDragToDismiss } from "./useDragToDismiss";

export function getPaginationDots(current: number, total: number, maxDots = 7) {
  if (total <= maxDots) {
    return Array.from({ length: total }, (_, i) => ({ index: i, active: i === current }));
  }
  const half = Math.floor(maxDots / 2);
  let start = Math.max(0, current - half);
  const end = Math.min(total - 1, start + maxDots - 1);
  if (end - start < maxDots - 1) {
    start = Math.max(0, end - maxDots + 1);
  }
  return Array.from({ length: end - start + 1 }, (_, i) => {
    const idx = start + i;
    return { index: idx, active: idx === current };
  });
}

export interface FieldChange {
  col: Column;
  value: unknown;
  display?: string;
}

/** What happened when the sheet asked for a save. */
export type SheetSave = "saved" | "conflict" | "failed";

interface Props {
  tableName: string;
  columns: readonly Column[];
  /** Position in the list; equals `total` for the new record. */
  index: number;
  total: number;
  isNew: boolean;
  row: Row | undefined;
  canEdit: boolean;
  buffer: RowBuffer | undefined;
  lookupFor: (col: Column) => LookupInfo | undefined;
  /** Number field that "Use my location" may fill with the GPS accuracy. */
  accuracyCol?: Column;
  /** `base` is the record as the sheet showed it when editing began (its version is the base for If-Match). */
  onSave: (changes: FieldChange[], base: Row | undefined) => Promise<SheetSave>;
  onGo: (index: number) => void;
  onClose: () => void;
  onDelete: () => void;
  onHistory: () => void;
}

export type Draft = { text: string } | { value: unknown; display?: string };

function initialDraft(col: Column, row: Row | undefined): Draft {
  const kind = kindOf(col);
  const value = row?.fields[col.id];
  if (kind === "boolean") return { value: value === true ? true : value === false ? false : null };
  if (kind === "lookup") return { value: value ?? null, display: row?.display?.[col.id] };
  if (kind === "attachments") return { value: toValue(parseIds(value)) };
  if (kind === "geo_point" || kind === "geo_area") return { value: value ?? null };
  return { text: draftOf(col, value) };
}

/**
 * Full-screen record view for phones: every field of one record, with previous/next and save.
 * Edits go through the datasheet's own buffer and save path (validation, If-Match, conflicts).
 */
export function RecordSheet(props: Props) {
  const { tableName, columns, index, total, isNew, row, canEdit, buffer, onClose } = props;
  const formId = useId();
  const loading = !isNew && !row;
  const key = isNew ? "new" : (row?.id ?? `loading-${index}`);
  const [form, setForm] = useState<{ key: string | number; base: Row | undefined; drafts: Record<string, Draft>; touched: ReadonlySet<string> }>(() => ({
    key,
    base: row,
    drafts: Object.fromEntries(columns.map((c) => [c.id, initialDraft(c, row)])),
    touched: new Set(),
  }));
  const [local, setLocal] = useState<{ key: string | number; errors: Record<string, string> }>({ key, errors: {} });
  const [busy, setBusy] = useState(false);
  const [awaitingMerge, setAwaitingMerge] = useState(false);
  /** A photo is still uploading: saving now would leave it out of the record. */
  const [uploading, setUploading] = useState(false);

  // A different record (or one that finished loading) starts from its own values.
  if (form.key !== key) {
    setForm({ key, base: row, drafts: Object.fromEntries(columns.map((c) => [c.id, initialDraft(c, row)])), touched: new Set() });
    setLocal({ key, errors: {} });
  }
  const drafts = form.drafts;
  const errors = { ...(local.key === key ? local.errors : {}), ...(buffer?.errors ?? {}) };
  const dirty = form.touched.size > 0;
  const isDirty = dirty || hasChanges(buffer);
  const formRef = useRef<HTMLFormElement | null>(null);

  // After a conflict the merge dialog resolves the buffer; the record is then up to date, so leave.
  useEffect(() => {
    if (awaitingMerge && !buffer) onClose();
  }, [awaitingMerge, buffer, onClose]);

  function touch(colId: string, draft: Draft) {
    setForm((f) => ({ ...f, drafts: { ...f.drafts, [colId]: draft }, touched: new Set(f.touched).add(colId) }));
    setLocal((l) => {
      const { [colId]: _drop, ...rest } = l.errors;
      void _drop;
      return { ...l, errors: rest };
    });
  }

  /** Parses the touched fields and saves them. True when the caller may move on. */
  async function save(): Promise<boolean> {
    if (!canEdit || !dirty) return true;
    const changes: FieldChange[] = [];
    const bad: Record<string, string> = {};
    for (const col of columns) {
      if (!form.touched.has(col.id) || isReadOnlyColumn(col)) continue;
      const d = drafts[col.id];
      if (d === undefined) continue;
      if ("text" in d) {
        const r = parseDraft(col, d.text);
        if (r.ok) changes.push({ col, value: r.value });
        else bad[col.id] = r.message;
      } else {
        changes.push({ col, value: d.value, ...(d.display !== undefined ? { display: d.display } : {}) });
      }
    }
    if (Object.keys(bad).length > 0) {
      setLocal({ key, errors: bad });
      document.getElementById(`${formId}-${Object.keys(bad)[0]}`)?.focus();
      return false;
    }
    setBusy(true);
    try {
      const outcome = await props.onSave(changes, form.base);
      if (outcome === "conflict") setAwaitingMerge(true);
      return outcome === "saved";
    } finally {
      setBusy(false);
    }
  }

  /** Closes the sheet, first confirming when there are unsaved edits. Resolves to whether it closed. */
  async function close(): Promise<boolean> {
    if (isDirty && !(await askToConfirm({ title: "Discard your changes?", message: "Your changes to this record are not saved.", confirmLabel: "Discard changes", danger: true }))) return false;
    onClose();
    return true;
  }

  // Dragging the grab bar or header down dismisses the sheet; the buttons remain the keyboard path.
  const { sheetRef, handleProps } = useDragToDismiss({ enabled: true, onRequestClose: close });

  async function go(next: number) {
    if (dirty && !(await save())) return;
    props.onGo(next);
  }

  async function swipeGo(next: number) {
    if (isDirty && !(await askToConfirm({ title: "Discard your changes?", message: "Your changes to this record are not saved.", confirmLabel: "Discard changes", danger: true }))) return;
    props.onGo(next);
  }

  useSwipeGesture({
    containerRef: formRef,
    canSwipeLeft: !isNew && index < total - 1,
    canSwipeRight: isNew ? total > 0 : index > 0,
    onSwipeLeft: () => void swipeGo(index + 1),
    onSwipeRight: () => void swipeGo(index - 1),
  });

  const position = isNew ? "New record" : `Record ${(index + 1).toLocaleString("en-US")} of ${total.toLocaleString("en-US")}`;

  return (
    <Dialog.Root open onOpenChange={(o) => (o ? undefined : void close())}>
      <Dialog.Portal>
        <Dialog.Content
          ref={sheetRef}
          aria-describedby={undefined}
          data-presentation="fullscreen"
          onPointerDown={(e) => dismissActiveInput(e.target)}
          onEscapeKeyDown={(e) => {
            if (isDirty) {
              e.preventDefault();
              void close();
            }
          }}
          className="fixed inset-0 z-50 flex flex-col bg-bg text-fg touch-pan-y"
        >
          <div
            {...handleProps}
            className="shrink-0 touch-none border-b border-border bg-surface pl-[max(0.25rem,env(safe-area-inset-left))] pr-[max(0.25rem,env(safe-area-inset-right))] pt-[env(safe-area-inset-top)]"
          >
            <div aria-hidden="true" className="mx-auto my-2 h-1.5 w-10 rounded-full bg-border-strong/40" data-testid="sheet-grab-bar" />
            <div className="flex items-center gap-0.5">
            <button type="button" className={iconBtn} aria-label="Close record" onClick={() => void close()}>
              <X size={20} aria-hidden="true" />
            </button>
            <div className="min-w-0 flex-1 px-1">
              <Dialog.Title className="m-0 truncate text-sm font-semibold text-fg">{position}</Dialog.Title>
              <p className="m-0 flex items-center gap-1 truncate text-xs text-fg-muted">
                <KindIcon kind="table" size="sm" />
                <span className="truncate">{tableName}</span>
                {!canEdit && <span className={badge}>Read-only</span>}
              </p>
            </div>
            <button type="button" className={iconBtn} aria-label="Previous record" disabled={isNew ? total === 0 : index <= 0} onClick={() => void go(index - 1)}>
              <ChevronLeft size={20} aria-hidden="true" />
            </button>
            <button type="button" className={iconBtn} aria-label="Next record" disabled={isNew || index >= total - 1} onClick={() => void go(index + 1)}>
              <ChevronRight size={20} aria-hidden="true" />
            </button>
            {!isNew && row && (
              <DropdownMenu.Root>
                <DropdownMenu.Trigger className={iconBtn} aria-label="Record actions">
                  <Ellipsis size={20} aria-hidden="true" />
                </DropdownMenu.Trigger>
                <DropdownMenu.Portal>
                  <DropdownMenu.Content align="end" sideOffset={4} className={`ui-menu z-[60] min-w-48 ${menu} text-fg`}>
                    <DropdownMenu.Item className={menuItem} onSelect={props.onHistory}>
                      <History size={16} aria-hidden="true" />
                      History
                    </DropdownMenu.Item>
                    {canEdit && (
                      <DropdownMenu.Item className={`${menuItem} text-danger`} onSelect={props.onDelete}>
                        <Trash2 size={16} aria-hidden="true" />
                        Delete record
                      </DropdownMenu.Item>
                    )}
                  </DropdownMenu.Content>
                </DropdownMenu.Portal>
              </DropdownMenu.Root>
            )}
            </div>
          </div>

          <form
            id={formId}
            ref={formRef}
            noValidate
            className="swipe-card min-h-0 flex-1 space-y-4 overflow-y-auto overscroll-contain px-4 py-4 touch-pan-y"
            onSubmit={(e) => {
              e.preventDefault();
              void save().then((ok) => {
                if (ok) onClose();
              });
            }}
          >
            {loading ? (
              <div role="status" className="space-y-4">
                <span className="sr-only">Loading record</span>
                {columns.slice(0, 5).map((c) => (
                  <div key={c.id} aria-hidden="true" className="h-12 animate-pulse rounded-md bg-surface-3" />
                ))}
              </div>
            ) : (
              columns.map((col) => (
                <Field
                  key={col.id}
                  id={`${formId}-${col.id}`}
                  col={col}
                  draft={drafts[col.id]}
                  row={row}
                  canEdit={canEdit}
                  error={errors[col.id]}
                  lookup={props.lookupFor(col)}
                  accuracyCol={props.accuracyCol}
                  onChange={(d) => touch(col.id, d)}
                  onBusy={setUploading}
                  onAccuracy={(c, v) => touch(c.id, { text: String(v) })}
                />
              ))
            )}
          </form>

          {total > 1 && !isNew && (
            <div
              role="tablist"
              aria-label="Record pagination"
              className="flex shrink-0 items-center justify-center gap-1 border-t border-border/50 bg-surface/30 px-3 py-1.5 touch-pan-y"
            >
              {getPaginationDots(index, total).map((dot) => (
                <button
                  key={dot.index}
                  type="button"
                  role="tab"
                  data-dense
                  aria-selected={dot.active}
                  aria-label={`Record ${dot.index + 1} of ${total}`}
                  className="flex h-8 w-6 items-center justify-center"
                  onClick={() => void go(dot.index)}
                >
                  <span
                    className={`h-2 rounded-full transition-all duration-150 ${
                      dot.active ? "w-5 bg-accent" : "w-2 bg-border hover:bg-fg-muted"
                    }`}
                  />
                </button>
              ))}
            </div>
          )}

          {canEdit && (
            <div className="flex shrink-0 gap-2 border-t border-border bg-surface px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
              <button type="button" className={`${btn} flex-1`} onClick={() => void close()}>
                Cancel
              </button>
              <button type="submit" form={formId} className={`${btnPrimary} flex-1`} disabled={busy || loading || !dirty || uploading}>
                {busy ? "Saving…" : uploading ? "Uploading…" : isNew ? "Add record" : "Save"}
              </button>
            </div>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

import { useEffect, useId, useState, type ReactNode } from "react";
import { Dialog, DropdownMenu } from "radix-ui";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronLeft, ChevronRight, Ellipsis, History, Trash2, X } from "lucide-react";
import type { Column } from "@/lib/schema";
import { badge, btn, btnPrimary, fieldError, hint, iconBtn, input, label as labelClass, menu, menuItem, textarea } from "@/components/styles";
import { KindIcon } from "@/components/objectKinds";
import type { LookupInfo } from "./Editors";
import type { RowBuffer } from "./buffer";
import { searchParent, type Row } from "./rowsApi";
import { draftOf, formatValue, isEmpty, isReadOnlyColumn, kindOf, parseDraft, valuesEqual, type ValueKind } from "./values";

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
  /** `base` is the record as the sheet showed it when editing began (its version is the base for If-Match). */
  onSave: (changes: FieldChange[], base: Row | undefined) => Promise<SheetSave>;
  onGo: (index: number) => void;
  onClose: () => void;
  onDelete: () => void;
  onHistory: () => void;
}

type Draft = { text: string } | { value: unknown; display?: string };

function inputTypeOf(kind: ValueKind): string {
  switch (kind) {
    case "date":
      return "date";
    case "datetime":
      return "datetime-local";
    case "hyperlink":
      return "url";
    default:
      return "text";
  }
}

function inputModeOf(kind: ValueKind): "numeric" | "decimal" | "url" | undefined {
  switch (kind) {
    case "integer":
      return "numeric";
    case "number":
    case "currency":
      return "decimal";
    case "hyperlink":
      return "url";
    default:
      return undefined;
  }
}

function initialDraft(col: Column, row: Row | undefined): Draft {
  const kind = kindOf(col);
  const value = row?.fields[col.id];
  if (kind === "boolean") return { value: value === true ? true : value === false ? false : null };
  if (kind === "lookup") return { value: value ?? null, display: row?.display?.[col.id] };
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

  // A different record (or one that finished loading) starts from its own values.
  if (form.key !== key) {
    setForm({ key, base: row, drafts: Object.fromEntries(columns.map((c) => [c.id, initialDraft(c, row)])), touched: new Set() });
    setLocal({ key, errors: {} });
  }
  const drafts = form.drafts;
  const errors = { ...(local.key === key ? local.errors : {}), ...(buffer?.errors ?? {}) };
  const dirty = form.touched.size > 0;

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

  function close() {
    if (dirty && !window.confirm("Discard your changes to this record?")) return;
    onClose();
  }

  async function go(next: number) {
    if (dirty && !(await save())) return;
    props.onGo(next);
  }

  const position = isNew ? "New record" : `Record ${(index + 1).toLocaleString("en-US")} of ${total.toLocaleString("en-US")}`;

  return (
    <Dialog.Root open onOpenChange={(o) => (o ? undefined : close())}>
      <Dialog.Portal>
        <Dialog.Content
          aria-describedby={undefined}
          data-presentation="fullscreen"
          onEscapeKeyDown={(e) => {
            if (dirty) {
              e.preventDefault();
              close();
            }
          }}
          className="fixed inset-0 z-50 flex flex-col bg-bg text-fg"
        >
          <div className="flex shrink-0 items-center gap-0.5 border-b border-border bg-surface pl-[max(0.25rem,env(safe-area-inset-left))] pr-[max(0.25rem,env(safe-area-inset-right))] pt-[env(safe-area-inset-top)]">
            <button type="button" className={iconBtn} aria-label="Close record" onClick={close}>
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

          <form
            id={formId}
            noValidate
            className="min-h-0 flex-1 space-y-4 overflow-y-auto overscroll-contain px-4 py-4"
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
                  onChange={(d) => touch(col.id, d)}
                />
              ))
            )}
          </form>

          {canEdit && (
            <div className="flex shrink-0 gap-2 border-t border-border bg-surface px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
              <button type="button" className={`${btn} flex-1`} onClick={close}>
                Cancel
              </button>
              <button type="submit" form={formId} className={`${btnPrimary} flex-1`} disabled={busy || loading || !dirty}>
                {busy ? "Saving…" : isNew ? "Add record" : "Save"}
              </button>
            </div>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function Field({
  id,
  col,
  draft,
  row,
  canEdit,
  error,
  lookup,
  onChange,
}: {
  id: string;
  col: Column;
  draft: Draft | undefined;
  row: Row | undefined;
  canEdit: boolean;
  error: string | undefined;
  lookup: LookupInfo | undefined;
  onChange: (d: Draft) => void;
}) {
  const kind = kindOf(col);
  const errId = `${id}-err`;
  const readOnly = !canEdit || isReadOnlyColumn(col);

  if (readOnly) {
    const value = col.field_type === "autonumber" ? row?.id : row?.fields[col.id];
    return (
      <div>
        <p className={labelClass}>{col.display_name}</p>
        <p className="m-0 min-h-6 break-words text-base text-fg" data-testid={`record-field-${col.display_name}`}>
          {isEmpty(value) && kind !== "lookup" ? <span className="text-fg-subtle">—</span> : formatValue(col, value, row?.display?.[col.id]) || <span className="text-fg-subtle">—</span>}
        </p>
        {!canEdit ? null : <p className={hint}>Calculated automatically.</p>}
      </div>
    );
  }

  let control: ReactNode;
  if (kind === "boolean") {
    const v = draft && "value" in draft ? draft.value : null;
    control = (
      <label className="flex min-h-11 items-center gap-3 text-base text-fg">
        <input
          id={id}
          type="checkbox"
          checked={v === true}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errId : undefined}
          onChange={(e) => onChange({ value: e.target.checked })}
          className="h-5 w-5 accent-accent"
        />
        {col.display_name}
      </label>
    );
    return (
      <div>
        {control}
        {error && (
          <p id={errId} role="alert" className={fieldError}>
            {error}
          </p>
        )}
      </div>
    );
  }
  if (kind === "lookup") {
    const d = draft && "value" in draft ? draft : { value: null };
    control = <LookupField id={id} col={col} lookup={lookup} value={d.value} display={"display" in d ? d.display : undefined} error={error} errId={errId} onChange={(value, display) => onChange({ value, ...(display !== undefined ? { display } : {}) })} />;
  } else if (kind === "longtext") {
    control = (
      <textarea
        id={id}
        rows={5}
        value={draft && "text" in draft ? draft.text : ""}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onChange={(e) => onChange({ text: e.target.value })}
        className={textarea}
      />
    );
  } else {
    const maxLength = kind === "text" ? col.type_options.max_length : undefined;
    control = (
      <input
        id={id}
        type={inputTypeOf(kind)}
        inputMode={inputModeOf(kind)}
        step={kind === "datetime" ? "any" : undefined}
        maxLength={maxLength && maxLength > 0 ? maxLength : undefined}
        value={draft && "text" in draft ? draft.text : ""}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onChange={(e) => onChange({ text: e.target.value })}
        className={input}
      />
    );
  }
  return (
    <div>
      <label htmlFor={id} className={labelClass}>
        {col.display_name}
      </label>
      {control}
      {error && (
        <p id={errId} role="alert" className={fieldError}>
          {error}
        </p>
      )}
    </div>
  );
}

function LookupField({
  id,
  col,
  lookup,
  value,
  display,
  error,
  errId,
  onChange,
}: {
  id: string;
  col: Column;
  lookup: LookupInfo | undefined;
  value: unknown;
  display: string | undefined;
  error: string | undefined;
  errId: string;
  onChange: (value: unknown, display?: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const results = useQuery({
    queryKey: ["lookup", lookup?.ws, lookup?.parentTableId, lookup?.displayColumnId, query],
    enabled: open && lookup !== undefined,
    staleTime: 5_000,
    queryFn: ({ signal }) =>
      lookup ? searchParent(lookup.ws, lookup.parentTableId, lookup.displayColumnId, query, signal) : Promise.resolve([]),
  });
  const options = (results.data ?? []).map((r) => ({ id: r.id, text: String(r.fields[lookup?.displayColumnId ?? ""] ?? r.id) }));
  const shown = isEmpty(value) ? "" : (display ?? String(value));
  return (
    <div>
      <button
        id={id}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onClick={() => setOpen((o) => !o)}
        className={`${input} flex items-center justify-between gap-2 text-left`}
      >
        <span className={`truncate ${shown ? "text-fg" : "text-fg-subtle"}`}>{shown || "Choose…"}</span>
        <ChevronDown size={16} aria-hidden="true" className="shrink-0 text-fg-subtle" />
      </button>
      {open && (
        <div className="mt-2 rounded-lg border border-border bg-surface p-2 shadow-sm">
          <input
            type="search"
            autoFocus
            value={query}
            aria-label={`Search ${col.display_name}`}
            placeholder="Search"
            onChange={(e) => setQuery(e.target.value)}
            className={input}
          />
          <ul role="listbox" aria-label={`${col.display_name} choices`} className="m-0 mt-1 max-h-56 list-none overflow-y-auto p-0">
            {!isEmpty(value) && (
              <li role="option" aria-selected={false} className="m-0">
                <button
                  type="button"
                  className="flex min-h-11 w-full items-center rounded-md px-2 text-left text-sm text-danger hover:bg-surface-2"
                  onClick={() => {
                    onChange(null);
                    setOpen(false);
                  }}
                >
                  Clear value
                </button>
              </li>
            )}
            {options.map((o) => (
              <li key={o.id} role="option" aria-selected={String(o.id) === String(value)} className="m-0">
                <button
                  type="button"
                  className="flex min-h-11 w-full items-center rounded-md px-2 text-left text-sm text-fg hover:bg-surface-2"
                  onClick={() => {
                    if (!valuesEqual(col, o.id, value) || display !== o.text) onChange(o.id, o.text);
                    setOpen(false);
                  }}
                >
                  {o.text}
                </button>
              </li>
            ))}
          </ul>
          {options.length === 0 && <p className="m-0 px-2 py-1 text-sm text-fg-muted">{results.isPending ? "Searching…" : "No matching records"}</p>}
        </div>
      )}
    </div>
  );
}

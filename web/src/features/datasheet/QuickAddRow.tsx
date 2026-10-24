import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { Plus, TriangleAlert } from "lucide-react";
import type { Column } from "@/lib/schema";
import { kindOf, isNumericKind } from "./values";
import { QUICK_ADD_HEIGHT, buildQuickAdd, quickAddable } from "./quickAdd";

const ROW_HEADER_WIDTH = 76;

interface Props {
  columns: readonly Column[];
  widths: readonly number[];
  totalWidth: number;
  /** Top offset (the column header height) the strip sticks at. */
  top: number;
  /** Saves the record. Resolves to inline field errors, or null when it was saved. */
  onSubmit: (fields: Record<string, unknown>) => Promise<Record<string, string> | null>;
  onClose: () => void;
}

function inputType(col: Column): string {
  switch (kindOf(col)) {
    case "date":
      return "date";
    case "datetime":
      return "datetime-local";
    default:
      return "text";
  }
}

/**
 * A strip pinned under the column headers for adding records without scrolling to the bottom of the
 * grid (Shift+Mod+N). Enter saves the record and clears the strip for the next one; Escape closes it.
 */
export function QuickAddRow({ columns, widths, totalWidth, top, onSubmit, onClose }: Props) {
  const baseId = useId();
  const rowRef = useRef<HTMLDivElement>(null);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  function focusFirst() {
    rowRef.current?.querySelector<HTMLInputElement>("input:not(:disabled)")?.focus();
  }

  useEffect(() => {
    focusFirst();
  }, []);

  function set(id: string, value: string) {
    setDrafts((d) => ({ ...d, [id]: value }));
    if (errors[id]) setErrors((e) => Object.fromEntries(Object.entries(e).filter(([k]) => k !== id)));
  }

  function focusError(found: Record<string, string>) {
    const first = columns.find((c) => found[c.id]);
    if (first) document.getElementById(`${baseId}-${first.id}`)?.focus();
  }

  async function submit() {
    if (busy) return;
    const built = buildQuickAdd(columns, drafts);
    if (!built.ok) {
      setErrors(built.errors);
      focusError(built.errors);
      return;
    }
    if (Object.keys(built.fields).length === 0) {
      setErrors({});
      return;
    }
    setBusy(true);
    const failed = await onSubmit(built.fields);
    setBusy(false);
    if (failed) {
      setErrors(failed);
      focusError(failed);
      return;
    }
    setDrafts({});
    setErrors({});
    focusFirst();
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    // The strip has its own keys: nothing reaches the datasheet's handler (typing, arrows, paste).
    e.stopPropagation();
    if (e.nativeEvent.isComposing) return;
    if (e.key === "Enter") {
      e.preventDefault();
      void submit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }

  const firstError = columns.map((c) => errors[c.id]).find((m) => m !== undefined);

  return (
    <div role="rowgroup" className="sticky z-[15]" style={{ top, width: totalWidth, height: QUICK_ADD_HEIGHT }}>
      <div
        ref={rowRef}
        role="row"
        aria-label="Quick add record"
        onKeyDown={onKeyDown}
        onPaste={(e) => e.stopPropagation()}
        onContextMenu={(e) => e.stopPropagation()}
        className="flex border-b border-accent/40 bg-accent-soft"
        style={{ height: QUICK_ADD_HEIGHT }}
      >
        <div
          role="rowheader"
          style={{ width: ROW_HEADER_WIDTH }}
          className="sticky left-0 z-10 flex shrink-0 items-center justify-center gap-1 border-r border-border bg-accent-soft text-xs font-semibold text-accent"
        >
          <Plus size={14} aria-hidden="true" />
          Add
        </div>
        {columns.map((col, i) => {
          const id = `${baseId}-${col.id}`;
          const ok = quickAddable(col);
          const kind = kindOf(col);
          const error = errors[col.id];
          const common = {
            id,
            "aria-label": `${col.display_name}, new record`,
            "aria-invalid": error ? true : undefined,
            "aria-describedby": error ? `${id}-err` : undefined,
            disabled: !ok,
          } as const;
          return (
            <div
              key={col.id}
              role="gridcell"
              style={{ width: widths[i] }}
              className="relative flex shrink-0 items-center border-r border-border/70 px-1.5"
            >
              {kind === "boolean" ? (
                <input
                  {...common}
                  type="checkbox"
                  checked={drafts[col.id] === "true"}
                  onChange={(e) => set(col.id, e.target.checked ? "true" : "")}
                  className="size-4 accent-accent"
                />
              ) : (
                <input
                  {...common}
                  type={inputType(col)}
                  inputMode={kind === "integer" ? "numeric" : kind === "number" || kind === "currency" ? "decimal" : undefined}
                  value={ok ? (drafts[col.id] ?? "") : ""}
                  placeholder={ok ? "" : "n/a"}
                  autoComplete="off"
                  readOnly={busy}
                  onChange={(e) => set(col.id, e.target.value)}
                  className={`h-7 w-full min-w-0 rounded border bg-surface px-2 text-sm text-fg placeholder:text-fg-subtle focus:border-focus focus:outline-none focus:ring-1 focus:ring-focus disabled:bg-surface-2/60 disabled:text-fg-muted ${
                    error ? "border-danger" : "border-border-strong/70"
                  } ${isNumericKind(kind) ? "text-right tabular-nums" : ""}`}
                />
              )}
              {error && (
                <span id={`${id}-err`} className="sr-only">
                  {error}
                </span>
              )}
            </div>
          );
        })}
      </div>
      {firstError && (
        <p
          role="alert"
          className="absolute left-20 top-full z-30 mt-1 flex max-w-96 items-start gap-1 rounded border border-danger bg-danger-soft px-2 py-1 text-xs text-fg shadow"
        >
          <TriangleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0 text-danger" />
          {firstError}
        </p>
      )}
    </div>
  );
}

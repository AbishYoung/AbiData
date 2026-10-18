import { useEffect, useId, useImperativeHandle, useRef, useState, type KeyboardEvent, type Ref } from "react";
import { Popover } from "radix-ui";
import { useQuery } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";
import type { Column } from "@/lib/schema";
import { AttachmentsDialog } from "@/features/files/AttachmentsDialog";
import { AttachmentThumbs, useFilesWorkspace } from "@/features/files/FileParts";
import { maxFilesOf, parseIds } from "@/features/files/filesLogic";
import { draftOf, isEmpty, parseDraft, type ValueKind } from "./values";
import { searchParent } from "./rowsApi";
import { GeoValueDialog } from "@/components/maps/lazy";
import { accuracyValue } from "@/lib/geo/locate";

/** Where focus goes after an edit is committed. */
export type Move = "down" | "right" | "left" | "stay" | "save";

export interface EditorHandle {
  /** Commits the current draft (used when the user clicks another cell). False when the draft is invalid. */
  commit: (move: Move) => boolean;
}

export interface LookupInfo {
  ws: string;
  parentTableId: string;
  displayColumnId: string;
}

export interface EditorProps {
  col: Column;
  kind: ValueKind;
  initialValue: unknown;
  initialDisplay?: string;
  /** Character that started the edit by typing, or null for Enter / F2 / double click. */
  starter: string | null;
  lookup?: LookupInfo;
  /** The record cannot be edited: an attachments dialog only shows the files. */
  readOnly?: boolean;
  /** A number field that "Use my location" may fill with the GPS accuracy (point fields). */
  accuracyCol?: Column;
  /** `also` carries changes to other fields of the record (the GPS accuracy). */
  onCommit: (value: unknown, display: string | undefined, move: Move, also?: { col: Column; value: unknown }[]) => void;
  onCancel: () => void;
  ref?: Ref<EditorHandle>;
}

type KeyAction = Move | "cancel" | null;

function editorKeyAction(e: KeyboardEvent): KeyAction {
  if (e.nativeEvent.isComposing) return null;
  switch (e.key) {
    case "Escape":
      return "cancel";
    case "Tab":
      return e.shiftKey ? "left" : "right";
    case "Enter":
      return e.shiftKey ? "save" : "down";
    default:
      return null;
  }
}

function ErrorNote({ id, message }: { id: string; message: string }) {
  return (
    <span
      id={id}
      role="alert"
      className="absolute left-0 top-full z-30 mt-1 flex w-max max-w-80 text-left items-start gap-1 rounded border border-danger bg-danger-soft px-2 py-1 text-xs text-fg shadow"
    >
      <TriangleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0 text-danger" />
      <span>{message}</span>
    </span>
  );
}

function inputType(kind: ValueKind): string {
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

function inputMode(kind: ValueKind): "numeric" | "decimal" | "url" | undefined {
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

function SingleLineEditor({ col, kind, initialValue, starter, onCommit, onCancel, ref }: EditorProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const done = useRef(false);
  const errId = useId();
  const [error, setError] = useState<string | null>(null);
  const pickerKind = kind === "date" || kind === "datetime";
  const initial = starter !== null && !pickerKind ? starter : draftOf(col, initialValue);
  const maxLength = kind === "text" ? col.type_options.max_length : undefined;

  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.focus();
    if (starter !== null && !pickerKind) {
      const end = el.value.length;
      try {
        el.setSelectionRange(end, end);
      } catch {
        // Some input types do not support selection ranges.
      }
    } else {
      el.select();
    }
  }, [starter, pickerKind]);

  function commit(move: Move): boolean {
    const el = inputRef.current;
    if (!el || done.current) return true;
    const result = parseDraft(col, el.value);
    if (!result.ok) {
      setError(result.message);
      return false;
    }
    done.current = true;
    onCommit(result.value, undefined, move);
    return true;
  }

  function cancel() {
    done.current = true;
    onCancel();
  }

  useImperativeHandle(ref, () => ({ commit }));

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    const action = editorKeyAction(e);
    if (action === null) return;
    e.preventDefault();
    if (action === "cancel") cancel();
    else commit(action);
  }

  return (
    <>
      <input
        ref={inputRef}
        type={inputType(kind)}
        inputMode={inputMode(kind)}
        step={pickerKind ? "any" : undefined}
        maxLength={maxLength && maxLength > 0 ? maxLength : undefined}
        defaultValue={initial}
        aria-label={col.display_name}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onKeyDown={onKeyDown}
        onChange={() => setError(null)}
        onBlur={() => commit("stay")}
        className="h-full w-full min-w-0 bg-surface px-2 text-fg outline-2 -outline-offset-2 outline-focus"
      />
      {error && <ErrorNote id={errId} message={error} />}
    </>
  );
}

function LongTextEditor({ col, initialValue, starter, onCommit, onCancel, ref }: EditorProps) {
  const areaRef = useRef<HTMLTextAreaElement>(null);
  const done = useRef(false);
  const hintId = useId();
  const initial = starter ?? draftOf(col, initialValue);

  function commit(move: Move): boolean {
    const el = areaRef.current;
    if (!el || done.current) return true;
    const result = parseDraft(col, el.value);
    if (!result.ok) return false;
    done.current = true;
    onCommit(result.value, undefined, move);
    return true;
  }

  function cancel() {
    done.current = true;
    onCancel();
  }

  useImperativeHandle(ref, () => ({ commit }));

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && e.altKey) {
      e.preventDefault();
      const el = e.currentTarget;
      el.setRangeText("\n", el.selectionStart, el.selectionEnd, "end");
      return;
    }
    const action = editorKeyAction(e);
    if (action === null) return;
    e.preventDefault();
    if (action === "cancel") cancel();
    else commit(action);
  }

  return (
    <Popover.Root open>
      <Popover.Anchor asChild>
        <div className="h-full w-full truncate px-2 leading-7 outline-2 -outline-offset-2 outline-focus">{initial}</div>
      </Popover.Anchor>
      <Popover.Portal>
        <Popover.Content
          aria-label={`Edit ${col.display_name}`}
          align="start"
          sideOffset={2}
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            const el = areaRef.current;
            if (!el) return;
            el.focus();
            const end = el.value.length;
            if (starter !== null) el.setSelectionRange(end, end);
            else el.select();
          }}
          onEscapeKeyDown={(e) => e.preventDefault()}
          onInteractOutside={() => commit("stay")}
          className="z-50 w-80 rounded-md border border-border bg-surface p-2 text-fg shadow-lg"
        >
          <textarea
            ref={areaRef}
            defaultValue={initial}
            aria-label={col.display_name}
            aria-describedby={hintId}
            rows={6}
            onKeyDown={onKeyDown}
            className="w-full resize-y rounded border border-border bg-surface p-1 text-sm text-fg"
          />
          <p id={hintId} className="mt-1 text-xs text-fg-muted">
            Enter saves the cell. Alt+Enter adds a new line. Esc cancels.
          </p>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

function LookupEditor({ col, initialValue, initialDisplay, starter, lookup, onCommit, onCancel, ref }: EditorProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const done = useRef(false);
  const listId = useId();
  const [query, setQuery] = useState(starter ?? "");
  const [highlight, setHighlight] = useState(starter ? 0 : -1);

  const results = useQuery({
    queryKey: ["lookup", lookup?.ws, lookup?.parentTableId, lookup?.displayColumnId, query],
    enabled: lookup !== undefined,
    staleTime: 5_000,
    queryFn: ({ signal }) =>
      lookup
        ? searchParent(lookup.ws, lookup.parentTableId, lookup.displayColumnId, query, signal)
        : Promise.resolve([]),
  });
  const options = (results.data ?? []).map((r) => ({
    id: r.id,
    text: String(r.fields[lookup?.displayColumnId ?? ""] ?? r.id),
  }));

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  function choose(index: number, move: Move): boolean {
    if (done.current) return true;
    done.current = true;
    if (index === -2) {
      onCommit(null, undefined, move);
      return true;
    }
    const opt = options[index];
    if (opt) onCommit(opt.id, opt.text, move);
    else onCommit(initialValue, initialDisplay, move);
    return true;
  }

  useImperativeHandle(ref, () => ({ commit: (move) => choose(highlight, move) }));

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.nativeEvent.isComposing) return;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setHighlight((h) => Math.min(h + 1, options.length - 1));
        return;
      case "ArrowUp":
        e.preventDefault();
        setHighlight((h) => Math.max(h - 1, -1));
        return;
      case "Escape":
        e.preventDefault();
        done.current = true;
        onCancel();
        return;
      case "Delete":
        if (e.ctrlKey || e.metaKey) {
          e.preventDefault();
          choose(-2, "stay");
        }
        return;
    }
    const action = editorKeyAction(e);
    if (action === null || action === "cancel") return;
    e.preventDefault();
    choose(highlight, action);
  }

  const activeId = highlight >= 0 && options[highlight] ? `${listId}-o${highlight}` : undefined;
  const placeholder = isEmpty(initialValue) ? "Search" : (initialDisplay ?? "Search");

  return (
    <Popover.Root open>
      <Popover.Anchor asChild>
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-label={col.display_name}
          aria-expanded="true"
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={activeId}
          value={query}
          placeholder={placeholder}
          onChange={(e) => {
            setQuery(e.target.value);
            setHighlight(e.target.value === "" ? -1 : 0);
          }}
          onKeyDown={onKeyDown}
          className="h-full w-full min-w-0 bg-surface px-2 text-fg placeholder:text-fg-muted outline-2 -outline-offset-2 outline-focus"
        />
      </Popover.Anchor>
      <Popover.Portal>
        <Popover.Content
          role="presentation"
          align="start"
          sideOffset={2}
          onOpenAutoFocus={(e) => e.preventDefault()}
          onEscapeKeyDown={(e) => e.preventDefault()}
          onInteractOutside={() => choose(-1, "stay")}
          className="z-50 w-64 rounded-md border border-border bg-surface py-1 text-fg shadow-lg"
        >
          <ul id={listId} role="listbox" aria-label={`${col.display_name} choices`} className="max-h-60 overflow-auto">
            {options.map((o, i) => (
              <li
                key={o.id}
                id={`${listId}-o${i}`}
                role="option"
                aria-selected={i === highlight}
                onMouseDown={(e) => {
                  e.preventDefault();
                  choose(i, "stay");
                }}
                className={`cursor-pointer px-2 py-1 text-sm ${i === highlight ? "bg-accent text-accent-fg" : ""}`}
              >
                {o.text}
              </li>
            ))}
          </ul>
          {options.length === 0 && (
            <p className="px-2 py-1 text-sm text-fg-muted">{results.isPending ? "Searching…" : "No matching records"}</p>
          )}
          <p className="border-t border-border px-2 pt-1 text-xs text-fg-muted">Ctrl+Delete clears the value.</p>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

/** Attachments open the attachment manager in a dialog; the cell keeps showing its thumbnails behind it. */
function AttachmentsEditor({ col, initialValue, readOnly, onCommit, onCancel, ref }: EditorProps) {
  const ws = useFilesWorkspace();
  const ids = parseIds(initialValue);
  // The dialog is modal, so no other cell can be clicked while it is open; Done / Cancel end the edit.
  useImperativeHandle(ref, () => ({
    commit: () => {
      onCancel();
      return true;
    },
  }));
  return (
    <>
      <AttachmentThumbs ws={ws} ids={ids} />
      <AttachmentsDialog
        ws={ws}
        label={col.display_name}
        value={ids}
        maxFiles={maxFilesOf(col.type_options)}
        readOnly={readOnly === true}
        onDone={(v) => onCommit(v, undefined, "stay")}
        onCancel={onCancel}
      />
    </>
  );
}

/** Point and area cells edit in a map dialog. */
function GeoEditor({ col, kind, initialValue, accuracyCol, onCommit, onCancel, ref }: EditorProps) {
  useImperativeHandle(ref, () => ({ commit: () => true }));
  return (
    <>
      <div className="h-full w-full truncate px-2 leading-7 outline-2 -outline-offset-2 outline-focus" aria-hidden="true" />
      <GeoValueDialog
        kind={kind === "geo_area" ? "geo_area" : "geo_point"}
        fieldName={col.display_name}
        initial={initialValue}
        accuracyField={accuracyCol?.display_name}
        onCancel={onCancel}
        onSave={(value, accuracy) =>
          onCommit(value, undefined, "save", accuracyCol && accuracy !== null ? [{ col: accuracyCol, value: accuracyValue(accuracyCol, accuracy) }] : undefined)
        }
      />
    </>
  );
}

/** The inline editor for a cell; picks the control that matches the field type. */
export function CellEditor(props: EditorProps) {
  switch (props.kind) {
    case "attachments":
      return <AttachmentsEditor {...props} />;
    case "geo_point":
    case "geo_area":
      return <GeoEditor {...props} />;
    case "longtext":
      return <LongTextEditor {...props} />;
    case "lookup":
      return <LookupEditor {...props} />;
    default:
      return <SingleLineEditor {...props} />;
  }
}

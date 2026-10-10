import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Dialog } from "radix-ui";
import { History, X } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { findTable, useSchema, type Column } from "@/lib/schema";
import { alertBox, btn } from "@/components/styles";
import { absoluteTime, relativeTime } from "@/features/backups/backupLogic";
import { useRowHistory } from "./historyApi";
import {
  actorLabel,
  diffFields,
  opLabel,
  snapshotLines,
  truncate,
  type FieldChange,
  type HistoryItem,
  type ValueLine,
} from "./historyLogic";

const BUILDER_ROLES: ReadonlySet<string> = new Set(["owner", "builder"]);

interface Props {
  ws: string;
  tableId: string;
  rowId: number;
  onClose: () => void;
}

/** The history side sheet of one record: who changed what and when, newest first. Mount only while open. */
export function RowHistoryPanel({ ws, tableId, rowId, onClose }: Props) {
  // Whatever had focus when the panel opened gets it back on close (Radix has no trigger to return to).
  const opener = useRef<Element | null>(typeof document === "undefined" ? null : document.activeElement);
  const schema = useSchema(ws);
  const workspace = useQuery({
    queryKey: ["workspace", ws],
    queryFn: () => api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), { label: "Opening workspace" }),
  });
  const history = useRowHistory(ws, tableId, rowId);
  const table = findTable(schema.data, tableId);
  const columns: readonly Column[] = table?.columns ?? [];
  const isBuilder = BUILDER_ROLES.has(workspace.data?.role ?? "");
  const items = history.data?.pages.flatMap((p) => p.items) ?? [];
  const tracked = history.data?.pages[0]?.tracked ?? true;

  let body;
  if (history.isError) {
    body = (
      <div role="alert" className={alertBox}>
        <p className="m-0">{history.error instanceof ApiError ? history.error.message : "The history could not be loaded."}</p>
        <button type="button" className={`${btn} mt-2`} onClick={() => void history.refetch()}>
          Try again
        </button>
      </div>
    );
  } else if (history.isPending || !schema.data) {
    body = <p className="text-sm text-fg-muted">Loading history…</p>;
  } else if (!tracked) {
    body = (
      <p className="m-0 text-sm" data-testid="history-off">
        History is off for this table.
        {isBuilder && " A builder can turn on Track history in Design."}
      </p>
    );
  } else if (items.length === 0) {
    body = <p className="m-0 text-sm text-fg-muted">No history has been recorded for this record yet.</p>;
  } else {
    body = (
      <>
        <ol aria-label="Changes, newest first" className="m-0 flex list-none flex-col gap-3 p-0">
          {items.map((item) => (
            <Entry key={item.id} item={item} columns={columns} />
          ))}
        </ol>
        {history.hasNextPage && (
          <button type="button" className={`${btn} mt-3`} disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>
            {history.isFetchingNextPage ? "Loading…" : "Load older"}
          </button>
        )}
      </>
    );
  }

  return (
    <Dialog.Root open onOpenChange={(o) => (o ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40" />
        <Dialog.Content
          aria-describedby={undefined}
          onCloseAutoFocus={(e) => {
            if (opener.current instanceof HTMLElement && opener.current.isConnected) {
              e.preventDefault();
              opener.current.focus();
            }
          }}
          className="fixed inset-y-0 right-0 z-50 flex w-[min(32rem,100vw)] flex-col border-l border-border bg-surface text-fg shadow-xl"
        >
          <div className="flex items-center gap-2 border-b border-border px-4 py-2">
            <History size={18} aria-hidden="true" />
            <Dialog.Title className="m-0 mr-auto text-base font-semibold">
              History{table ? ` of record ${rowId} in ${table.display_name}` : ` of record ${rowId}`}
            </Dialog.Title>
            <Dialog.Close className={`${btn} !px-2`} aria-label="Close history">
              <X size={16} aria-hidden="true" />
            </Dialog.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">{body}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function Entry({ item, columns }: { item: HistoryItem; columns: readonly Column[] }) {
  return (
    <li className="rounded-md border border-border p-3" data-testid="history-entry" data-op={item.op}>
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="text-sm font-semibold">{actorLabel(item)}</span>
        <span className="text-sm">{opLabel(item.op)}</span>
      </div>
      <time dateTime={item.at} className="block text-sm text-fg-muted">
        {relativeTime(item.at)} · {absoluteTime(item.at)}
      </time>
      <div className="mt-2">
        {item.op === "update" ? (
          <Changes changes={diffFields(item.before, item.after, columns)} />
        ) : (
          <Snapshot lines={snapshotLines(item.op === "insert" ? item.after : item.before, columns)} />
        )}
      </div>
    </li>
  );
}

function Changes({ changes }: { changes: FieldChange[] }) {
  if (changes.length === 0) return <p className="m-0 text-sm text-fg-muted">No visible field changed.</p>;
  return (
    <ul className="m-0 flex list-none flex-col gap-1 p-0 text-sm">
      {changes.map((c) => (
        <li key={c.colId} data-testid="history-change">
          <span className={`font-medium ${c.removed ? "italic" : ""}`}>{c.label}</span>
          {": "}
          <Text value={c.before} muted />
          <span aria-label="changed to"> → </span>
          <Text value={c.after} />
        </li>
      ))}
    </ul>
  );
}

function Snapshot({ lines }: { lines: ValueLine[] }) {
  if (lines.length === 0) return <p className="m-0 text-sm text-fg-muted">No values were recorded.</p>;
  return (
    <ul className="m-0 flex list-none flex-col gap-1 p-0 text-sm">
      {lines.map((l) => (
        <li key={l.colId}>
          <span className={`font-medium ${l.removed ? "italic" : ""}`}>{l.label}</span>
          {": "}
          <Text value={l.value} />
        </li>
      ))}
    </ul>
  );
}

/** A value, long ones cut with a Show more toggle. */
function Text({ value, muted }: { value: string; muted?: boolean }) {
  const [open, setOpen] = useState(false);
  const t = truncate(value);
  return (
    <span className={`break-words ${muted ? "text-fg-muted" : ""}`}>
      {open || !t.truncated ? value : t.short}
      {t.truncated && (
        <>
          {" "}
          <button type="button" className="cursor-pointer text-accent underline" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? "Show less" : "Show more"}
          </button>
        </>
      )}
    </span>
  );
}

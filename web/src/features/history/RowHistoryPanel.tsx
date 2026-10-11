import { EmptyState, SkeletonLines } from "@/components/EditorParts";
import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Dialog } from "radix-ui";
import { History, X } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { findTable, useSchema, type Column } from "@/lib/schema";
import { alertBox, badgeAccent, badgeDanger, badgeSuccess, btn, iconBtn } from "@/components/styles";
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
    body = <SkeletonLines rows={6} label="Loading history…" />;
  } else if (!tracked) {
    body = (
      <div data-testid="history-off">
        <EmptyState
          icon={History}
          compact
          title="History is off for this table."
          hint={isBuilder ? "A builder can turn on Track history in Design." : "Ask a builder to turn on Track history."}
        />
      </div>
    );
  } else if (items.length === 0) {
    body = <EmptyState icon={History} compact title="No history yet" hint="No history has been recorded for this record yet." />;
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
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-[2px]" />
        <Dialog.Content
          aria-describedby={undefined}
          data-presentation="side"
          onCloseAutoFocus={(e) => {
            if (opener.current instanceof HTMLElement && opener.current.isConnected) {
              e.preventDefault();
              opener.current.focus();
            }
          }}
          className="fixed inset-y-0 right-0 z-50 flex w-[min(92vw,28rem)] flex-col border-l border-border bg-surface text-fg shadow-lg"
        >
          <div className="flex items-center gap-2.5 border-b border-border px-4 py-3">
            <span aria-hidden="true" className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent-soft text-accent">
              <History size={18} />
            </span>
            <Dialog.Title className="m-0 mr-auto min-w-0 text-base font-semibold tracking-tight">
              History{table ? ` of record ${rowId} in ${table.display_name}` : ` of record ${rowId}`}
            </Dialog.Title>
            <Dialog.Close className={iconBtn} aria-label="Close history">
              <X size={16} aria-hidden="true" />
            </Dialog.Close>
          </div>
          <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-bg px-4 py-4">{body}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function Entry({ item, columns }: { item: HistoryItem; columns: readonly Column[] }) {
  return (
    <li className="rounded-xl border border-border bg-surface p-3 shadow-xs" data-testid="history-entry" data-op={item.op}>
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="text-sm font-semibold">{actorLabel(item)}</span>
        <span className={item.op === "delete" ? badgeDanger : item.op === "insert" ? badgeSuccess : badgeAccent}>{opLabel(item.op)}</span>
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

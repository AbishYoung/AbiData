import { useState } from "react";
import { Popover } from "radix-ui";
import { cancelOperation, useActiveOperations, type OperationProgress } from "@/lib/operations";
import { ApiError } from "@/lib/api";
import { btn } from "@/components/styles";

/** Human label of an operation kind ("import" → "Import"). */
export function operationLabel(kind: string): string {
  const k = kind.replace(/[._]/g, " ").trim();
  return k === "" ? "Operation" : k.charAt(0).toUpperCase() + k.slice(1);
}

function Row({ ws, op }: { ws: string; op: OperationProgress }) {
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const label = operationLabel(op.kind);
  const percent = Math.round(op.progress * 100);

  async function cancel(): Promise<void> {
    setPending(true);
    setError(null);
    try {
      await cancelOperation(ws, op.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "The operation could not be cancelled.");
    } finally {
      setPending(false);
    }
  }

  return (
    <li className="border-b border-border py-2 last:border-b-0">
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-medium">{label}</span>
        <span className="text-fg-muted">{op.status === "queued" ? "Queued" : `${percent}%`}</span>
        <button
          type="button"
          className={`${btn} !px-2 !py-0.5`}
          disabled={pending}
          aria-label={`Cancel ${label}`}
          onClick={() => void cancel()}
        >
          Cancel
        </button>
      </div>
      <progress className="mt-1 h-2 w-full" max={100} value={percent} aria-label={`${label} progress`} />
      {op.message && <p className="m-0 mt-1 text-fg-muted">{op.message}</p>}
      {error && (
        <p role="alert" className="m-0 mt-1 text-danger">
          {error}
        </p>
      )}
    </li>
  );
}

/** Status bar entry: the number of running background operations, with a popover listing them. */
export function OperationsStatus({ ws }: { ws: string }) {
  const ops = useActiveOperations(ws);
  return (
    <Popover.Root>
      <Popover.Trigger asChild>
        <button type="button" className="cursor-pointer rounded px-1 underline-offset-2 hover:underline">
          Background operations: {ops.length}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="top"
          align="end"
          sideOffset={6}
          aria-label="Background operations"
          className="z-50 w-80 rounded-md border border-border bg-surface p-3 text-sm text-fg shadow-lg"
        >
          {ops.length === 0 ? (
            <p className="m-0 text-fg-muted">No background operations are running.</p>
          ) : (
            <ul className="m-0 list-none p-0" aria-live="polite">
              {ops.map((op) => (
                <Row key={op.id} ws={ws} op={op} />
              ))}
            </ul>
          )}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

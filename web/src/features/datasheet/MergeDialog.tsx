import { useState } from "react";
import { Dialog } from "radix-ui";
import { GitMerge } from "lucide-react";
import type { Column } from "@/lib/schema";
import { btn, btnPrimary } from "@/components/styles";
import type { Row } from "./rowsApi";
import { mergedRequest, type Choice, type MergeField } from "./merge";
import { formatValue } from "./values";

interface MergeDialogProps {
  columns: readonly Column[];
  fields: readonly MergeField[];
  current: Row;
  busy: boolean;
  /** Re-PATCH with the chosen values and If-Match = the server's current version. */
  onSave: (request: { version: number; fields: Record<string, unknown> }) => void;
  /** Drop my changes and show the server's version. */
  onDiscard: () => void;
}

export function MergeDialog({ columns, fields, current, busy, onSave, onDiscard }: MergeDialogProps) {
  const [choices, setChoices] = useState<Record<string, Choice>>({});
  const byId = new Map(columns.map((c) => [c.id, c]));

  function shown(f: MergeField, side: Choice): string {
    const col = byId.get(f.colId);
    if (!col) return "";
    const text =
      side === "mine" ? formatValue(col, f.mine, f.mineDisplay) : formatValue(col, f.theirs, f.theirsDisplay);
    return text === "" ? "(empty)" : text;
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onDiscard())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content
          onEscapeKeyDown={(e) => e.preventDefault()}
          onPointerDownOutside={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
          className="fixed left-1/2 top-1/2 max-h-[90vh] w-[min(40rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 overflow-auto rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
          <Dialog.Title className="flex items-center gap-2 text-base font-semibold">
            <GitMerge size={18} aria-hidden="true" />
            Someone else changed this record
          </Dialog.Title>
          <Dialog.Description className="mt-2 text-sm">
            Record {current.id} was saved by another user while you were editing it. For each field you changed,
            choose which value to keep.
          </Dialog.Description>
          <div className="mt-4 space-y-3">
            {fields.map((f) => {
              const col = byId.get(f.colId);
              const choice = choices[f.colId] ?? "mine";
              return (
                <fieldset key={f.colId} className="rounded-md border border-border p-3">
                  <legend className="px-1 text-sm font-medium">{col?.display_name ?? f.colId}</legend>
                  <div className="flex flex-col gap-1">
                    {(["mine", "theirs"] as const).map((side) => (
                      <label key={side} className="flex items-center gap-2 text-sm">
                        <input
                          type="radio"
                          name={`merge-${f.colId}`}
                          checked={choice === side}
                          onChange={() => setChoices((c) => ({ ...c, [f.colId]: side }))}
                        />
                        <span className="w-14 font-medium">{side === "mine" ? "Mine" : "Theirs"}</span>
                        <span className="min-w-0 break-words">{shown(f, side)}</span>
                      </label>
                    ))}
                  </div>
                </fieldset>
              );
            })}
          </div>
          <div className="mt-5 flex justify-end gap-2">
            <button type="button" className={btn} onClick={onDiscard} disabled={busy}>
              Discard mine
            </button>
            <button
              type="button"
              className={btnPrimary}
              disabled={busy}
              onClick={() => onSave(mergedRequest(fields, choices, current))}
            >
              {busy ? "Saving…" : "Save merged"}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

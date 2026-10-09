import { useId, useState, type FormEvent } from "react";
import { Dialog } from "radix-ui";
import { useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import { btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { SchemaChangeFlow } from "@/features/tabledesign/SchemaChangeFlow";
import type { SchemaApplyResult } from "@/features/tabledesign/schemaChange";
import { keys } from "@/lib/events";
import type { Schema, Table } from "@/lib/schema";

function useCommit(ws: string) {
  const qc = useQueryClient();
  return (result: SchemaApplyResult) => {
    qc.setQueryData(keys.schema(ws), result.schema);
    void qc.invalidateQueries({ queryKey: keys.schema(ws) });
  };
}

interface DialogProps {
  ws: string;
  table: Table;
  schema: Schema | undefined;
  onClose: () => void;
}

/** Asks for a new name, then previews and applies rename_table. */
export function RenameTableDialog({ ws, table, schema, onClose }: DialogProps) {
  const commit = useCommit(ws);
  const renameTab = useTabs((s) => s.rename);
  const fieldId = useId();
  const [name, setName] = useState(table.display_name);
  const [confirmed, setConfirmed] = useState<string | null>(null);
  const trimmed = name.trim();

  if (confirmed !== null) {
    return (
      <SchemaChangeFlow
        ws={ws}
        ops={[{ op: "rename_table", table_id: table.id, display_name: confirmed }]}
        baseVersion={schema?.schema_version ?? null}
        title={`Rename table “${table.display_name}”`}
        summary={`Rename “${table.display_name}” to “${confirmed}”.`}
        applyLabel="Rename"
        onCancel={onClose}
        onApplied={(result) => {
          commit(result);
          renameTab(ws, `table:${table.id}`, confirmed);
          onClose();
        }}
      />
    );
  }

  function submit(e: FormEvent): void {
    e.preventDefault();
    if (trimmed !== "" && trimmed !== table.display_name) setConfirmed(trimmed);
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 w-[min(26rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
          <Dialog.Title className="m-0 text-base font-semibold">Rename table</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            You will see what the change affects before it is applied.
          </Dialog.Description>
          <form onSubmit={submit} className="mt-3">
            <label className={labelClass} htmlFor={fieldId}>
              Table name
            </label>
            <input id={fieldId} className={input} value={name} onChange={(e) => setName(e.target.value)} />
            <div className="mt-4 flex justify-end gap-2">
              <button type="button" className={btn} onClick={onClose}>
                Cancel
              </button>
              <button type="submit" className={btnPrimary} disabled={trimmed === "" || trimmed === table.display_name}>
                Rename
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** Previews drop_table, shows the impact (including data loss), then applies it. */
export function DeleteTableDialog({ ws, table, schema, onClose }: DialogProps) {
  const commit = useCommit(ws);
  const closeTab = useTabs((s) => s.close);
  const qc = useQueryClient();
  return (
    <SchemaChangeFlow
      ws={ws}
      ops={[{ op: "drop_table", table_id: table.id }]}
      baseVersion={schema?.schema_version ?? null}
      title={`Delete table “${table.display_name}”`}
      summary={`Permanently delete “${table.display_name}” and all of its rows.`}
      applyLabel="Delete table"
      onCancel={onClose}
      onApplied={(result) => {
        commit(result);
        closeTab(ws, `table:${table.id}`);
        void qc.invalidateQueries({ queryKey: keys.rowsAll(ws) });
        onClose();
      }}
    />
  );
}

import { useId, useState, type FormEvent } from "react";
import { Dialog } from "radix-ui";
import { useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { draftKey, useQueryDrafts } from "@/features/queries/draftStore";
import {
  deleteQuery,
  fetchUsages,
  inputFromSaved,
  queryKeys,
  updateQuery,
  type Saved,
  type Usage,
} from "@/features/queries/savedQueries";
import { ApiError } from "@/lib/api";

const overlay = "fixed inset-0 bg-black/50";
const content =
  "fixed left-1/2 top-1/2 w-[min(26rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-5 text-fg shadow-xl";

function message(e: unknown): string {
  if (e instanceof ApiError) return e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return "Something went wrong while contacting the server.";
}

interface Props {
  ws: string;
  query: Saved;
  onClose: () => void;
}

/** Renames a saved query (PUT with the new name). */
export function RenameQueryDialog({ ws, query, onClose }: Props) {
  const fieldId = useId();
  const qc = useQueryClient();
  const renameTab = useTabs((s) => s.rename);
  const [name, setName] = useState(query.name);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const trimmed = name.trim();

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    if (trimmed === "" || trimmed === query.name) return;
    setBusy(true);
    try {
      const saved = await updateQuery(ws, query.id, query.version, inputFromSaved(query, { name: trimmed }));
      qc.setQueryData(queryKeys.item(ws, saved.id), saved);
      void qc.invalidateQueries({ queryKey: queryKeys.all(ws) });
      renameTab(ws, `query:${query.id}`, saved.name);
      useQueryDrafts.getState().patch(draftKey(ws, `query:${query.id}`), (d) => ({ ...d, name: saved.name, version: saved.version }));
      onClose();
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className={overlay} />
        <Dialog.Content className={content}>
          <Dialog.Title className="m-0 text-base font-semibold">Rename query</Dialog.Title>
          <Dialog.Description className="sr-only">Enter a new name for the query.</Dialog.Description>
          <form onSubmit={(e) => void submit(e)} className="mt-3">
            <label className={labelClass} htmlFor={fieldId}>
              Query name
            </label>
            <input id={fieldId} className={input} value={name} onChange={(e) => setName(e.target.value)} />
            {error && (
              <p role="alert" className={`${alertBox} mt-3`}>
                {error}
              </p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button type="button" className={btn} onClick={onClose}>
                Cancel
              </button>
              <button type="submit" className={btnPrimary} disabled={busy || trimmed === "" || trimmed === query.name}>
                Rename
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** Deletes a saved query; when other queries use it, lists them instead. */
export function DeleteQueryDialog({ ws, query, onClose }: Props) {
  const qc = useQueryClient();
  const closeTab = useTabs((s) => s.close);
  const [error, setError] = useState<string | null>(null);
  const [users, setUsers] = useState<Usage[]>([]);
  const [busy, setBusy] = useState(false);

  async function remove(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      await deleteQuery(ws, query.id, query.version);
      closeTab(ws, `query:${query.id}`);
      void qc.invalidateQueries({ queryKey: queryKeys.all(ws) });
      onClose();
    } catch (err) {
      setError(message(err));
      if (err instanceof ApiError && err.code === "LINKED_RECORDS") {
        try {
          setUsers(await fetchUsages(ws, query.id));
        } catch {
          setUsers([]);
        }
      }
      setBusy(false);
    }
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className={overlay} />
        <Dialog.Content className={content}>
          <Dialog.Title className="m-0 text-base font-semibold">Delete query “{query.name}”</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            The saved query is permanently deleted. Tables and their data are not affected.
          </Dialog.Description>
          {error && (
            <div role="alert" className={`${alertBox} mt-3`}>
              <p className="m-0">{error}</p>
              {users.length > 0 && (
                <>
                  <p className="m-0 mt-2 font-medium">Used by:</p>
                  <ul className="m-0 pl-5">
                    {users.map((u) => (
                      <li key={`${u.kind}:${u.id}`}>{u.name}</li>
                    ))}
                  </ul>
                </>
              )}
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <button type="button" className={btn} onClick={onClose}>
              Cancel
            </button>
            <button type="button" className={btnPrimary} disabled={busy} onClick={() => void remove()}>
              Delete query
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

import { useId, useState } from "react";
import { Dialog } from "radix-ui";
import { Trash2 } from "lucide-react";
import { btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { MAX_INDEX_FIELDS, indexFieldNames, isIndexable, type Draft, type DraftIndex } from "./draft";
import { ulid } from "./ulid";

export interface IndexesDialogProps {
  draft: Draft;
  onChange: (indexes: DraftIndex[]) => void;
  onClose: () => void;
}

/** The Indexes window: lists the table's indexes and adds or deletes them in the draft (applied via Preview changes). */
export function IndexesDialog({ draft, onChange, onClose }: IndexesDialogProps) {
  const uid = useId();
  const [picked, setPicked] = useState<string[]>([""]);
  const [unique, setUnique] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const candidates = draft.fields.filter((f) => isIndexable(f) && f.displayName.trim() !== "");

  function setPick(i: number, id: string): void {
    setPicked((p) => p.map((v, j) => (j === i ? id : v)));
  }

  function add(): void {
    const ids = picked.filter((id) => id !== "");
    if (ids.length === 0) {
      setError("Choose at least one field for the index.");
      return;
    }
    setError(null);
    onChange([...draft.indexes, { id: ulid(), isNew: true, physicalName: "", columnIds: ids, unique }]);
    setPicked([""]);
    setUnique(false);
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 flex max-h-[calc(100vh-2rem)] w-[min(40rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
          <Dialog.Title className="m-0 text-base font-semibold">Indexes</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            Changes here join your other design changes; nothing is applied until you use Preview changes.
          </Dialog.Description>

          <div className="mt-3 min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
            {draft.indexes.length === 0 ? (
              <p className="m-0 text-sm text-fg-muted">This table has no indexes.</p>
            ) : (
              <table className="w-full border-collapse text-sm">
                <caption className="sr-only">Indexes of this table</caption>
                <thead>
                  <tr className="bg-surface-2 text-left">
                    <th scope="col" className="border-b border-border px-2 py-1.5 font-semibold">Fields</th>
                    <th scope="col" className="w-28 border-b border-border px-2 py-1.5 font-semibold">Duplicates</th>
                    <th scope="col" className="w-24 border-b border-border px-2 py-1.5 font-semibold">Status</th>
                    <th scope="col" className="w-12 border-b border-border px-1 py-1.5">
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {draft.indexes.map((x) => {
                    const names = indexFieldNames(draft, x);
                    return (
                      <tr key={x.id} data-testid="index-row">
                        <td className="border-b border-border px-2 py-1.5">{names}</td>
                        <td className="border-b border-border px-2 py-1.5">{x.unique ? "Not allowed" : "OK"}</td>
                        <td className="border-b border-border px-2 py-1.5 text-fg-muted">{x.isNew ? "New" : "Saved"}</td>
                        <td className="border-b border-border px-1 py-1">
                          <button
                            type="button"
                            className={`${btn} h-8 w-8 px-0`}
                            aria-label={`Delete index on ${names}`}
                            onClick={() => onChange(draft.indexes.filter((i) => i.id !== x.id))}
                          >
                            <Trash2 size={16} aria-hidden="true" />
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}

            <fieldset className="m-0 space-y-3 rounded-md border border-border p-3">
              <legend className="px-1 text-sm font-semibold">Add an index</legend>
              {picked.map((value, i) => {
                const taken = new Set(picked.filter((_, j) => j !== i));
                return (
                  <div key={i} className="flex items-end gap-2">
                    <div className="min-w-0 flex-1">
                      <label className={labelClass} htmlFor={`${uid}-f${i}`}>
                        Index field {i + 1}
                      </label>
                      <select id={`${uid}-f${i}`} className={input} value={value} onChange={(e) => setPick(i, e.target.value)}>
                        <option value="">Choose a field…</option>
                        {candidates
                          .filter((f) => !taken.has(f.id))
                          .map((f) => (
                            <option key={f.id} value={f.id}>
                              {f.displayName.trim()}
                            </option>
                          ))}
                      </select>
                    </div>
                    {picked.length > 1 && (
                      <button
                        type="button"
                        className={btn}
                        aria-label={`Remove index field ${i + 1}`}
                        onClick={() => setPicked((p) => p.filter((_, j) => j !== i))}
                      >
                        Remove
                      </button>
                    )}
                  </div>
                );
              })}
              <div className="flex flex-wrap items-center gap-3">
                <button
                  type="button"
                  className={btn}
                  disabled={picked.length >= MAX_INDEX_FIELDS}
                  onClick={() => setPicked((p) => [...p, ""])}
                >
                  Add another field
                </button>
                <span className="flex items-center gap-2">
                  <input id={`${uid}-unique`} type="checkbox" checked={unique} onChange={(e) => setUnique(e.target.checked)} />
                  <label htmlFor={`${uid}-unique`} className="text-sm">
                    Unique (no duplicate combinations)
                  </label>
                </span>
              </div>
              {error && (
                <p role="alert" className="m-0 text-sm text-danger">
                  {error}
                </p>
              )}
              <button type="button" className={btnPrimary} onClick={add}>
                Add index
              </button>
            </fieldset>
          </div>

          <div className="mt-4 flex justify-end">
            <Dialog.Close asChild>
              <button type="button" className={btn}>
                Close
              </button>
            </Dialog.Close>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

import { Dialog } from "radix-ui";
import { Trash2, TriangleAlert } from "lucide-react";
import { btn, btnPrimary } from "@/components/styles";

interface DeleteDialogProps {
  /** Number of records about to be deleted. */
  count: number;
  busy: boolean;
  /** Inline error, e.g. LINKED_RECORDS, shown instead of the global dialog. */
  error: { title: string; message: string; recovery: string } | null;
  onConfirm: () => void;
  onCancel: () => void;
}

export function DeleteDialog({ count, busy, error, onConfirm, onCancel }: DeleteDialogProps) {
  const noun = count === 1 ? "record" : "records";
  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onCancel())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
          <Dialog.Title className="flex items-center gap-2 text-base font-semibold">
            <Trash2 size={18} aria-hidden="true" />
            Delete {count} {noun}?
          </Dialog.Title>
          <Dialog.Description className="mt-2 text-sm">
            The {noun} will be permanently deleted. This cannot be undone.
          </Dialog.Description>
          {error && (
            <div role="alert" className="mt-3 flex items-start gap-2 rounded-md border border-danger bg-danger-soft px-3 py-2 text-sm">
              <TriangleAlert size={16} aria-hidden="true" className="mt-0.5 shrink-0 text-danger" />
              <div>
                <p className="font-medium">{error.title}</p>
                <p>{error.message}</p>
                {error.recovery && <p className="text-fg-muted">{error.recovery}</p>}
              </div>
            </div>
          )}
          <div className="mt-5 flex justify-end gap-2">
            <button type="button" className={btn} onClick={onCancel} disabled={busy}>
              Cancel
            </button>
            <button type="button" className={btnPrimary} onClick={onConfirm} disabled={busy}>
              {busy ? "Deleting…" : `Delete ${noun}`}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

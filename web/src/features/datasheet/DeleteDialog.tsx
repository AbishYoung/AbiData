import { ConfirmDialog } from "@/components/ui";
import { alertBox } from "@/components/styles";

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
    <ConfirmDialog
      title={`Delete ${count} ${noun}?`}
      message={`The ${noun} will be permanently deleted. This cannot be undone.`}
      confirmLabel={busy ? "Deleting…" : `Delete ${noun}`}
      danger
      busy={busy}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {error && (
        <div role="alert" className={alertBox}>
          <p className="m-0 font-medium">{error.title}</p>
          <p className="m-0">{error.message}</p>
          {error.recovery && <p className="m-0 text-fg-muted">{error.recovery}</p>}
        </div>
      )}
    </ConfirmDialog>
  );
}

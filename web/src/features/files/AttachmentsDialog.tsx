import { useEffect, useRef, useState } from "react";
import { Dialog } from "radix-ui";
import { Paperclip, X } from "lucide-react";
import { btn, btnPrimary, dialogDescription, dialogFooter, dialogOverlay, dialogPanel, dialogTitle, iconBtn, iconChip } from "@/components/styles";
import { AttachmentManager } from "./AttachmentManager";
import { sameIds, toValue } from "./filesLogic";

/**
 * The attachment manager in a dialog (datasheet cells). Done keeps the changes (Esc and the close
 * button do the same, so photos that were just uploaded are never lost by accident); Cancel
 * discards them. `onDone` gets the new value: a list of ids, or null for none.
 */
export function AttachmentsDialog({
  ws,
  label,
  value,
  maxFiles,
  readOnly,
  onDone,
  onCancel,
}: {
  ws: string;
  label: string;
  value: readonly string[];
  maxFiles: number;
  readOnly: boolean;
  onDone: (value: string[] | null) => void;
  onCancel: () => void;
}) {
  // Whatever had focus when the dialog opened (the datasheet grid) gets it back on close.
  const opener = useRef<Element | null>(typeof document === "undefined" ? null : document.activeElement);
  // Give focus back as soon as the dialog is gone (Radix does it a tick later), so the next key press lands on the grid.
  useEffect(
    () => () => {
      if (opener.current instanceof HTMLElement && opener.current.isConnected) opener.current.focus({ preventScroll: true });
    },
    [],
  );
  const [ids, setIds] = useState<string[]>([...value]);
  const [busy, setBusy] = useState(false);
  const finish = (): void => {
    if (busy) return; // an upload is still running: wait for it, or Cancel
    if (readOnly || sameIds(ids, value)) onCancel();
    else onDone(toValue(ids));
  };
  return (
    <Dialog.Root open onOpenChange={(o) => (o ? undefined : finish())}>
      <Dialog.Portal>
        <Dialog.Overlay className={dialogOverlay} />
        <Dialog.Content
          className={`${dialogPanel} w-[min(40rem,calc(100vw-2rem))] p-5`}
          aria-describedby="attachments-dialog-desc"
          data-testid="attachments-dialog"
          onCloseAutoFocus={(e) => {
            if (opener.current instanceof HTMLElement && opener.current.isConnected) {
              e.preventDefault();
              opener.current.focus({ preventScroll: true });
            }
          }}
        >
          <div className="flex items-start gap-2.5">
            <span aria-hidden="true" className={`${iconChip} bg-accent-soft text-accent`}>
              <Paperclip size={16} />
            </span>
            <div className="min-w-0 flex-1">
              <Dialog.Title className={dialogTitle}>{label}</Dialog.Title>
              <Dialog.Description id="attachments-dialog-desc" className={dialogDescription}>
                {readOnly ? "The files attached to this record." : "Add, remove and reorder the files of this record. Done keeps your changes."}
              </Dialog.Description>
            </div>
            <Dialog.Close className={iconBtn} aria-label="Close">
              <X size={16} aria-hidden="true" />
            </Dialog.Close>
          </div>
          <div className="mt-4">
            <AttachmentManager ws={ws} value={ids} onChange={setIds} maxFiles={maxFiles} readOnly={readOnly} label={label} onBusyChange={setBusy} />
          </div>
          <div className={dialogFooter}>
            {!readOnly && (
              <button type="button" className={btn} onClick={onCancel}>
                Cancel
              </button>
            )}
            <button type="button" className={btnPrimary} onClick={finish} disabled={busy}>
              {busy ? "Uploading…" : "Done"}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

import { useId, useState, type FormEvent } from "react";
import { Dialog } from "radix-ui";
import { useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import {
  alertBox,
  btn,
  btnPrimary,
  input,
  label as labelClass,
  dialogDescription,
  dialogFooter,
  dialogOverlay,
  dialogPanel,
  dialogTitle,
} from "@/components/styles";
import {
  deleteReport,
  inputFromReport,
  reportKeys,
  updateReport,
  type Report,
} from "@/features/reports/reportsApi";
import { ApiError } from "@/lib/api";
import { applyExternalSave } from "@/features/reports/designer/reportDraftStore";

const overlay = dialogOverlay;
const content = `${dialogPanel} w-[min(26rem,calc(100vw-2rem))] p-5`;

function message(e: unknown): string {
  if (e instanceof ApiError)
    return e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return "Something went wrong while contacting the server.";
}

interface Props {
  ws: string;
  report: Report;
  onClose: () => void;
}

/** Renames a report (PUT with the new name). */
export function RenameReportDialog({ ws, report, onClose }: Props) {
  const fieldId = useId();
  const qc = useQueryClient();
  const renameTab = useTabs((s) => s.rename);
  const [name, setName] = useState(report.name);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const trimmed = name.trim();

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    if (trimmed === "" || trimmed === report.name) return;
    setBusy(true);
    try {
      const saved = await updateReport(
        ws,
        report.id,
        report.version,
        inputFromReport(report, { name: trimmed }),
      );
      qc.setQueryData(reportKeys.one(ws, saved.id), saved);
      void qc.invalidateQueries({ queryKey: reportKeys.all(ws) });
      applyExternalSave(ws, saved);
      renameTab(ws, `report:${report.id}`, saved.name);
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
          <Dialog.Title className={dialogTitle}>Rename report</Dialog.Title>
          <Dialog.Description className="sr-only">
            Enter a new name for the report.
          </Dialog.Description>
          <form onSubmit={(e) => void submit(e)} className="mt-3">
            <label className={labelClass} htmlFor={fieldId}>
              Report name
            </label>
            <input
              id={fieldId}
              className={input}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            {error && (
              <p role="alert" className={`${alertBox} mt-3`}>
                {error}
              </p>
            )}
            <div className={dialogFooter}>
              <button type="button" className={btn} onClick={onClose}>
                Cancel
              </button>
              <button
                type="submit"
                className={btnPrimary}
                disabled={busy || trimmed === "" || trimmed === report.name}
              >
                Rename
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** Deletes a report. */
export function DeleteReportDialog({ ws, report, onClose }: Props) {
  const qc = useQueryClient();
  const closeTab = useTabs((s) => s.close);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function remove(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      await deleteReport(ws, report.id, report.version);
      closeTab(ws, `report:${report.id}`);
      void qc.invalidateQueries({ queryKey: reportKeys.all(ws) });
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
          <Dialog.Title className={dialogTitle}>
            Delete report “{report.name}”
          </Dialog.Title>
          <Dialog.Description className={dialogDescription}>
            The report definition is permanently deleted. Tables and their data
            are not affected.
          </Dialog.Description>
          {error && (
            <p role="alert" className={`${alertBox} mt-3`}>
              {error}
            </p>
          )}
          <div className={dialogFooter}>
            <button type="button" className={btn} onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className={btnPrimary}
              disabled={busy}
              onClick={() => void remove()}
            >
              Delete report
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

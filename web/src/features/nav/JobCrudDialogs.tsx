import { useId, useState, type FormEvent } from "react";
import { Dialog } from "radix-ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
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
import { ApiError } from "@/lib/api";
import { jobToInput } from "@/features/jobs/jobForm";
import { jobKeys, updateJob, type Job } from "@/features/jobs/jobsApi";

/** Renames a job with a PUT that carries its current version (If-Match). */
export function RenameJobDialog({
  ws,
  job,
  onClose,
}: {
  ws: string;
  job: Job;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const renameTab = useTabs((s) => s.rename);
  const fieldId = useId();
  const [name, setName] = useState(job.name);
  const trimmed = name.trim();
  const valid = trimmed !== "" && trimmed.length <= 200;

  const rename = useMutation({
    meta: { inline: true },
    mutationFn: () =>
      updateJob(ws, job.id, job.version, jobToInput(job, { name: trimmed })),
    onSuccess: (saved) => {
      qc.setQueryData(jobKeys.one(ws, job.id), saved);
      void qc.invalidateQueries({ queryKey: jobKeys.list(ws) });
      renameTab(ws, `job:${job.id}`, saved.name);
      onClose();
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "CONCURRENT_MODIFICATION")
        void qc.invalidateQueries({ queryKey: jobKeys.all(ws) });
    },
  });
  const err = rename.error instanceof ApiError ? rename.error : null;

  function submit(e: FormEvent): void {
    e.preventDefault();
    if (valid && trimmed !== job.name && !rename.isPending) rename.mutate();
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className={dialogOverlay} />
        <Dialog.Content
          className={`${dialogPanel} w-[min(26rem,calc(100vw-2rem))] p-5`}
        >
          <Dialog.Title className={dialogTitle}>Rename job</Dialog.Title>
          <Dialog.Description className={dialogDescription}>
            Forms and scripts that start this job with jobs.enqueue use its
            name, so update them too.
          </Dialog.Description>
          <form onSubmit={submit} className="mt-3">
            <label className={labelClass} htmlFor={fieldId}>
              Job name
            </label>
            <input
              id={fieldId}
              className={input}
              value={name}
              aria-invalid={!valid}
              onChange={(e) => setName(e.target.value)}
            />
            {err && (
              <div role="alert" className={`${alertBox} mt-2`}>
                <p className="m-0 font-semibold">{err.title}</p>
                <p className="m-0">{err.message}</p>
              </div>
            )}
            <div className={dialogFooter}>
              <button type="button" className={btn} onClick={onClose}>
                Cancel
              </button>
              <button
                type="submit"
                className={btnPrimary}
                disabled={!valid || trimmed === job.name || rename.isPending}
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

import { useId, useState } from "react";
import { Dialog } from "radix-ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnDanger, btnPrimary, label as labelClass, textarea } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { parseArgs } from "./jobForm";
import { deleteJob, jobKeys, runJob, type Job, type Run } from "./jobsApi";

const contentClass =
  "fixed left-1/2 top-1/2 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 z-50 rounded-xl border border-border bg-surface p-5 text-fg shadow-lg";

function ErrorBox({ error }: { error: unknown }) {
  if (!(error instanceof ApiError)) return null;
  return (
    <div role="alert" className={`${alertBox} mt-3`}>
      <p className="m-0 font-semibold">{error.title}</p>
      <p className="m-0">{error.message}</p>
      {error.recovery && <p className="m-0 text-fg-muted">{error.recovery}</p>}
    </div>
  );
}

/** Confirms and deletes a job together with its run history. */
export function DeleteJobDialog({ ws, job, onClose }: { ws: string; job: Pick<Job, "id" | "name" | "version">; onClose: () => void }) {
  const qc = useQueryClient();
  const closeTab = useTabs((s) => s.close);
  const del = useMutation({
    meta: { inline: true },
    mutationFn: () => deleteJob(ws, job.id, job.version),
    onSuccess: () => {
      closeTab(ws, `job:${job.id}`);
      void qc.invalidateQueries({ queryKey: jobKeys.all(ws) });
      onClose();
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "CONCURRENT_MODIFICATION") void qc.invalidateQueries({ queryKey: jobKeys.all(ws) });
    },
  });
  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-[2px]" />
        <Dialog.Content className={contentClass}>
          <Dialog.Title className="m-0 text-base font-semibold">Delete job “{job.name}”</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            This permanently deletes the job, its schedule and its whole run history, including logs and saved files.
          </Dialog.Description>
          <ErrorBox error={del.error} />
          <div className="mt-4 flex justify-end gap-2">
            <button type="button" className={btn} onClick={onClose}>
              Cancel
            </button>
            <button type="button" className={btnDanger} disabled={del.isPending} onClick={() => del.mutate()}>
              Delete job
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** Starts a run with extra arguments merged over the job's own for this run. */
export function RunWithArgsDialog({
  ws,
  job,
  onClose,
  onStarted,
}: {
  ws: string;
  job: Pick<Job, "id" | "name">;
  onClose: () => void;
  onStarted: (run: Run) => void;
}) {
  const fieldId = useId();
  const hintId = useId();
  const [text, setText] = useState("{}");
  const parsed = parseArgs(text);
  const run = useMutation({
    meta: { inline: true },
    mutationFn: () => (parsed.ok ? runJob(ws, job.id, parsed.value) : Promise.reject(new Error("invalid"))),
    onSuccess: (r) => {
      onStarted(r);
      onClose();
    },
  });
  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-[2px]" />
        <Dialog.Content className={contentClass}>
          <Dialog.Title className="m-0 text-base font-semibold">Run “{job.name}” with arguments</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            These arguments are merged over the job’s saved arguments for this run only.
          </Dialog.Description>
          <label className={`${labelClass} mt-3`} htmlFor={fieldId}>
            Run arguments (JSON object)
          </label>
          <textarea
            id={fieldId}
            rows={6}
            spellCheck={false}
            value={text}
            aria-invalid={!parsed.ok}
            aria-describedby={hintId}
            onChange={(e) => setText(e.target.value)}
            className={`${textarea} font-mono`}
          />
          <p id={hintId} className={`m-0 mt-1 text-sm ${parsed.ok ? "text-fg-muted" : "text-danger"}`}>
            {parsed.ok ? "A JSON object, for example {\"limit\": 10}." : parsed.error}
          </p>
          <ErrorBox error={run.error} />
          <div className="mt-4 flex justify-end gap-2">
            <button type="button" className={btn} onClick={onClose}>
              Cancel
            </button>
            <button type="button" className={btnPrimary} disabled={!parsed.ok || run.isPending} onClick={() => run.mutate()}>
              Run
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

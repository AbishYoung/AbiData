import { Dialog } from "radix-ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import { btn, alertBox } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { deleteScript, scriptKeys, type Script } from "./scriptsApi";

/** Confirms and deletes a script. The server refuses while jobs use it (LINKED_RECORDS); that is shown here. */
export function DeleteScriptDialog({
  ws,
  script,
  onClose,
}: {
  ws: string;
  script: Pick<Script, "id" | "name" | "version">;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const closeTab = useTabs((s) => s.close);
  const del = useMutation({
    meta: { inline: true },
    mutationFn: () => deleteScript(ws, script.id, script.version),
    onSuccess: () => {
      closeTab(ws, `script:${script.id}`);
      void qc.invalidateQueries({ queryKey: scriptKeys.all(ws) });
      onClose();
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "CONCURRENT_MODIFICATION") void qc.invalidateQueries({ queryKey: scriptKeys.all(ws) });
    },
  });
  const err = del.error instanceof ApiError ? del.error : null;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 w-[min(26rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
          <Dialog.Title className="m-0 text-base font-semibold">Delete script “{script.name}”</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            This permanently deletes the script. Other scripts that load it with require will stop working.
          </Dialog.Description>
          {err && (
            <div role="alert" className={`${alertBox} mt-3`}>
              <p className="m-0 font-semibold">{err.title}</p>
              <p className="m-0">{err.message}</p>
              {err.recovery && <p className="m-0 text-fg-muted">{err.recovery}</p>}
            </div>
          )}
          <div className="mt-4 flex justify-end gap-2">
            <button type="button" className={btn} onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className={`${btn} !border-danger !bg-danger !text-accent-fg`}
              disabled={del.isPending}
              onClick={() => del.mutate()}
            >
              Delete script
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import { alertBox } from "@/components/styles";
import { ConfirmDialog } from "@/components/ui";
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
    <ConfirmDialog
      title={`Delete script “${script.name}”`}
      message="This permanently deletes the script. Other scripts that load it with require will stop working."
      confirmLabel="Delete script"
      danger
      busy={del.isPending}
      onConfirm={() => del.mutate()}
      onCancel={onClose}
    >
      {err && (
        <div role="alert" className={alertBox}>
          <p className="m-0 font-semibold">{err.title}</p>
          <p className="m-0">{err.message}</p>
          {err.recovery && <p className="m-0 text-fg-muted">{err.recovery}</p>}
        </div>
      )}
    </ConfirmDialog>
  );
}

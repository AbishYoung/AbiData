import { useId, useState, type FormEvent } from "react";
import { Dialog } from "radix-ui";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { MODULE_NAME, scriptKeys, updateScript, type Script } from "@/features/scripts/scriptsApi";

/** Renames a script with a PUT that carries its current version (If-Match). */
export function RenameScriptDialog({ ws, script, onClose }: { ws: string; script: Script; onClose: () => void }) {
  const qc = useQueryClient();
  const renameTab = useTabs((s) => s.rename);
  const fieldId = useId();
  const hintId = useId();
  const [name, setName] = useState(script.name);
  const trimmed = name.trim();
  const valid = script.kind === "module" ? MODULE_NAME.test(trimmed) : trimmed !== "" && trimmed.length <= 100;
  const changed = trimmed !== script.name;

  const rename = useMutation({
    meta: { inline: true },
    mutationFn: () =>
      updateScript(ws, script.id, script.version, {
        name: trimmed,
        kind: script.kind,
        source: script.source,
        ...(script.owner_id ? { owner_id: script.owner_id } : {}),
      }),
    onSuccess: (saved) => {
      qc.setQueryData(scriptKeys.one(ws, script.id), saved);
      void qc.invalidateQueries({ queryKey: scriptKeys.all(ws) });
      renameTab(ws, `script:${script.id}`, saved.name);
      onClose();
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "CONCURRENT_MODIFICATION") void qc.invalidateQueries({ queryKey: scriptKeys.all(ws) });
    },
  });
  const err = rename.error instanceof ApiError ? rename.error : null;

  function submit(e: FormEvent): void {
    e.preventDefault();
    if (valid && changed && !rename.isPending) rename.mutate();
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 w-[min(26rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
          <Dialog.Title className="m-0 text-base font-semibold">Rename script</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            Scripts that load this module with require must be updated to the new name.
          </Dialog.Description>
          <form onSubmit={submit} className="mt-3">
            <label className={labelClass} htmlFor={fieldId}>
              Script name
            </label>
            <input
              id={fieldId}
              className={input}
              value={name}
              aria-invalid={trimmed !== "" && !valid}
              aria-describedby={hintId}
              onChange={(e) => setName(e.target.value)}
            />
            <p id={hintId} className="mt-1 text-sm text-fg-muted">
              {script.kind === "module" ? "Start with a letter; letters, digits, underscore and dot." : "Any name up to 100 characters."}
            </p>
            {err && (
              <div role="alert" className={`${alertBox} mt-2`}>
                <p className="m-0 font-semibold">{err.title}</p>
                <p className="m-0">{err.message}</p>
              </div>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button type="button" className={btn} onClick={onClose}>
                Cancel
              </button>
              <button type="submit" className={btnPrimary} disabled={!valid || !changed || rename.isPending}>
                Rename
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

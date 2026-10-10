/** Script Studio for a shared module or job script; scriptId null for a new script (implemented by the scripts feature). */
export function ScriptTab({ ws, scriptId, tabKey }: { ws: string; scriptId: string | null; tabKey: string }) {
  return (
    <p className="p-4 text-fg-muted">
      Script {scriptId ?? "(new)"} in {ws} ({tabKey}).
    </p>
  );
}

/** Runs a saved form: record navigation, controls, D10 events, save/delete. (Phase 3 runtime packet.) */
export function FormRuntime({ ws, formId, tabKey }: { ws: string; formId: string; tabKey: string }) {
  return (
    <p className="p-4 text-fg-muted">
      Form {formId} in {ws} ({tabKey}).
    </p>
  );
}

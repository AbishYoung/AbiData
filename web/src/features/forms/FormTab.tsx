/** A form: "form" view runs it (records, events); "design" view edits layout and module (implemented by the forms feature). */
export function FormTab({ ws, formId, tabKey, view }: { ws: string; formId: string | null; tabKey: string; view: "form" | "design" }) {
  return (
    <p className="p-4 text-fg-muted">
      Form {formId ?? "(new)"} in {ws} ({tabKey}), {view} view.
    </p>
  );
}

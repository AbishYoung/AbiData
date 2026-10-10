/** Edits a form's layout, properties and module. formId null = new form. (Phase 3 designer packet.) */
export function FormDesigner({ ws, formId, tabKey }: { ws: string; formId: string | null; tabKey: string }) {
  return (
    <p className="p-4 text-fg-muted">
      Designing form {formId ?? "(new)"} in {ws} ({tabKey}).
    </p>
  );
}

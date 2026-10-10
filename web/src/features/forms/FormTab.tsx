import { lazy, Suspense } from "react";
import { FormRuntime } from "./runtime/FormRuntime";

const FormDesigner = lazy(() => import("./designer/FormDesigner").then((m) => ({ default: m.FormDesigner })));

/** A form: "form" view runs it (records, events); "design" view edits layout and module. */
export function FormTab({ ws, formId, tabKey, view }: { ws: string; formId: string | null; tabKey: string; view: "form" | "design" }) {
  if (view === "design" || formId === null) {
    return (
      <Suspense fallback={<p className="p-4 text-fg-muted">Loading designer…</p>}>
        <FormDesigner ws={ws} formId={formId} tabKey={tabKey} />
      </Suspense>
    );
  }
  return <FormRuntime ws={ws} formId={formId} tabKey={tabKey} />;
}

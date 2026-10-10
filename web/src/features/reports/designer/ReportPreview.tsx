import { Save } from "lucide-react";
import { btnPrimary } from "@/components/styles";
import type { Report } from "../reportsApi";
import { ReportRun } from "../ReportTab";

/**
 * Previews the real report through the viewer (run, measure, paginate). The viewer runs the saved
 * report on the server, so the preview needs a saved report with no pending edits: while the design
 * has unsaved changes it asks to save first instead of showing a stale version.
 */
export function ReportPreview({ ws, saved, dirty, saving, onSave }: { ws: string; saved: Report | null; dirty: boolean; saving: boolean; onSave: () => void }) {
  if (!saved || dirty) {
    return (
      <div className="p-4" data-testid="report-preview-gate">
        <p role="status" className="m-0 mb-3">
          {saved ? "Save to preview your changes." : "Save to preview the report."} The preview runs the saved report, so it always matches what the report view shows.
        </p>
        <button type="button" className={btnPrimary} disabled={saving} onClick={onSave}>
          <Save size={16} aria-hidden="true" />
          Save to preview
        </button>
      </div>
    );
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="report-preview">
      <ReportRun key={`${saved.id}-${saved.version}`} ws={ws} reportId={saved.id} />
    </div>
  );
}

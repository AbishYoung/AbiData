import { KindIcon } from "@/components/objectKinds";
import { useMemo } from "react";
import type { ReportInput } from "../reportsApi";
import { DraftRun } from "../ReportTab";

/** A short stable key for a draft, so the preview reruns exactly when the design changes. */
export function draftKey(input: ReportInput): string {
  const text = JSON.stringify(input);
  let h = 5381;
  for (let i = 0; i < text.length; i++) h = ((h << 5) + h + text.charCodeAt(i)) | 0;
  return `${text.length}-${(h >>> 0).toString(36)}`;
}

/**
 * Previews the report as currently designed, saved or not: the draft runs on the server through
 * the preview endpoint (nothing is stored) and goes through the real viewer (measure, paginate).
 * A design with problems cannot run, so those are listed instead.
 */
export function ReportPreview({ ws, input, problems }: { ws: string; input: ReportInput; problems: readonly string[] }) {
  const key = useMemo(() => draftKey(input), [input]);
  if (problems.length > 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center" data-testid="report-preview-gate">
        <KindIcon kind="report" size="lg" />
        <div role="status" className="max-w-md text-sm text-fg-muted">
          <p className="m-0">Fix these problems to preview the report:</p>
          <ul className="m-0 mt-2 list-disc pl-5 text-left">
            {problems.slice(0, 5).map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </div>
      </div>
    );
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="report-preview">
      <DraftRun key={key} ws={ws} input={input} />
    </div>
  );
}

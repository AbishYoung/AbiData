import { useId, useState } from "react";
import { Dialog } from "radix-ui";
import { Info, OctagonAlert, TriangleAlert } from "lucide-react";
import { Tip } from "@/components/Tip";
import { btn, btnPrimary } from "@/components/styles";
import type { ImpactReport } from "./draft";

type Issue = NonNullable<ImpactReport["breaking"]>[number];
type Failure = NonNullable<ImpactReport["conversion_failures"]>[number];

/** Breaking issues of these kinds mean existing rows break the new design; they can never be forced. */
const HARD_KINDS: ReadonlySet<string> = new Set(["constraint_violation", "fk_violation"]);

export function hasHardViolation(report: ImpactReport): boolean {
  return (report.breaking ?? []).some((i) => HARD_KINDS.has(i.kind));
}

function sampleValue(v: unknown): string {
  if (typeof v === "string") return `“${v}”`;
  if (v === null || v === undefined) return "empty";
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}

function IssueList({ issues }: { issues: Issue[] }) {
  return (
    <ul className="m-0 list-none space-y-1 p-0">
      {issues.map((i, n) => (
        <li key={`${i.kind}-${i.id ?? n}-${n}`} className="text-sm">
          {i.message}
        </li>
      ))}
    </ul>
  );
}

function ConversionTable({ failures }: { failures: Failure[] }) {
  return (
    <table className="w-full border-collapse text-sm">
      <caption className="sr-only">Values that cannot be converted</caption>
      <thead>
        <tr className="text-left">
          <th scope="col" className="border-b border-border py-1 pr-3 font-semibold">Field</th>
          <th scope="col" className="border-b border-border py-1 pr-3 font-semibold">Change</th>
          <th scope="col" className="border-b border-border py-1 pr-3 font-semibold">Values affected</th>
          <th scope="col" className="border-b border-border py-1 font-semibold">Sample rows</th>
        </tr>
      </thead>
      <tbody>
        {failures.map((f) => (
          <tr key={f.column_id} className="align-top">
            <th scope="row" className="border-b border-border py-1 pr-3 text-left font-medium">{f.column}</th>
            <td className="border-b border-border py-1 pr-3">
              {f.from} → {f.to}
            </td>
            <td className="border-b border-border py-1 pr-3">{f.count}</td>
            <td className="border-b border-border py-1">
              <ul className="m-0 list-none space-y-0.5 p-0">
                {(f.samples ?? []).map((s) => (
                  <li key={s.row_id}>
                    Row {s.row_id}: <span className="font-mono">{sampleValue(s.value)}</span>
                  </li>
                ))}
              </ul>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export interface ImpactDialogProps {
  title: string;
  /** One sentence describing what is being changed. */
  summary: string;
  /** Null while the preview is still running. */
  report: ImpactReport | null;
  busy: boolean;
  /** An error from the last preview/apply attempt, shown inline. */
  error?: string | null;
  /** The design was changed elsewhere; offers "Reload design". */
  concurrent?: boolean;
  onReload?: () => void;
  applyLabel?: string;
  onApply: (force: boolean) => void;
  onCancel: () => void;
}

export function ImpactDialog({
  title,
  summary,
  report,
  busy,
  error,
  concurrent,
  onReload,
  applyLabel = "Apply",
  onApply,
  onCancel,
}: ImpactDialogProps) {
  const [understood, setUnderstood] = useState(false);
  const understoodId = useId();
  const breaking = report?.breaking ?? [];
  const warnings = report?.warnings ?? [];
  const failures = report?.conversion_failures ?? [];
  const hard = report ? hasHardViolation(report) : false;
  const canForce = report !== null && breaking.length > 0 && !hard;
  const nothingToReport = report !== null && breaking.length === 0 && warnings.length === 0 && failures.length === 0;

  return (
    <Dialog.Root open onOpenChange={(open) => (open || busy ? undefined : onCancel())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content
          className="fixed left-1/2 top-1/2 flex max-h-[calc(100vh-2rem)] w-[min(44rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-surface p-5 text-fg shadow-xl"
        >
          <Dialog.Title className="m-0 text-base font-semibold">{title}</Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">{summary}</Dialog.Description>

          <div className="mt-3 min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
            {report === null && !error && (
              <p role="status" className="text-sm">
                Checking what this change would affect…
              </p>
            )}

            {concurrent && (
              <div role="alert" className="rounded-md border border-danger bg-danger-soft px-3 py-2 text-sm">
                <p className="m-0 font-semibold">The design changed elsewhere</p>
                <p className="m-0 mt-1">
                  Someone else changed this design after you started editing. Nothing was applied. Reload the design to see
                  the current version; your unapplied changes will be discarded.
                </p>
                {onReload && (
                  <button type="button" className={`${btn} mt-2`} onClick={onReload}>
                    Reload design
                  </button>
                )}
              </div>
            )}

            {error && !concurrent && (
              <p role="alert" className="m-0 rounded-md border border-danger bg-danger-soft px-3 py-2 text-sm">
                {error}
              </p>
            )}

            {report !== null && (
              <>
                {breaking.length > 0 && (
                  <section aria-labelledby={`${understoodId}-breaking`}>
                    <h3 id={`${understoodId}-breaking`} className="m-0 mb-1 flex items-center gap-2 text-sm font-semibold text-danger">
                      <OctagonAlert size={16} aria-hidden="true" />
                      Breaking ({breaking.length})
                    </h3>
                    <IssueList issues={breaking} />
                    {hard && (
                      <p className="m-0 mt-2 text-sm">
                        Some existing rows do not satisfy the new design, so this change cannot be forced. Fix those rows or
                        adjust the design first.
                      </p>
                    )}
                  </section>
                )}

                {failures.length > 0 && (
                  <section aria-labelledby={`${understoodId}-conv`}>
                    <h3 id={`${understoodId}-conv`} className="m-0 mb-1 text-sm font-semibold">
                      Values that cannot be converted
                    </h3>
                    <ConversionTable failures={failures} />
                  </section>
                )}

                {warnings.length > 0 && (
                  <section aria-labelledby={`${understoodId}-warn`}>
                    <h3 id={`${understoodId}-warn`} className="m-0 mb-1 flex items-center gap-2 text-sm font-semibold text-warning">
                      <TriangleAlert size={16} aria-hidden="true" />
                      Warnings ({warnings.length})
                    </h3>
                    <IssueList issues={warnings} />
                  </section>
                )}

                {nothingToReport && (
                  <p className="m-0 flex items-center gap-2 text-sm">
                    <Info size={16} aria-hidden="true" />
                    This change has no breaking effects.
                  </p>
                )}

                <p className="m-0 text-sm">
                  Rows affected: <strong>{report.rows_affected}</strong>
                </p>
                {report.restore_point_required && (
                  <p className="m-0 flex items-center gap-2 text-sm">
                    <Info size={16} aria-hidden="true" />A restore point will be taken first.
                  </p>
                )}

                {canForce && (
                  <label className="flex items-start gap-2 text-sm" htmlFor={understoodId}>
                    <input
                      id={understoodId}
                      type="checkbox"
                      checked={understood}
                      onChange={(e) => setUnderstood(e.target.checked)}
                      className="mt-0.5"
                    />
                    I understand this may break the listed objects
                  </label>
                )}
              </>
            )}
          </div>

          <div className="mt-4 flex justify-end gap-2">
            <button type="button" className={btn} onClick={onCancel} disabled={busy}>
              Cancel
            </button>
            {report !== null && !concurrent && (
              <>
                <Tip
                  label={
                    busy
                      ? "Wait for the current step to finish"
                      : breaking.length > 0
                        ? "Resolve the breaking changes listed above first"
                        : applyLabel
                  }
                >
                  <span className="inline-flex">
                    <button
                      type="button"
                      className={btnPrimary}
                      disabled={busy || breaking.length > 0}
                      onClick={() => onApply(false)}
                    >
                      {applyLabel}
                    </button>
                  </span>
                </Tip>
                {canForce && (
                  <Tip
                    label={
                      busy
                        ? "Wait for the current step to finish"
                        : !understood
                          ? "Tick the confirmation box above to apply anyway"
                          : `${applyLabel} anyway`
                    }
                  >
                    <span className="inline-flex">
                      <button
                        type="button"
                        className={btnPrimary}
                        disabled={busy || !understood}
                        onClick={() => onApply(true)}
                      >
                        {applyLabel} anyway
                      </button>
                    </span>
                  </Tip>
                )}
              </>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

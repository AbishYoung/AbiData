import { alertBox } from "@/components/styles";
import { ApiError } from "@/lib/api";
import {
  type UploadOptions,
} from "./importsApi";

// Small pieces of the import wizard: the error box and table, and file helpers.
// Split from ImportWizard.tsx (ADR 0015, O4).

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  if (!(error instanceof ApiError)) {
    return (
      <div role="alert" className={`${alertBox} mt-3`}>
        Something went wrong. Try again.
      </div>
    );
  }
  return (
    <div role="alert" className={`${alertBox} mt-3`}>
      <p className="m-0 font-semibold">{error.title}</p>
      <p className="m-0">{error.message}</p>
      {error.recovery && <p className="m-0 text-fg-muted">{error.recovery}</p>}
    </div>
  );
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / 1024 / 1024).toFixed(1)} MiB`;
}

export function sameOptions(a: UploadOptions, b: UploadOptions): boolean {
  return a.format === b.format && a.delimiter === b.delimiter && a.hasHeader === b.hasHeader && a.sheet === b.sheet;
}

export function extensionOf(name: string): string {
  const m = /\.([A-Za-z0-9]+)$/.exec(name);
  return m?.[1]?.toLowerCase() ?? "";
}

export function ErrorsTable({ errors, total }: { errors: readonly { row: number; column?: string | undefined; message: string }[]; total: number }) {
  return (
    <div>
      <div role="region" aria-label="Row errors" tabIndex={0} className="max-h-64 overflow-auto rounded-lg border border-border">
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">The first {errors.length} rows with errors</caption>
          <thead>
            <tr className="bg-surface-2 text-left text-xs font-semibold text-fg-muted">
              <th scope="col" className="px-3 py-1.5">Row</th>
              <th scope="col" className="px-3 py-1.5">Column</th>
              <th scope="col" className="px-3 py-1.5">Problem</th>
            </tr>
          </thead>
          <tbody>
            {errors.map((e, i) => (
              <tr key={i} className="border-t border-border">
                <td className="whitespace-nowrap px-2 py-1">{e.row.toLocaleString()}</td>
                <td className="px-2 py-1">{e.column ?? ""}</td>
                <td className="px-2 py-1">{e.message}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {total > errors.length && (
        <p className="m-0 mt-1 text-sm text-fg-muted">
          Showing the first {errors.length} of {total.toLocaleString()} rows with errors.
        </p>
      )}
    </div>
  );
}

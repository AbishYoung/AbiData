import type { ImportView } from "./importsApi";
import { typeLabel } from "./mapping";

/** The first rows of the staged file with each column's inferred type. */
export function PreviewGrid({ view }: { view: ImportView }) {
  const columns = view.columns ?? [];
  const rows = (view.preview ?? []).map((r) => r ?? []);
  return (
    <div
      role="region"
      aria-label={`Preview of the first ${rows.length} rows`}
      tabIndex={0}
      className="max-h-60 overflow-auto rounded-md border border-border"
    >
      <table className="w-full border-collapse text-sm">
        <caption className="sr-only">Preview of the first {rows.length} rows of {view.source_name}</caption>
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.index} scope="col" className="sticky top-0 whitespace-nowrap border-b border-border bg-surface-2 px-2 py-1 text-left font-semibold">
                {c.original || c.name}
                <span className="block text-xs font-normal text-fg-muted">{typeLabel(c.inferred_type)}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="odd:bg-surface even:bg-surface-2">
              {columns.map((c) => (
                <td key={c.index} className="max-w-64 truncate whitespace-nowrap border-b border-border px-2 py-1">
                  {r[c.index] ?? ""}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

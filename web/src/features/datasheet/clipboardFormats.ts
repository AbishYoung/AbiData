import type { Column } from "@/lib/schema";
import { toTsv } from "./range";
import { kindOf, type ValueKind } from "./values";

// What a copied block of cells puts on the clipboard: plain TSV, an HTML table Excel reads
// without mangling identifiers, and AbiData's own structured payload (pure, so tests pin the output).

export const CELLS_MIME = "application/x-abidata-cells";
/** The payload's version, so a later release can still read what an older one copied. */
export const CELLS_VERSION = 1;

export interface CopiedColumn {
  id: string;
  name: string;
  display_name: string;
  type: string;
}

/** AbiData's structured clipboard payload: where the cells came from and their raw values. */
export interface CellsPayload {
  version: number;
  source_table_id: string;
  source_table_name: string;
  columns: CopiedColumn[];
  rows: unknown[][];
}

export interface CopyBlock {
  tableId: string;
  tableName: string;
  columns: readonly Column[];
  /** The text of each cell, as the datasheet shows it. */
  text: readonly (readonly string[])[];
  /** The raw API value of each cell. */
  raw: readonly (readonly unknown[])[];
  /** Whether the column display names go first. */
  headers: boolean;
}

export function htmlEscape(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

const TEXT_KINDS: ReadonlySet<ValueKind> = new Set(["text", "longtext", "hyperlink"]);
const NUMERIC_KINDS: ReadonlySet<ValueKind> = new Set(["integer", "number", "currency", "autonumber"]);

/** Excel's cell style for a column: text stays text (leading zeros, "1-4"), currency keeps two decimals. */
function cellStyle(col: Column): string {
  const kind = kindOf(col);
  const parts: string[] = [];
  if (TEXT_KINDS.has(kind)) parts.push("mso-number-format:'\\@'");
  else if (kind === "currency") parts.push("mso-number-format:'\\$#,##0.00'");
  if (NUMERIC_KINDS.has(kind)) parts.push("text-align:right");
  return parts.join(";");
}

// Excel paints this fill, not our theme: a fixed light grey on a header row (ADR 0019).
// eslint-disable-next-line no-restricted-syntax
const HEADER_STYLE = "font-weight:bold;background-color:#f3f4f6";

export function toPayload(b: CopyBlock): CellsPayload {
  return {
    version: CELLS_VERSION,
    source_table_id: b.tableId,
    source_table_name: b.tableName,
    columns: b.columns.map((c) => ({ id: c.id, name: c.physical_name, display_name: c.display_name, type: c.field_type })),
    rows: b.raw.map((r) => [...r]),
  };
}

export function blockTsv(b: CopyBlock): string {
  const rows = b.headers ? [b.columns.map((c) => c.display_name), ...b.text] : b.text;
  return toTsv(rows);
}

/** A semantic table; the structured payload rides along in a data attribute (Excel ignores it). */
export function blockHtml(b: CopyBlock): string {
  const th = b.columns.map((c) => `<th style="${HEADER_STYLE}">${htmlEscape(c.display_name)}</th>`).join("");
  const head = b.headers ? `<tr>${th}</tr>` : "";
  const body = b.text
    .map((cells) => {
      const tds = cells
        .map((text, i) => {
          const col = b.columns[i];
          const style = col ? cellStyle(col) : "";
          return `<td${style ? ` style="${htmlEscape(style)}"` : ""}>${htmlEscape(text).replace(/\r?\n/g, "<br>")}</td>`;
        })
        .join("");
      return `<tr>${tds}</tr>`;
    })
    .join("");
  const meta = htmlEscape(JSON.stringify(toPayload(b)));
  return `<table data-abidata-cells="${meta}"><tbody>${head}${body}</tbody></table>`;
}

/** The clipboard flavors of a block, keyed by MIME type. */
export function blockFormats(b: CopyBlock): Record<string, string> {
  return {
    "text/plain": blockTsv(b),
    "text/html": blockHtml(b),
    [CELLS_MIME]: JSON.stringify(toPayload(b)),
  };
}

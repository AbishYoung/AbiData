import { CELLS_MIME, CELLS_VERSION, type CellsPayload } from "./clipboardFormats";
import { parseClipboard } from "./range";

// Reading what was pasted. Clipboard content comes from anywhere and is untrusted: HTML is parsed into an
// inert document (no scripts run, nothing loads) and only its text is kept, never inserted as markup,
// and the number of rows, columns and characters is capped.

export const MAX_CLIPBOARD_ROWS = 5000;
export const MAX_CLIPBOARD_COLS = 200;
export const MAX_CELL_CHARS = 32_768;
const MAX_HTML_CHARS = 8_000_000;

export interface ClipboardCells {
  matrix: string[][];
  /** Where the cells came from, when AbiData copied them. */
  payload: CellsPayload | null;
}

function textOfNode(node: Node, out: string[]): void {
  if (node.nodeType === 3) {
    out.push((node.nodeValue ?? "").replace(/\s+/g, " "));
    return;
  }
  if (node.nodeType !== 1) return;
  const el = node as Element;
  const tag = el.tagName.toLowerCase();
  if (tag === "script" || tag === "style" || tag === "template") return;
  if (tag === "br") {
    out.push("\n");
    return;
  }
  const block = tag === "p" || tag === "div" || tag === "li";
  if (block && out.length > 0 && !out[out.length - 1]?.endsWith("\n")) out.push("\n");
  for (const child of Array.from(node.childNodes)) textOfNode(child, out);
  if (block) out.push("\n");
}

/** A cell's text: whitespace collapsed like a browser shows it, <br> and block ends as line breaks. */
function cellText(cell: Element): string {
  const parts: string[] = [];
  textOfNode(cell, parts);
  return parts
    .join("")
    .replace(/\p{Zs}/gu, " ")
    .split("\n")
    .map((l) => l.trim())
    .join("\n")
    .replace(/^\n+|\n+$/g, "")
    .slice(0, MAX_CELL_CHARS);
}

/** The structured payload if `value` is one this release understands. */
export function validatePayload(value: unknown): CellsPayload | null {
  if (typeof value !== "object" || value === null) return null;
  const v = value as Record<string, unknown>;
  if (typeof v.version !== "number" || v.version > CELLS_VERSION) return null;
  if (typeof v.source_table_id !== "string" || typeof v.source_table_name !== "string") return null;
  if (!Array.isArray(v.columns) || v.columns.length > MAX_CLIPBOARD_COLS) return null;
  if (!Array.isArray(v.rows) || v.rows.length > MAX_CLIPBOARD_ROWS) return null;
  const columns = [];
  for (const c of v.columns) {
    const cc = c as Record<string, unknown> | null;
    if (!cc || typeof cc.id !== "string" || typeof cc.name !== "string" || typeof cc.display_name !== "string" || typeof cc.type !== "string") return null;
    columns.push({ id: cc.id, name: cc.name, display_name: cc.display_name, type: cc.type });
  }
  if (!v.rows.every((r) => Array.isArray(r) && r.length === columns.length)) return null;
  return { version: v.version, source_table_id: v.source_table_id, source_table_name: v.source_table_name, columns, rows: v.rows as unknown[][] };
}

function parsePayload(json: string | null | undefined): CellsPayload | null {
  if (!json || json.length > MAX_HTML_CHARS) return null;
  try {
    return validatePayload(JSON.parse(json));
  } catch {
    return null;
  }
}

/** The first table of an HTML fragment as rows of text, plus AbiData's payload if the table carries one. */
export function parseHtmlTable(html: string): ClipboardCells | null {
  if (html === "" || html.length > MAX_HTML_CHARS) return null;
  const doc = new DOMParser().parseFromString(html, "text/html");
  const table = doc.querySelector("table");
  if (!table) return null;
  const matrix: string[][] = [];
  for (const tr of Array.from(table.querySelectorAll("tr"))) {
    if (tr.closest("table") !== table) continue;
    if (matrix.length >= MAX_CLIPBOARD_ROWS) break;
    const cells: string[] = [];
    for (const cell of Array.from(tr.children)) {
      const tag = cell.tagName.toLowerCase();
      if (tag !== "td" && tag !== "th") continue;
      cells.push(cellText(cell));
      const span = Math.min(Number.parseInt(cell.getAttribute("colspan") ?? "1", 10) || 1, MAX_CLIPBOARD_COLS);
      for (let i = 1; i < span; i++) cells.push("");
      if (cells.length >= MAX_CLIPBOARD_COLS) break;
    }
    matrix.push(cells.slice(0, MAX_CLIPBOARD_COLS));
  }
  if (matrix.length === 0) return null;
  return { matrix, payload: parsePayload(table.getAttribute("data-abidata-cells")) };
}

/**
 * The cells of the clipboard by its flavors (MIME type -> text): AbiData's own payload when present,
 * else an HTML table, else the plain text (tab-separated, CSV or one column).
 */
export function readCells(flavors: Readonly<Record<string, string | undefined>>): ClipboardCells | null {
  const fromHtml = parseHtmlTable(flavors["text/html"] ?? "");
  const payload = parsePayload(flavors[CELLS_MIME]) ?? fromHtml?.payload ?? null;
  let matrix: string[][] | null = fromHtml?.matrix ?? null;
  if (!matrix) {
    const plain = flavors["text/plain"] ?? "";
    if (plain === "") return null;
    matrix = parseClipboard(plain.slice(0, MAX_HTML_CHARS));
  }
  matrix = matrix.slice(0, MAX_CLIPBOARD_ROWS).map((r) => r.slice(0, MAX_CLIPBOARD_COLS).map((c) => c.slice(0, MAX_CELL_CHARS)));
  return { matrix, payload };
}

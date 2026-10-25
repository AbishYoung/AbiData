import type { Cell } from "./nav";

// The datasheet's rectangular cell selection and its clipboard text (TSV), as pure functions.

/** A rectangle of cells: where the selection started and where it was last extended to. */
export interface CellRange {
  anchor: Cell;
  head: Cell;
}

/** The same rectangle with its edges in order. */
export interface Bounds {
  r0: number;
  r1: number;
  c0: number;
  c1: number;
}

export function boundsOf(r: CellRange): Bounds {
  return {
    r0: Math.min(r.anchor.row, r.head.row),
    r1: Math.max(r.anchor.row, r.head.row),
    c0: Math.min(r.anchor.col, r.head.col),
    c1: Math.max(r.anchor.col, r.head.col),
  };
}

export function cellCount(b: Bounds): number {
  return (b.r1 - b.r0 + 1) * (b.c1 - b.c0 + 1);
}

/** A range of one cell is no range at all. */
export function isMultiCell(r: CellRange | null): r is CellRange {
  return r !== null && cellCount(boundsOf(r)) > 1;
}

export function inBounds(b: Bounds | null, row: number, col: number): boolean {
  return b !== null && row >= b.r0 && row <= b.r1 && col >= b.c0 && col <= b.c1;
}

/** The screen-reader announcement of a selection (rows and columns count from 1). */
export function describeRange(b: Bounds): string {
  const n = cellCount(b);
  return `Selected ${n.toLocaleString("en-US")} ${n === 1 ? "cell" : "cells"} from Row ${b.r0 + 1} Column ${b.c0 + 1} to Row ${b.r1 + 1} Column ${b.c1 + 1}`;
}

// ---- TSV ----------------------------------------------------------------------

function quoteTsv(text: string): string {
  return /[\t\n\r"]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

/** Rows joined by newlines and cells by tabs; a cell holding a tab, line break or quote is quoted like Excel does. */
export function toTsv(rows: readonly (readonly string[])[]): string {
  return rows.map((r) => r.map(quoteTsv).join("\t")).join("\n");
}

/** Splits text into rows of cells on `delimiter`, honouring double-quoted cells ("" is a quote). */
function parseDelimited(text: string, delimiter: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let i = 0;
  const n = text.length;
  const endCell = () => {
    row.push(cell);
    cell = "";
  };
  while (i < n) {
    const ch = text.charAt(i);
    if (ch === '"' && cell === "") {
      // A quoted cell runs to the closing quote.
      i++;
      while (i < n) {
        const c = text.charAt(i);
        if (c === '"') {
          if (text.charAt(i + 1) === '"') {
            cell += '"';
            i += 2;
            continue;
          }
          i++;
          break;
        }
        cell += c;
        i++;
      }
      continue;
    }
    if (ch === delimiter) {
      endCell();
      i++;
    } else if (ch === "\n") {
      endCell();
      rows.push(row);
      row = [];
      i++;
    } else {
      cell += ch;
      i++;
    }
  }
  endCell();
  rows.push(row);
  return rows;
}

/**
 * The cells of clipboard text. Tab-separated text (what spreadsheets copy) is split on tabs; text
 * of several lines with the same number of commas on every line is read as CSV; anything else
 * with several lines is one column. A single value (no tab, no line break) is returned whole.
 */
export function parseClipboard(raw: string): string[][] {
  const text = raw.replace(/\r\n?/g, "\n");
  if (!text.includes("\t") && !text.includes("\n")) return [[text]];
  let delimiter = "\t";
  if (!text.includes("\t")) {
    const asCsv = trimTrailingEmptyRow(parseDelimited(text, ","));
    const width = asCsv[0]?.length ?? 0;
    if (width > 1 && asCsv.every((r) => r.length === width)) return asCsv;
    // One column: a delimiter that cannot occur in text.
    delimiter = "\u0000";
  }
  return trimTrailingEmptyRow(parseDelimited(text, delimiter));
}

/** Spreadsheets end the copied text with a line break: that is not a further row. */
function trimTrailingEmptyRow(rows: string[][]): string[][] {
  const last = rows[rows.length - 1];
  if (rows.length > 1 && last && last.length === 1 && last[0] === "") return rows.slice(0, -1);
  return rows;
}

/**
 * What a paste fills a selected rectangle with, when the clipboard holds less than the selection:
 * one cell goes into every cell, one row of cells goes into every row. Null when the clipboard is
 * not smaller than the selection in that way (then it pastes from the top left as usual).
 */
export function fillMatrix(matrix: readonly (readonly string[])[], b: Bounds): string[][] | null {
  const rows = b.r1 - b.r0 + 1;
  const cols = b.c1 - b.c0 + 1;
  if (matrix.length !== 1 || rows * cols <= 1) return null;
  const row = matrix[0] ?? [];
  if (row.length === 1) return Array.from({ length: rows }, () => Array.from({ length: cols }, () => row[0] ?? ""));
  if (rows > 1) return Array.from({ length: rows }, () => [...row]);
  return null;
}

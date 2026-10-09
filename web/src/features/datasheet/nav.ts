/** Pure keyboard navigation for the ARIA grid. Row indexes include the new-record row. */
export interface Cell {
  row: number;
  col: number;
}

export interface NavDims {
  /** Rows including the new-record row. */
  rowCount: number;
  colCount: number;
  /** Rows that fit in the viewport (PageUp / PageDown step). */
  pageSize: number;
}

export interface NavMods {
  /** Ctrl or Cmd. */
  mod: boolean;
  alt: boolean;
}

export const NAV_KEYS: ReadonlySet<string> = new Set([
  "ArrowUp",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "Home",
  "End",
  "PageUp",
  "PageDown",
]);

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(Math.max(n, lo), hi);
}

export function clampCell(cell: Cell, dims: NavDims): Cell {
  return {
    row: clamp(cell.row, 0, Math.max(dims.rowCount - 1, 0)),
    col: clamp(cell.col, 0, Math.max(dims.colCount - 1, 0)),
  };
}

/**
 * Next active cell for a navigation key. Alt+ArrowUp/ArrowDown move to the
 * previous/next record, exactly like the plain arrows.
 */
export function moveActive(cell: Cell, key: string, mods: NavMods, dims: NavDims): Cell {
  const lastRow = Math.max(dims.rowCount - 1, 0);
  const lastCol = Math.max(dims.colCount - 1, 0);
  const page = Math.max(dims.pageSize, 1);
  let { row, col } = cell;
  switch (key) {
    case "ArrowUp":
      row -= 1;
      break;
    case "ArrowDown":
      row += 1;
      break;
    case "ArrowLeft":
      col -= 1;
      break;
    case "ArrowRight":
      col += 1;
      break;
    case "Home":
      col = 0;
      if (mods.mod) row = 0;
      break;
    case "End":
      col = lastCol;
      if (mods.mod) row = lastRow;
      break;
    case "PageUp":
      row -= page;
      break;
    case "PageDown":
      row += page;
      break;
    default:
      break;
  }
  return { row: clamp(row, 0, lastRow), col: clamp(col, 0, lastCol) };
}

/**
 * Tab / Shift+Tab across cells, wrapping to the neighbouring row. Returns null
 * at the first/last cell so focus can leave the grid.
 */
export function tabTarget(cell: Cell, dims: NavDims, backwards: boolean): Cell | null {
  if (dims.rowCount === 0 || dims.colCount === 0) return null;
  if (backwards) {
    if (cell.col > 0) return { row: cell.row, col: cell.col - 1 };
    if (cell.row > 0) return { row: cell.row - 1, col: dims.colCount - 1 };
    return null;
  }
  if (cell.col < dims.colCount - 1) return { row: cell.row, col: cell.col + 1 };
  if (cell.row < dims.rowCount - 1) return { row: cell.row + 1, col: 0 };
  return null;
}

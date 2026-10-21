export interface SelectionStats {
  count: number; // Total non-empty cells in selection
  numericCount: number; // Number of numeric / currency values
  sum: number | null; // Sum of numeric values
  avg: number | null; // Arithmetic mean
  min: number | null; // Minimum value
  max: number | null; // Maximum value
  isCurrency: boolean; // True if aggregated numeric cells represent currency values
}

function roundTo(n: number, places: number): number {
  const factor = Math.pow(10, places);
  return Math.round((n + Number.EPSILON) * factor) / factor;
}

export function computeSelectionStats(
  values: unknown[],
  columns?: { isCurrency?: boolean }[],
): SelectionStats | null {
  if (!values || values.length === 0) return null;

  let count = 0;
  let numericCount = 0;
  let rawSum = 0;
  let min: number | null = null;
  let max: number | null = null;
  let allCurrency = true;

  for (let i = 0; i < values.length; i++) {
    const val = values[i];

    // Null, undefined, and empty string "" values are ignored
    if (val === null || val === undefined) continue;
    if (typeof val === "string" && val.trim() === "") continue;
    if (Array.isArray(val) && val.length === 0) continue;

    count++;

    // Non-numeric strings, dates, and booleans increment count but are omitted from numeric aggregates
    if (typeof val === "boolean" || val instanceof Date) continue;

    const isColCurrency = columns && columns.length > 0
      ? (columns.length === 1 ? !!columns[0]?.isCurrency : !!columns[i]?.isCurrency)
      : false;

    let num: number | null = null;
    let isCurrencyVal = false;

    if (typeof val === "number") {
      if (!Number.isNaN(val) && Number.isFinite(val)) {
        if (isColCurrency) {
          // Ten-thousandths integer representation (e.g. 125000 -> 12.5)
          num = Number.isInteger(val) ? val / 10000 : val;
          isCurrencyVal = true;
        } else {
          num = val;
          isCurrencyVal = false;
        }
      }
    } else if (typeof val === "string") {
      let s = val.trim();
      const isParenNeg = /^\(.*\)$/.test(s);
      if (isParenNeg) s = "-" + s.slice(1, -1);
      const hasCurrencySymbol = /[$€£¥]/.test(s);
      const cleaned = s.replace(/[$€£¥,\s]/g, "");

      if (/^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/.test(cleaned)) {
        const parsed = Number(cleaned);
        if (!Number.isNaN(parsed) && Number.isFinite(parsed)) {
          num = parsed;
          isCurrencyVal = isColCurrency || hasCurrencySymbol;
        }
      }
    }

    if (num !== null) {
      numericCount++;
      rawSum += num;
      if (min === null || num < min) min = num;
      if (max === null || num > max) max = num;
      if (!isCurrencyVal) allCurrency = false;
    }
  }

  // Return null if fewer than 2 non-empty values are selected
  if (count < 2) return null;

  const isCurrency = numericCount > 0 && allCurrency;
  const places = isCurrency ? 4 : 10;

  const sum = numericCount > 0 ? roundTo(rawSum, places) : null;
  const avg = numericCount > 0 && sum !== null ? roundTo(sum / numericCount, places) : null;

  return {
    count,
    numericCount,
    sum,
    avg,
    min: min !== null ? roundTo(min, places) : null,
    max: max !== null ? roundTo(max, places) : null,
    isCurrency,
  };
}

export function formatStatValue(val: number, isCurrency: boolean): string {
  if (isCurrency) {
    const absFormatted = Math.abs(val).toLocaleString("en-US", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 4,
    });
    return val < 0 ? `-$${absFormatted}` : `$${absFormatted}`;
  }
  return val.toLocaleString("en-US", {
    maximumFractionDigits: 4,
  });
}

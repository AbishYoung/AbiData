/**
 * Display formatting for calculated and formatted controls.
 * Formats: "currency", "integer", "percent", or a pattern such as "0.00" / "#,##0.0".
 */
function group(intPart: string): string {
  return intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

function fixed(n: number, decimals: number, grouped: boolean): string {
  const s = Math.abs(n).toFixed(decimals);
  const [i = "0", f] = s.split(".");
  const body = `${grouped ? group(i) : i}${f ? "." + f : ""}`;
  return n < 0 && Number(s) !== 0 ? `-${body}` : body;
}

const PATTERN = /^[#0,]+(\.0+)?$/;

export function formatNumeric(value: number, format: string): string {
  switch (format) {
    case "currency":
      return fixed(value, 2, true);
    case "integer":
      return fixed(Math.round(value), 0, true);
    case "percent":
      return `${fixed(value * 100, 0, false)}%`;
  }
  if (PATTERN.test(format)) {
    const decimals = (format.split(".")[1] ?? "").length;
    return fixed(value, decimals, format.includes(","));
  }
  return String(value);
}

/** Text shown for a calculated value. */
export function formatCalcValue(value: unknown, format: string): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value === "number") return format ? formatNumeric(value, format) : String(value);
  if (typeof value === "string" && format && /^-?\d+(\.\d+)?$/.test(value)) return formatNumeric(Number(value), format);
  return String(value);
}

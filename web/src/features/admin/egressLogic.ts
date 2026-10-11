import { ApiError } from "@/lib/api";

/** At most this many entries fit in the platform allowlist (the server enforces the same number). */
export const MAX_ALLOWLIST = 200;

/** The same normalisation the server applies: trim, lower-case, drop one trailing dot. */
export function normalizePattern(raw: string): string {
  return raw.trim().toLowerCase().replace(/\.$/, "");
}

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

function isIPv4(s: string): boolean {
  const m = IPV4.exec(s);
  return m !== null && m.slice(1).every((o) => Number(o) <= 255);
}

/** Null when the host pattern is acceptable. Mirrors the server's egress.ValidPattern. */
export function patternError(raw: string): string | null {
  let p = normalizePattern(raw);
  if (p === "") return "Enter a host name.";
  if (p === "*") return "A bare * is not allowed. List the exact hosts, or use *.example.com for subdomains.";
  const wild = p.startsWith("*.");
  if (wild) p = p.slice(2);
  if (p === "") return "Enter a domain after *.";
  if (p.length > 253) return "A host name can be at most 253 characters.";
  if (/\s/.test(p)) return "A host cannot contain spaces.";
  if (p.startsWith("[") && p.endsWith("]") && /^\[[0-9a-f:.]+\]$/.test(p)) {
    return wild ? "A wildcard cannot be combined with an IP address." : null;
  }
  if (/[/:@]/.test(p)) return "Enter the host only, without a scheme, port, path or credentials.";
  if (p.includes("*")) return "Use * only at the start, as *.example.com.";
  if (isIPv4(p)) return wild ? "A wildcard cannot be combined with an IP address." : null;
  const labels = p.split(".");
  if (wild && labels.length < 2) return "A wildcard must be under a domain, as *.example.com, not a whole top-level domain.";
  if (/^\d*$/.test(labels[labels.length - 1] ?? "")) return "A host name cannot end in a number. Enter a valid IP address or a host name.";
  for (const label of labels) {
    if (label === "") return "A host name cannot have empty parts.";
    if (label.length > 63) return "Each part of a host name can be at most 63 characters.";
    if (!/^[a-z0-9-]+$/.test(label)) return "Use letters, digits, hyphens and dots only. Write internationalised names in their xn-- form.";
  }
  return null;
}

/** Null when the ports list is acceptable (empty means 443). Mirrors egress.ValidPorts. */
export function portsError(raw: string): string | null {
  if (raw.trim() === "") return null;
  for (const part of raw.split(",")) {
    const t = part.trim();
    if (!/^\d+$/.test(t) || Number(t) < 1 || Number(t) > 65535) return "Ports must be numbers from 1 to 65535, separated by commas.";
  }
  return null;
}

export interface EntryValues {
  pattern: string;
  ports: string;
  note: string;
}

/** True when row i repeats the pattern of an earlier row (after normalising). */
export function isDuplicate(rows: readonly { pattern: string }[], i: number): boolean {
  const p = normalizePattern(rows[i]?.pattern ?? "");
  return p !== "" && rows.findIndex((r) => normalizePattern(r.pattern) === p) < i;
}

const ROW_FIELDS: ReadonlySet<string> = new Set(["pattern", "ports", "note"]);

/** The row and field a server validation error points at (from details.index and details.field). */
export function errorTarget(e: unknown): { index: number; field: "pattern" | "ports" | "note" } | null {
  if (!(e instanceof ApiError)) return null;
  const { index, field } = e.details ?? {};
  if (typeof index !== "number" || typeof field !== "string" || !ROW_FIELDS.has(field)) return null;
  return { index, field: field as "pattern" | "ports" | "note" };
}

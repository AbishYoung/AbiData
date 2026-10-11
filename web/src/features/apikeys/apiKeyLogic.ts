import type { ApiKey, KeyRole } from "./apiKeysApi";

/** Plain-language role descriptions shown next to the role choice. */
export const ROLE_OPTIONS: readonly { value: KeyRole; label: string; help: string }[] = [
  { value: "viewer", label: "Viewer", help: "Can read tables, run saved queries and reports. Cannot change anything." },
  { value: "editor", label: "Editor", help: "Can also add, change and delete rows. Cannot change the design of tables." },
  { value: "builder", label: "Builder", help: "Can also change tables, queries, scripts and jobs, and import data. This is the most a key can do." },
];

/** Expiry choices for new keys; `days` is null for a key that never expires. */
export const EXPIRY_OPTIONS: readonly { id: string; label: string; days: number | null }[] = [
  { id: "30", label: "30 days", days: 30 },
  { id: "90", label: "90 days", days: 90 },
  { id: "365", label: "1 year", days: 365 },
  { id: "never", label: "Never expires", days: null },
];

export type KeyStatus = "active" | "revoked" | "expired";

export function keyStatus(k: Pick<ApiKey, "revoked" | "expired">): KeyStatus {
  if (k.revoked) return "revoked";
  if (k.expired) return "expired";
  return "active";
}

export const STATUS_LABEL: Record<KeyStatus, string> = { active: "Active", revoked: "Revoked", expired: "Expired" };

/** Validates a key name the way the server does; null when acceptable. */
export function keyNameError(name: string): string | null {
  const t = name.trim();
  if (t === "") return "Enter a name so you can recognise this key later.";
  if ([...t].length > 100) return "Use at most 100 characters.";
  if (/[\p{Cc}"\\]/u.test(t)) return "Do not use quotes, backslashes or control characters.";
  return null;
}

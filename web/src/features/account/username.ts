/** The username rules the server applies (ADR 0017), except the reserved list, which only the server knows. */
export const MIN_USERNAME = 3;
export const MAX_USERNAME = 30;

export function normalizeUsername(v: string): string {
  return v.trim().toLowerCase();
}

/** A message for an invalid username, or null when the format is fine. */
export function usernameProblem(raw: string): string | null {
  const v = normalizeUsername(raw);
  if (v.length < MIN_USERNAME || v.length > MAX_USERNAME) return `Use ${MIN_USERNAME} to ${MAX_USERNAME} characters.`;
  if (!/^[a-z]/.test(v)) return "Start with a letter.";
  if (!/^[a-z0-9-]+$/.test(v)) return "Use only letters a-z, digits and hyphens.";
  if (v.endsWith("-") || v.includes("--")) return "Do not end with a hyphen or use two in a row.";
  return null;
}

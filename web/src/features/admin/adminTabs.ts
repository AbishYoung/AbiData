/** The tabs of the admin console. `/admin?tab=<id>` opens one (the overview is the default and has no param). */
export const ADMIN_TABS = [
  { id: "overview", label: "Overview" },
  { id: "workspaces", label: "Workspaces" },
  { id: "users", label: "Users" },
  { id: "api-keys", label: "API keys" },
  { id: "apps", label: "Public apps" },
  { id: "settings", label: "Settings" },
  { id: "logs", label: "Live logs" },
] as const;

export type AdminTab = (typeof ADMIN_TABS)[number]["id"];

/** Validates the `tab` search param; unknown or missing values fall back to the overview. */
export function parseAdminTab(v: unknown): AdminTab {
  return ADMIN_TABS.some((t) => t.id === v) ? (v as AdminTab) : "overview";
}

import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type AdminStats = components["schemas"]["AdminStats"];
export type AdminOverview = components["schemas"]["AdminOverview"];
export type OverviewRange = "1h" | "24h";
export type AdminWorkspace = components["schemas"]["WorkspaceDTO"];
export type AdminUser = components["schemas"]["AdminUserDTO"];
export type Quota = components["schemas"]["Quota"];
export type BackupPolicy = components["schemas"]["Policy"];
export type PlatformEgress = components["schemas"]["Global"];
export type MapsSettings = components["schemas"]["MapsSettings"];

export const adminKeys = {
  stats: ["admin", "stats"] as const,
  overview: (range: OverviewRange) => ["admin", "overview", range] as const,
  workspaces: ["admin", "workspaces"] as const,
  users: ["admin", "users"] as const,
  backupPolicy: ["admin", "settings", "backup"] as const,
  egress: ["admin", "settings", "egress"] as const,
  maps: ["admin", "settings", "maps"] as const,
  settings: ["admin", "settings", "all"] as const,
};

export async function getStats(): Promise<AdminStats> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/stats", init), { label: "Loading statistics" });
}

export async function getOverview(range: OverviewRange): Promise<AdminOverview> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/overview", { ...init, params: { query: { range } } }), {
    label: "Loading the dashboard",
  });
}

export async function listWorkspaces(): Promise<AdminWorkspace[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/admin/workspaces", init), { label: "Loading workspaces" });
  return res.items ?? [];
}

export type DemoProvisionResult = components["schemas"]["DemoProvisionDTO"];

/** Gives every active user (administrators included) their own demo workspace. */
/** The sample workspaces an administrator can hand out. */
export type DemoKind = "traders" | "invasives";

export const DEMO_SAMPLES: Record<DemoKind, { name: string; about: string }> = {
  traders: { name: "Northwind Traders", about: "customers, orders, products, queries, a form, a report and a scheduled job" },
  invasives: { name: "Northwind Invasives", about: "invasive species sightings with maps, quarantine zones and photos" },
};

export async function provisionDemos(kind: DemoKind): Promise<DemoProvisionResult> {
  return api.requestData((c, init) => c.POST("/api/v1/admin/demo-workspaces", { ...init, body: { kind } }), {
    label: `Creating ${DEMO_SAMPLES[kind].name} workspaces`,
  });
}

/** One sentence describing a provisioning run of the kind's sample, for the status line. */
export function demoSummary(r: DemoProvisionResult, kind: DemoKind): string {
  const name = DEMO_SAMPLES[kind].name;
  const people = (n: number) => (n === 1 ? "1 person" : `${n} people`);
  const created = r.created ?? [];
  const skipped = r.skipped ?? [];
  const failed = r.failed ?? [];
  const parts: string[] = [];
  if (created.length > 0) parts.push(`Created a ${name} workspace for ${people(created.length)}`);
  else parts.push(`No new ${name} workspaces were needed`);
  if (skipped.length > 0) parts.push(`${people(skipped.length)} already had one`);
  let text = parts.join("; ") + ".";
  if (failed.length > 0) text += ` Could not create one for ${failed.map((f) => f.email).join(", ")}: see the server log.`;
  return text;
}

export async function createWorkspace(body: { name: string; owner_email?: string; quota?: Quota }): Promise<AdminWorkspace> {
  return api.requestData((c, init) => c.POST("/api/v1/admin/workspaces", { ...init, body }), { label: "Creating workspace" });
}

export async function updateWorkspace(
  id: string,
  body: { name?: string; status?: "active" | "suspended"; quota?: Quota },
): Promise<AdminWorkspace> {
  return api.requestData((c, init) => c.PATCH("/api/v1/admin/workspaces/{id}", { ...init, params: { path: { id } }, body }), {
    label: "Saving workspace",
  });
}

export async function listUsers(): Promise<AdminUser[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/admin/users", init), { label: "Loading users" });
  return res.items ?? [];
}

export async function createUser(body: { email: string; display_name: string; password: string; is_platform_admin?: boolean }): Promise<AdminUser> {
  return api.requestData((c, init) => c.POST("/api/v1/admin/users", { ...init, body }), { label: "Creating user" });
}

export async function updateUser(id: string, body: { disabled?: boolean; is_platform_admin?: boolean; display_name?: string }): Promise<AdminUser> {
  return api.requestData((c, init) => c.PATCH("/api/v1/admin/users/{id}", { ...init, params: { path: { id } }, body }), {
    label: "Saving user",
  });
}

export async function getAllSettings(): Promise<Record<string, string>> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/settings", init), { label: "Loading settings" });
}

export async function getBackupPolicy(): Promise<BackupPolicy> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/settings/backup", init), { label: "Loading backup policy" });
}

export async function putBackupPolicy(body: BackupPolicy): Promise<BackupPolicy> {
  return api.requestData((c, init) => c.PUT("/api/v1/admin/settings/backup", { ...init, body }), { label: "Saving backup policy" });
}

export async function getEgressPolicy(): Promise<PlatformEgress> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/settings/egress", init), { label: "Loading egress policy" });
}

export async function putEgressPolicy(body: PlatformEgress): Promise<PlatformEgress> {
  return api.requestData((c, init) => c.PUT("/api/v1/admin/settings/egress", { ...init, body }), { label: "Saving egress policy" });
}

export async function getMaps(): Promise<MapsSettings> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/settings/maps", init), { label: "Loading map settings" });
}

export async function putMaps(body: MapsSettings): Promise<MapsSettings> {
  return api.requestData((c, init) => c.PUT("/api/v1/admin/settings/maps", { ...init, body }), { label: "Saving map settings" });
}

export async function resetUserPassword(id: string, newPassword: string): Promise<void> {
  await api.request((c, init) => c.POST("/api/v1/admin/users/{id}/password", { ...init, params: { path: { id } }, body: { new_password: newPassword } }), {
    label: "Resetting password",
  });
}

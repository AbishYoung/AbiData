import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type Backup = components["schemas"]["BackupDTO"];
export type WorkspaceSummary = components["schemas"]["WorkspaceDTO"];

export const backupKeys = {
  list: (ws: string) => ["backups", ws, "list"] as const,
  members: (ws: string) => ["backups", ws, "members"] as const,
};

export async function listBackups(ws: string): Promise<Backup[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/backups", { ...init, params: { path: { ws } } }), {
    label: "Loading backups",
  });
  return res.items ?? [];
}

/** Synchronous on the server and can take seconds for a big workspace, so it uses the background timeout. */
export async function createBackup(ws: string): Promise<Backup> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/backups", { ...init, params: { path: { ws } } }), {
    class: "background",
    timeoutMs: 300_000,
    label: "Backing up",
  });
}

export async function restoreBackup(ws: string, id: string): Promise<void> {
  await api.request((c, init) => c.POST("/api/v1/w/{ws}/backups/{id}/restore", { ...init, params: { path: { ws, id } } }), {
    class: "background",
    timeoutMs: 300_000,
    label: "Restoring",
  });
}

export async function deleteBackup(ws: string, id: string): Promise<void> {
  await api.request((c, init) => c.DELETE("/api/v1/w/{ws}/backups/{id}", { ...init, params: { path: { ws, id } } }), {
    label: "Deleting backup",
  });
}

export async function restoreBackupAsNew(ws: string, id: string, name: string): Promise<WorkspaceSummary> {
  return api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/backups/{id}/restore-as-new", { ...init, params: { path: { ws, id } }, body: { name } }),
    { class: "background", timeoutMs: 300_000, label: "Restoring as a new workspace" },
  );
}

export async function listMemberNames(ws: string): Promise<Record<string, string>> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/members", { ...init, params: { path: { ws } } }), {
    class: "background",
    label: "Loading members",
  });
  const out: Record<string, string> = {};
  for (const m of res.items ?? []) out[m.user_id] = m.display_name || m.email;
  return out;
}

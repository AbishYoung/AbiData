import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type EgressEntry = components["schemas"]["EgressEntry"];
export type WorkspaceEgress = components["schemas"]["WorkspaceEgress"];
export type PlatformEgress = components["schemas"]["Global"];

export const settingsKeys = {
  egress: (ws: string) => ["settings", ws, "egress"] as const,
};

export async function getEgress(ws: string): Promise<WorkspaceEgress> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/settings/egress", { ...init, params: { path: { ws } } }), {
    label: "Loading egress settings",
  });
}

export async function putEgress(ws: string, items: EgressEntry[]): Promise<WorkspaceEgress> {
  return api.requestData(
    (c, init) => c.PUT("/api/v1/w/{ws}/settings/egress", { ...init, params: { path: { ws } }, body: { items } }),
    { label: "Saving egress settings" },
  );
}

import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type EgressHosts = components["schemas"]["EgressHosts"];

export const settingsKeys = {
  egressHosts: (ws: string) => ["settings", ws, "egress-hosts"] as const,
};

/** The outbound hosts the platform administrator allows (read-only, builders and owners). */
export async function getEgressHosts(ws: string): Promise<EgressHosts> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/egress-hosts", { ...init, params: { path: { ws } } }), {
    label: "Loading allowed hosts",
  });
}

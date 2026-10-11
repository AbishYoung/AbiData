import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type ApiKey = components["schemas"]["APIKeyDTO"];
export type CreatedApiKey = components["schemas"]["APIKeyCreated"];
export type KeyRole = ApiKey["role"];

export const apiKeyKeys = {
  list: (ws: string) => ["api-keys", ws] as const,
  admin: ["admin", "api-keys"] as const,
};

export async function listApiKeys(ws: string): Promise<ApiKey[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/api-keys", { ...init, params: { path: { ws } } }), {
    label: "Loading API keys",
  });
  return res.items ?? [];
}

export async function createApiKey(ws: string, body: { name: string; role: KeyRole; expires_in_days?: number }): Promise<CreatedApiKey> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/api-keys", { ...init, params: { path: { ws } }, body }), {
    label: "Creating API key",
  });
}

export async function revokeApiKey(ws: string, id: string): Promise<void> {
  await api.request((c, init) => c.DELETE("/api/v1/w/{ws}/api-keys/{id}", { ...init, params: { path: { ws, id } } }), {
    label: "Revoking API key",
  });
}

export async function adminListApiKeys(): Promise<ApiKey[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/admin/api-keys", { ...init, params: { query: {} } }), {
    label: "Loading API keys",
  });
  return res.items ?? [];
}

export async function adminRevokeApiKey(id: string): Promise<void> {
  await api.request((c, init) => c.DELETE("/api/v1/admin/api-keys/{id}", { ...init, params: { path: { id } } }), {
    label: "Revoking API key",
  });
}

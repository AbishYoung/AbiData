import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type Member = components["schemas"]["MemberDTO"];
export type MemberRole = Member["role"];

export const memberKeys = {
  list: (ws: string) => ["members", ws] as const,
};

export async function listMembers(ws: string): Promise<Member[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/members", { ...init, params: { path: { ws } } }), {
    label: "Loading members",
  });
  return res.items ?? [];
}

export async function addMember(ws: string, body: { email: string; role: MemberRole }): Promise<Member> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/members", { ...init, params: { path: { ws } }, body }), {
    label: "Adding member",
  });
}

export async function updateMemberRole(ws: string, user: string, role: MemberRole): Promise<Member> {
  return api.requestData((c, init) => c.PATCH("/api/v1/w/{ws}/members/{user}", { ...init, params: { path: { ws, user } }, body: { role } }), {
    label: "Changing role",
  });
}

export async function removeMember(ws: string, user: string): Promise<void> {
  await api.request((c, init) => c.DELETE("/api/v1/w/{ws}/members/{user}", { ...init, params: { path: { ws, user } } }), {
    label: "Removing member",
  });
}

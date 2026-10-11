import { queryOptions } from "@tanstack/react-query";
import { api } from "@/lib/api";

/** The open workspace (name and the caller's role); shared by the shell and the command palette. */
export function workspaceQuery(ws: string) {
  return queryOptions({
    queryKey: ["workspace", ws],
    queryFn: () =>
      api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), {
        label: "Opening workspace",
      }),
  });
}

import { useQuery, useMutation, type QueryClient } from "@tanstack/react-query";
import type { components } from "@/generated/api";
import { api, ApiError } from "./api";
import { useSessionStore } from "./sessionStore";

export type Me = components["schemas"]["MeBody"];
export type Membership = components["schemas"]["MembershipDTO"];

export const ME_KEY = ["me"] as const;

/** Resolves to the signed-in user, or null when the server answers 401. */
export async function fetchMe(): Promise<Me | null> {
  try {
    const me = await api.requestData((c, init) => c.GET("/api/v1/auth/me", init), {
      label: "Checking session",
    });
    useSessionStore.getState().setCsrf(me.csrf_token);
    return me;
  } catch (e) {
    if (e instanceof ApiError && (e.status === 401 || e.code === "UNAUTHENTICATED")) {
      useSessionStore.getState().setCsrf(null);
      return null;
    }
    throw e;
  }
}

export async function fetchSetupPending(): Promise<boolean> {
  const status = await api.requestData((c, init) => c.GET("/api/v1/auth/setup", init), {
    label: "Checking setup",
  });
  return status.pending;
}

export function useMe() {
  return useQuery({ queryKey: ME_KEY, queryFn: fetchMe, staleTime: 60_000 });
}

/** Stores a freshly authenticated session (login or setup) in the cache. */
export function adoptSession(queryClient: QueryClient, me: Me): void {
  useSessionStore.getState().setCsrf(me.csrf_token);
  queryClient.setQueryData(ME_KEY, me);
}

export async function login(email: string, password: string): Promise<Me> {
  return api.requestData(
    (c, init) => c.POST("/api/v1/auth/login", { ...init, body: { email, password } }),
    { label: "Signing in" },
  );
}

export async function completeSetup(token: string, displayName: string, password: string): Promise<Me> {
  return api.requestData(
    (c, init) =>
      c.POST("/api/v1/auth/setup", { ...init, body: { token, display_name: displayName, password } }),
    { label: "Setting up" },
  );
}

export function useLogout(queryClient: QueryClient, onDone: () => void) {
  return useMutation({
    mutationFn: () => api.request((c, init) => c.POST("/api/v1/auth/logout", init), { label: "Signing out" }),
    onSuccess: () => {
      useSessionStore.getState().setCsrf(null);
      queryClient.clear();
      queryClient.setQueryData(ME_KEY, null);
      onDone();
    },
  });
}

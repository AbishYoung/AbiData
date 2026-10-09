import { MutationCache, QueryClient } from "@tanstack/react-query";
import { ApiError } from "@/lib/api";
import { useErrorStore } from "@/lib/errorStore";

/**
 * Mutations report failures through the global ErrorDialog unless they set
 * `meta: { inline: true }` (forms that render the error themselves).
 */
export const queryClient = new QueryClient({
  mutationCache: new MutationCache({
    onError: (error, _variables, _context, mutation) => {
      if (mutation.meta?.inline === true) return;
      if (error instanceof ApiError && !error.canceled) useErrorStore.getState().show(error);
    },
  }),
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: false },
  },
});

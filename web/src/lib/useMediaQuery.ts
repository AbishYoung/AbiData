import { useSyncExternalStore } from "react";

/** The one definition of "compact" (phones, and touch devices in landscape). Keep in sync with `@custom-variant compact` in styles/tokens.css and styles/mobile.css. */
export const COMPACT_QUERY = "(max-width: 767px), (pointer: coarse) and (max-width: 1023px)";

function subscribeTo(query: string) {
  return (notify: () => void) => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return () => undefined;
    const mql = window.matchMedia(query);
    mql.addEventListener("change", notify);
    return () => mql.removeEventListener("change", notify);
  };
}

/** Subscribes to a CSS media query. False where matchMedia is unavailable (tests, SSR). */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    subscribeTo(query),
    () => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(query).matches,
    () => false,
  );
}

/** True on phone-sized or narrow touch screens, where the shell uses a drawer, sheets and cards. */
export function useIsCompact(): boolean {
  return useMediaQuery(COMPACT_QUERY);
}

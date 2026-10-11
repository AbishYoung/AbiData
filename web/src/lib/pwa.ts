// PWA service-worker registration. Production builds in secure contexts only
// (https or localhost); never under vitest/jsdom (import.meta.env.PROD is false there).
// A new version is announced, never applied silently: the page asks the waiting
// worker to take over (SKIP_WAITING) and reloads once it controls the page.

type Listener = (apply: () => void) => void;

const listeners = new Set<Listener>();
let pending: (() => void) | null = null;
let applying = false; // only reload for a takeover the user asked for (not the first-install claim)

/** Calls `fn` (now, if an update is already waiting) when a new version is ready to apply. */
export function onUpdateReady(fn: Listener): () => void {
  listeners.add(fn);
  if (pending) fn(pending);
  return () => listeners.delete(fn);
}

function announce(worker: ServiceWorker) {
  pending = () => {
    applying = true;
    worker.postMessage({ type: "SKIP_WAITING" });
  };
  for (const fn of listeners) fn(pending);
}

export function registerServiceWorker(): void {
  if (!import.meta.env.PROD) return;
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) return;
  if (!window.isSecureContext) return;

  let reloading = false;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (!applying || reloading) return;
    reloading = true;
    window.location.reload();
  });

  window.addEventListener("load", () => {
    navigator.serviceWorker
      .register("/sw.js", { scope: "/" })
      .then((reg) => {
        // An update is only worth announcing when an older version controls the page.
        const watch = (w: ServiceWorker) => {
          if (w.state === "installed" && navigator.serviceWorker.controller) announce(w);
        };
        if (reg.waiting) watch(reg.waiting);
        reg.addEventListener("updatefound", () => {
          const w = reg.installing;
          w?.addEventListener("statechange", () => watch(w));
        });
        const check = () => void reg.update().catch(() => undefined);
        document.addEventListener("visibilitychange", () => {
          if (document.visibilityState === "visible") check();
        });
        window.setInterval(check, 60 * 60 * 1000);
      })
      .catch(() => undefined);
  });
}

import { useEffect } from "react";
import { ToastRegion, useToasts, type ToastLevel, type ToastOptions } from "@/components/Toasts";
import { onUpdateReady } from "@/lib/pwa";

type Push = (level: ToastLevel, text: string, opts?: ToastOptions) => void;
const sinks = new Set<Push>();

/** Shows an app-wide toast through the host mounted at the root (see `AppToasts`). */
export function toast(level: ToastLevel, text: string, opts?: ToastOptions): void {
  for (const push of sinks) push(level, text, opts);
}

/** The app-wide toast region; it also offers a reload when a new service-worker version is waiting. */
export function AppToasts() {
  const { toasts, push, dismiss, pause, resume } = useToasts();
  useEffect(() => {
    sinks.add(push);
    return () => {
      sinks.delete(push);
    };
  }, [push]);
  useEffect(
    () =>
      onUpdateReady((apply) =>
        push("info", "A new version is available", {
          sticky: true,
          action: { label: "Reload", onClick: apply },
        }),
      ),
    [push],
  );
  return <ToastRegion toasts={toasts} onDismiss={dismiss} onPause={pause} onResume={resume} />;
}

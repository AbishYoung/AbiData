import { useEffect } from "react";
import { ToastRegion, useToasts } from "@/components/Toasts";
import { onUpdateReady } from "@/lib/pwa";

/** Offers a reload when a new service-worker version is waiting. */
export function UpdateToast() {
  const { toasts, push, dismiss } = useToasts();
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
  return <ToastRegion toasts={toasts} onDismiss={dismiss} />;
}

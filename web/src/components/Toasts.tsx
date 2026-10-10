import { useCallback, useEffect, useRef, useState } from "react";
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from "lucide-react";

export type ToastLevel = "info" | "success" | "warning" | "error";

export interface Toast {
  id: number;
  level: ToastLevel;
  text: string;
}

const LIFETIME_MS = 6000;

/** Local toast list: `push` shows a message that dismisses itself after a few seconds. */
export function useToasts() {
  const [toasts, setToasts] = useState<readonly Toast[]>([]);
  const next = useRef(1);
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());

  const dismiss = useCallback((id: number) => {
    const t = timers.current.get(id);
    if (t) clearTimeout(t);
    timers.current.delete(id);
    setToasts((all) => all.filter((x) => x.id !== id));
  }, []);

  const push = useCallback(
    (level: ToastLevel, text: string) => {
      const id = next.current++;
      setToasts((all) => [...all.slice(-4), { id, level, text }]);
      timers.current.set(
        id,
        setTimeout(() => dismiss(id), LIFETIME_MS),
      );
    },
    [dismiss],
  );

  useEffect(() => {
    const pending = timers.current;
    return () => {
      for (const t of pending.values()) clearTimeout(t);
      pending.clear();
    };
  }, []);

  return { toasts, push, dismiss };
}

const ICONS = { info: Info, success: CircleCheck, warning: TriangleAlert, error: CircleAlert } as const;
const LABEL = { info: "Info", success: "Success", warning: "Warning", error: "Error" } as const;
const TONE = { info: "text-accent", success: "text-success", warning: "text-warning", error: "text-danger" } as const;

/**
 * Polite live region for transient messages. The region is always mounted so assistive
 * technology announces toasts as they are added. Level is conveyed by icon and text, not color.
 */
export function ToastRegion({ toasts, onDismiss }: { toasts: readonly Toast[]; onDismiss: (id: number) => void }) {
  return (
    <div
      role="status"
      aria-live="polite"
      aria-label="Notifications"
      className="pointer-events-none fixed bottom-10 right-4 z-40 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2"
    >
      {toasts.map((t) => {
        const Icon = ICONS[t.level];
        return (
          <div
            key={t.id}
            className="pointer-events-auto flex items-start gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm text-fg shadow-lg"
          >
            <Icon size={16} aria-hidden="true" className={`mt-0.5 shrink-0 ${TONE[t.level]}`} />
            <p className="min-w-0 flex-1">
              <span className="sr-only">{LABEL[t.level]}: </span>
              {t.text}
            </p>
            <button
              type="button"
              aria-label={`Dismiss notification: ${t.text}`}
              onClick={() => onDismiss(t.id)}
              className="shrink-0 rounded p-0.5 text-fg-muted hover:bg-surface-2"
            >
              <X size={14} aria-hidden="true" />
            </button>
          </div>
        );
      })}
    </div>
  );
}

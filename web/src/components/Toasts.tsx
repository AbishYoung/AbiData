import { Tip } from "@/components/Tip";
import { iconBtnSm } from "@/components/styles";
import { useCallback, useEffect, useRef, useState } from "react";
import { CircleAlert, CircleCheck, Info, TriangleAlert, X } from "lucide-react";

export type ToastLevel = "info" | "success" | "warning" | "error";

export interface Toast {
  id: number;
  level: ToastLevel;
  text: string;
  /** Optional button beside the text; the toast stays until dismissed when `sticky`. */
  action?: { label: string; onClick: () => void };
  sticky?: boolean;
}

export interface ToastOptions {
  action?: Toast["action"];
  sticky?: boolean;
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
    (level: ToastLevel, text: string, opts?: ToastOptions) => {
      const id = next.current++;
      setToasts((all) => [...all.slice(-4), { id, level, text, ...opts }]);
      if (opts?.sticky) return;
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

const ICONS = {
  info: Info,
  success: CircleCheck,
  warning: TriangleAlert,
  error: CircleAlert,
} as const;
const LABEL = {
  info: "Info",
  success: "Success",
  warning: "Warning",
  error: "Error",
} as const;
const TONE = {
  info: "text-accent",
  success: "text-success",
  warning: "text-warning",
  error: "text-danger",
} as const;
const STRIPE = {
  info: "bg-accent",
  success: "bg-success",
  warning: "bg-warning",
  error: "bg-danger",
} as const;
const CHIP = {
  info: "bg-accent-soft",
  success: "bg-success-soft",
  warning: "bg-warning-soft",
  error: "bg-danger-soft",
} as const;

/**
 * Polite live region for transient messages. The region is always mounted so assistive
 * technology announces toasts as they are added. Level is conveyed by icon and text, not color.
 */
export function ToastRegion({
  toasts,
  onDismiss,
}: {
  toasts: readonly Toast[];
  onDismiss: (id: number) => void;
}) {
  return (
    <div
      role="status"
      aria-live="polite"
      aria-label="Notifications"
      className="pointer-events-none fixed bottom-10 right-4 z-40 flex w-96 max-w-[calc(100vw-2rem)] flex-col gap-2"
    >
      {toasts.map((t) => {
        const Icon = ICONS[t.level];
        return (
          <div
            key={t.id}
            className="ui-toast pointer-events-auto relative flex items-start gap-3 overflow-hidden rounded-lg border border-border bg-surface py-3 pl-4 pr-2.5 text-sm text-fg shadow-lg"
          >
            <span
              aria-hidden="true"
              className={`absolute inset-y-0 left-0 w-1 ${STRIPE[t.level]}`}
            />
            <span
              className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md ${CHIP[t.level]} ${TONE[t.level]}`}
            >
              <Icon size={14} aria-hidden="true" />
            </span>
            <p className="m-0 min-w-0 flex-1 pt-0.5 leading-snug">
              <span className="sr-only">{LABEL[t.level]}: </span>
              {t.text}
            </p>
            {t.action && (
              <button
                type="button"
                onClick={() => {
                  t.action?.onClick();
                  onDismiss(t.id);
                }}
                className="shrink-0 self-center rounded-md px-2 py-1 text-sm font-medium text-accent hover:bg-accent-soft"
              >
                {t.action.label}
              </button>
            )}
            <Tip label="Dismiss notification">
              <button
                type="button"
                aria-label={`Dismiss notification: ${t.text}`}
                onClick={() => onDismiss(t.id)}
                className={iconBtnSm}
              >
                <X size={14} aria-hidden="true" />
              </button>
            </Tip>
          </div>
        );
      })}
    </div>
  );
}

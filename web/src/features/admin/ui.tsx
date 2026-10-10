import { useEffect, type ReactNode } from "react";
import { ApiError } from "@/lib/api";
import { alertBox, btn, label as labelClass } from "@/components/styles";

export interface FieldAria {
  id: string;
  "aria-describedby"?: string;
  "aria-invalid"?: true;
}

/** A labelled form field with optional hint and error text wired up through aria-describedby. */
export function Field({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string | undefined;
  children: (aria: FieldAria) => ReactNode;
}) {
  const ids = [hint ? `${id}-hint` : null, error ? `${id}-err` : null].filter((x): x is string => x !== null);
  const aria: FieldAria = { id };
  if (ids.length > 0) aria["aria-describedby"] = ids.join(" ");
  if (error) aria["aria-invalid"] = true;
  return (
    <div>
      <label htmlFor={id} className={labelClass}>
        {label}
      </label>
      {children(aria)}
      {hint && (
        <p id={`${id}-hint`} className="m-0 mt-1 text-xs text-fg-muted">
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-err`} role="alert" className="m-0 mt-1 text-sm text-danger">
          {error}
        </p>
      )}
    </div>
  );
}

export function ErrorBox({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  if (!error) return null;
  return (
    <div role="alert" className={alertBox}>
      {error instanceof ApiError ? error.message : "Something went wrong."}
      {onRetry && (
        <button type="button" className={`${btn} ml-2`} onClick={onRetry}>
          Try again
        </button>
      )}
    </div>
  );
}

/** Warns before the page unloads while a form has unsaved edits. */
export function useUnloadGuard(dirty: boolean): void {
  useEffect(() => {
    if (!dirty) return;
    const handler = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [dirty]);
}

export const th = "px-2 py-1 text-left font-semibold";
export const td = "px-2 py-1.5 align-top";

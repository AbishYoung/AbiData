import type { InputHTMLAttributes } from "react";

/**
 * A touch-sized toggle (50x30 px) for a yes/no value (ADR 0022). It is a real checkbox with `role="switch"`, so
 * keyboard, labels and form behaviour are native; the track is only its look. Put it inside a <label> or give it an
 * `aria-label`.
 */
export function Switch({ className = "", ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "role">) {
  return (
    <span className={`relative inline-flex h-[30px] w-[50px] shrink-0 ${className}`}>
      <input
        {...props}
        type="checkbox"
        role="switch"
        className="peer absolute inset-0 z-10 m-0 !h-full !min-h-0 !w-full cursor-pointer appearance-none rounded-full opacity-0 disabled:cursor-not-allowed"
      />
      <span
        aria-hidden="true"
        className="absolute inset-0 rounded-full bg-surface-3 ring-1 ring-inset ring-border-strong/60 transition-colors motion-reduce:transition-none peer-checked:bg-accent peer-checked:ring-0 peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-focus peer-disabled:opacity-55"
      />
      <span
        aria-hidden="true"
        className="pointer-events-none absolute left-[3px] top-[3px] h-6 w-6 rounded-full bg-surface shadow-md transition-transform duration-150 motion-reduce:transition-none peer-checked:translate-x-5"
      />
    </span>
  );
}

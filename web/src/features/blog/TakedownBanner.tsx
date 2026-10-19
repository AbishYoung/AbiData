import { useId } from "react";
import { ShieldAlert } from "lucide-react";
import type { Takedown } from "./blogApi";

/**
 * An administrator's take-down of the blog or a post (ADR 0017). A static
 * region with a heading, not a dialog; it cannot be dismissed while the
 * take-down is active.
 */
export function TakedownBanner({ takedown, scope }: { takedown: Takedown; scope: "blog" | "post" }) {
  const id = useId();
  const date = new Date(takedown.at).toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" });
  return (
    <section role="status" aria-labelledby={id} className="flex gap-3 rounded-lg border border-danger/40 bg-danger-soft px-4 py-3 text-sm text-fg">
      <ShieldAlert size={18} aria-hidden="true" className="mt-0.5 shrink-0 text-danger" />
      <div className="min-w-0 space-y-1">
        <h2 id={id} className="m-0 text-sm font-semibold">
          {scope === "blog" ? "Your blog was taken down" : "This post was taken down"} by an administrator on {date}
        </h2>
        <p className="m-0">
          <span className="font-medium">Reason:</span> {takedown.reason}
        </p>
        {takedown.note && (
          <p className="m-0">
            <span className="font-medium">Note:</span> {takedown.note}
          </p>
        )}
        <p className="m-0 text-fg-muted">You can still edit. Publishing waits until an administrator reviews it and restores it.</p>
      </div>
    </section>
  );
}

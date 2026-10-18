import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { Logo } from "@/components/brand/Logo";

/**
 * A full page for a state outside the app's screens: not found, forbidden, an unexpected error
 * (review R6). The logo at the top, then a tinted icon, the `h1`, one or two sentences and the
 * actions, centred like an empty state (DESIGN.md principle 7).
 */
export function StatusPage({
  icon: Icon,
  tone = "neutral",
  title,
  children,
  actions,
}: {
  icon: LucideIcon;
  tone?: "neutral" | "danger";
  title: string;
  children?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div className="flex min-h-full flex-col bg-bg">
      <header className="flex h-12 shrink-0 items-center px-4">
        <a href="/" aria-label="AbiData home" className="rounded-md">
          <Logo size={24} />
        </a>
      </header>
      <main className="flex flex-1 flex-col items-center justify-center gap-4 px-6 pb-24 text-center">
        <span
          aria-hidden="true"
          className={`inline-flex h-12 w-12 items-center justify-center rounded-full ${tone === "danger" ? "bg-danger-soft text-danger" : "bg-surface-3 text-fg-muted"}`}
        >
          <Icon size={22} />
        </span>
        <h1 className="m-0 text-xl font-semibold tracking-tight text-fg">{title}</h1>
        {children && <div className="max-w-md space-y-2 text-sm text-fg-muted">{children}</div>}
        {actions && <div className="mt-2 flex flex-wrap justify-center gap-2">{actions}</div>}
      </main>
    </div>
  );
}

import type { ReactNode } from "react";
import { eyebrow as eyebrowClass, pageTitle } from "@/components/styles";

/**
 * The header of a full page (DESIGN.md "Page header"): an optional eyebrow, the title as a heading
 * (`h1` unless `level` says otherwise), a one-line description and an actions slot on the right, on a
 * faint `bg-canopy` wash. The icon chip is decorative.
 */
export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  icon,
  level = 1,
  className = "",
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  icon?: ReactNode;
  level?: 1 | 2;
  className?: string;
}) {
  const Heading = level === 1 ? "h1" : "h2";
  return (
    <div className={`bg-canopy flex flex-wrap items-center justify-between gap-x-4 gap-y-3 rounded-xl border border-border bg-surface px-5 py-4 ${className}`.trim()}>
      <div className="flex min-w-0 items-center gap-3">
        {icon}
        <div className="min-w-0">
          {eyebrow && <p className={`m-0 mb-0.5 ${eyebrowClass}`}>{eyebrow}</p>}
          <Heading className={`m-0 ${pageTitle}`}>{title}</Heading>
          {description && <p className="m-0 mt-0.5 text-sm text-fg-muted">{description}</p>}
        </div>
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

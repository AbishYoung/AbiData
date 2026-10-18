import type { HTMLAttributes, ReactNode } from "react";
import { badge, badgeAccent, badgeDanger, badgeSuccess, badgeWarning, card, cardBody, cardHeader, sectionTitle } from "@/components/styles";

/**
 * A raised container for grouped content (DESIGN.md "Cards and lists"). With a `title` it gets a
 * header row (title, optional description, actions on the right) and the body is padded unless
 * `flush` is set (for divided lists and tables that run to the edges).
 */
export function Card({
  title,
  description,
  actions,
  flush = false,
  className = "",
  children,
  ...rest
}: Omit<HTMLAttributes<HTMLElement>, "title"> & {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  flush?: boolean;
  children: ReactNode;
}) {
  return (
    <section className={`${card} ${className}`.trim()} {...rest}>
      {title !== undefined && (
        <div className={cardHeader}>
          <div className="min-w-0">
            <h2 className={sectionTitle}>{title}</h2>
            {description && <p className="mt-0.5 text-xs text-fg-muted">{description}</p>}
          </div>
          {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </div>
      )}
      {flush ? children : <div className={cardBody}>{children}</div>}
    </section>
  );
}

const TONES = { neutral: badge, accent: badgeAccent, success: badgeSuccess, warning: badgeWarning, danger: badgeDanger } as const;
export type BadgeTone = keyof typeof TONES;

/** A status or role pill. Colour is never the only signal: the text says it (DESIGN.md principle 3). */
export function Badge({ tone = "neutral", className = "", children }: { tone?: BadgeTone; className?: string; children: ReactNode }) {
  return <span className={`${TONES[tone]} ${className}`.trim()}>{children}</span>;
}

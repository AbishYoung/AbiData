/* eslint-disable no-restricted-syntax -- documented exception (DESIGN.md): white marks on the solid brand gradient. */
import { useId } from "react";

/**
 * The AbiData mark: a rounded tile in the brand gradient holding three data
 * rows, the top one split like a table header. Decorative unless `title` is set.
 */
export function LogoMark({ size = 28, title }: { size?: number; title?: string }) {
  const id = useId();
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" role={title ? "img" : undefined} aria-hidden={title ? undefined : true} aria-label={title}>
      <defs>
        <linearGradient id={`${id}-g`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="var(--brand-from)" />
          <stop offset="1" stopColor="var(--brand-to)" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" fill={`url(#${id}-g)`} />
      <rect x="7" y="8" width="7.5" height="4" rx="1.5" fill="#fff" />
      <rect x="16.5" y="8" width="8.5" height="4" rx="1.5" fill="#fff" fillOpacity="0.75" />
      <rect x="7" y="14" width="18" height="4" rx="1.5" fill="#fff" fillOpacity="0.9" />
      <rect x="7" y="20" width="12" height="4" rx="1.5" fill="#fff" fillOpacity="0.6" />
    </svg>
  );
}

/** Mark plus wordmark. The text stays real text (accessible name "AbiData"). */
export function Logo({ size = 28, className = "" }: { size?: number; className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 ${className}`}>
      <LogoMark size={size} />
      <span className="text-[1.05rem] font-semibold tracking-tight text-fg">
        Abi<span className="text-accent">Data</span>
      </span>
    </span>
  );
}

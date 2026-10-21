import { ChevronDown } from "lucide-react";
import { Badge, type BadgeTone } from "@/components/ui";
import { type KeyboardEvent, type ReactNode, type Ref } from "react";

export interface FormSectionMeta {
  id: string;
  title: string;
  collapsible?: boolean;
  defaultExpanded?: boolean;
  badgeCount?: number;
  badgeLabel?: string;
}

export interface FormSectionHeaderProps {
  section: FormSectionMeta;
  isExpanded: boolean;
  onToggle: () => void;
  onKeyDown?: (e: KeyboardEvent<HTMLButtonElement>) => void;
  buttonRef?: Ref<HTMLButtonElement>;
  badge?: ReactNode;
  badgeTone?: BadgeTone;
  className?: string;
}

/**
 * Accordion Header Component for collapsible form sections.
 * Adheres to WAI-ARIA Accordion Pattern (APG) with accessible button trigger,
 * animated chevron indicator (rotated 180deg when expanded), and design-system styling.
 */
export function FormSectionHeader({
  section,
  isExpanded,
  onToggle,
  onKeyDown,
  buttonRef,
  badge,
  badgeTone = "neutral",
  className = "",
}: FormSectionHeaderProps) {
  const isCollapsible = section.collapsible !== false;

  const handleClick = () => {
    if (isCollapsible) {
      onToggle();
    }
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (onKeyDown) {
      onKeyDown(e);
      if (e.defaultPrevented) return;
    }
    if ((e.key === "Enter" || e.key === " ") && isCollapsible) {
      e.preventDefault();
      onToggle();
    }
  };

  const badgeContent =
    badge ??
    (section.badgeCount !== undefined ? (
      <Badge tone={badgeTone} className="tabular font-normal text-xs">
        {section.badgeCount}
        {section.badgeLabel ? ` ${section.badgeLabel}` : ""}
      </Badge>
    ) : null);

  return (
    <button
      ref={buttonRef}
      type="button"
      id={`section-${section.id}-header`}
      aria-expanded={isExpanded}
      aria-controls={`section-${section.id}-content`}
      aria-disabled={!isCollapsible ? true : undefined}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      className={`w-full min-h-[44px] px-3 py-2.5 flex items-center justify-between gap-3 text-left bg-surface-2 hover:bg-surface-3 border border-border rounded-lg text-sm font-medium text-fg transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${className}`.trim()}
    >
      <div className="flex items-center gap-2.5 min-w-0">
        {isCollapsible ? (
          <ChevronDown
            size={16}
            aria-hidden="true"
            className={`shrink-0 text-fg-muted transition-transform duration-180 ease-out motion-reduce:transition-none ${
              isExpanded ? "rotate-180" : "rotate-0"
            }`}
          />
        ) : null}
        <span className="truncate">{section.title}</span>
      </div>
      {badgeContent}
    </button>
  );
}

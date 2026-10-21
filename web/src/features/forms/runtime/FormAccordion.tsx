import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
} from "react";
import {
  FormSectionHeader,
  type FormSectionHeaderProps,
  type FormSectionMeta,
} from "./FormSectionHeader";
import { type BadgeTone } from "@/components/ui";

export { FormSectionHeader, type FormSectionHeaderProps, type FormSectionMeta };

export interface FormAccordionItemProps {
  section: FormSectionMeta;
  isExpanded: boolean;
  onToggle: () => void;
  onKeyDown?: (e: KeyboardEvent<HTMLButtonElement>) => void;
  children: ReactNode;
  badge?: ReactNode;
  badgeTone?: BadgeTone;
  className?: string;
  headerClassName?: string;
  contentClassName?: string;
  buttonRef?: Ref<HTMLButtonElement>;
  hasError?: boolean;
  onAutoExpand?: (sectionId: string) => void;
}

export function FormAccordionItem({
  section,
  isExpanded,
  onToggle,
  onKeyDown,
  children,
  badge,
  badgeTone,
  className = "",
  headerClassName = "",
  contentClassName = "",
  buttonRef,
  hasError = false,
  onAutoExpand,
}: FormAccordionItemProps) {
  // Smart auto-expansion on error
  useEffect(() => {
    if (hasError && !isExpanded) {
      onToggle();
      onAutoExpand?.(section.id);
    }
  }, [hasError, isExpanded, onToggle, onAutoExpand, section.id]);

  return (
    <div className={`flex flex-col ${className}`.trim()}>
      <FormSectionHeader
        section={section}
        isExpanded={isExpanded}
        onToggle={onToggle}
        onKeyDown={onKeyDown}
        buttonRef={buttonRef}
        badge={badge}
        badgeTone={badgeTone}
        className={headerClassName}
      />
      <div
        id={`section-${section.id}-content`}
        role="region"
        aria-labelledby={`section-${section.id}-header`}
        className={`grid transition-[grid-template-rows] duration-180 ease-out motion-reduce:transition-none ${
          isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
        } ${contentClassName}`.trim()}
        style={{
          gridTemplateRows: isExpanded ? "1fr" : "0fr",
        }}
      >
        <div className="min-h-0 overflow-hidden">{children}</div>
      </div>
    </div>
  );
}

export interface AccordionSectionConfig {
  section: FormSectionMeta;
  content: ReactNode;
  badge?: ReactNode;
  badgeTone?: BadgeTone;
  hasError?: boolean;
}

export interface FormAccordionProps {
  items?: AccordionSectionConfig[];
  children?: ReactNode;
  expandedSections?: Record<string, boolean>;
  defaultExpandedSections?: Record<string, boolean>;
  onToggleSection?: (sectionId: string) => void;
  onAutoExpand?: (sectionId: string) => void;
  className?: string;
}

/**
 * Accessible Form Accordion wrapper implementing WAI-ARIA APG accordion keyboard pattern:
 * - ArrowDown / ArrowUp moves focus between section headers with circular wrap.
 * - Home / End moves focus to first / last section header.
 * - Enter / Space toggles expand/collapse.
 * - Region has role="region" and aria-labelledby matching header id.
 * - Smooth CSS Grid height transition.
 */
export function FormAccordion({
  items,
  children,
  expandedSections: controlledExpanded,
  defaultExpandedSections,
  onToggleSection,
  onAutoExpand,
  className = "",
}: FormAccordionProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [internalExpanded, setInternalExpanded] = useState<Record<string, boolean>>(() => {
    if (defaultExpandedSections) return { ...defaultExpandedSections };
    const init: Record<string, boolean> = {};
    if (items) {
      for (const item of items) {
        init[item.section.id] = item.section.defaultExpanded ?? true;
      }
    }
    return init;
  });

  const isControlled = controlledExpanded !== undefined;
  const isExpanded = (id: string, fallbackDefault = true) => {
    if (isControlled) {
      return controlledExpanded[id] ?? fallbackDefault;
    }
    return internalExpanded[id] ?? fallbackDefault;
  };

  const handleToggle = useCallback(
    (id: string) => {
      if (onToggleSection) {
        onToggleSection(id);
      }
      if (!isControlled) {
        setInternalExpanded((prev) => ({
          ...prev,
          [id]: !prev[id],
        }));
      }
    },
    [isControlled, onToggleSection],
  );

  const handleAutoExpand = useCallback(
    (id: string) => {
      onAutoExpand?.(id);
      if (!isControlled) {
        setInternalExpanded((prev) => ({
          ...prev,
          [id]: true,
        }));
      }
    },
    [isControlled, onAutoExpand],
  );

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement | null;
    if (!target || !containerRef.current) return;

    // Check if target is a section header button
    const headerBtn = target.closest<HTMLButtonElement>('button[id^="section-"][id$="-header"]');
    if (!headerBtn || !containerRef.current.contains(headerBtn)) return;

    const headers = Array.from(
      containerRef.current.querySelectorAll<HTMLButtonElement>('button[id^="section-"][id$="-header"]'),
    );
    if (headers.length === 0) return;

    const currentIndex = headers.indexOf(headerBtn);
    if (currentIndex === -1) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      const nextIndex = (currentIndex + 1) % headers.length;
      headers[nextIndex]?.focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      const prevIndex = (currentIndex - 1 + headers.length) % headers.length;
      headers[prevIndex]?.focus();
    } else if (e.key === "Home") {
      e.preventDefault();
      headers[0]?.focus();
    } else if (e.key === "End") {
      e.preventDefault();
      headers[headers.length - 1]?.focus();
    }
  };

  return (
    <div
      ref={containerRef}
      data-form-accordion="true"
      onKeyDown={handleKeyDown}
      className={`w-full flex flex-col gap-2 ${className}`.trim()}
    >
      {items
        ? items.map((item) => (
            <FormAccordionItem
              key={item.section.id}
              section={item.section}
              isExpanded={isExpanded(item.section.id, item.section.defaultExpanded ?? true)}
              onToggle={() => handleToggle(item.section.id)}
              badge={item.badge}
              badgeTone={item.badgeTone}
              hasError={item.hasError}
              onAutoExpand={handleAutoExpand}
            >
              {item.content}
            </FormAccordionItem>
          ))
        : children}
    </div>
  );
}

export function useAccordionState(
  sections: FormSectionMeta[],
  options?: {
    defaultExpanded?: Record<string, boolean>;
    onAutoExpand?: (sectionId: string) => void;
  },
) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>(() => {
    const init: Record<string, boolean> = {};
    for (const s of sections) {
      init[s.id] = options?.defaultExpanded?.[s.id] ?? s.defaultExpanded ?? true;
    }
    return init;
  });

  const toggle = useCallback((id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }));
  }, []);

  const expand = useCallback((id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: true }));
  }, []);

  const collapse = useCallback((id: string) => {
    setExpanded((prev) => ({ ...prev, [id]: false }));
  }, []);

  const autoExpand = useCallback(
    (id: string) => {
      setExpanded((prev) => {
        if (!prev[id]) {
          options?.onAutoExpand?.(id);
          return { ...prev, [id]: true };
        }
        return prev;
      });
    },
    [options],
  );

  return {
    expanded,
    isExpanded: (id: string) => expanded[id] ?? true,
    toggle,
    expand,
    collapse,
    autoExpand,
    setExpanded,
  };
}

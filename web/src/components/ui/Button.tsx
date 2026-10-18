import type { ButtonHTMLAttributes, ReactElement, ReactNode, Ref } from "react";
import { Tip } from "@/components/Tip";
import { btn, btnDanger, btnGhost, btnGhostSm, btnPrimary, btnSm, iconBtn, iconBtnSm } from "@/components/styles";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";

const CLASSES: Record<ButtonVariant, { md: string; sm: string }> = {
  primary: { md: btnPrimary, sm: btnPrimary.replace("h-8 px-3", "h-7 px-2.5 text-xs") },
  secondary: { md: btn, sm: btnSm },
  ghost: { md: btnGhost, sm: btnGhostSm },
  danger: { md: btnDanger, sm: btnDanger.replace("h-8 px-3", "h-7 px-2.5 text-xs") },
};

/** The class string for a button variant, for elements that cannot be a `Button` (Radix `asChild`, links). */
export function buttonClass(variant: ButtonVariant = "secondary", size: "md" | "sm" = "md"): string {
  return CLASSES[variant][size];
}

type NativeButton = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "title">;

/**
 * A text button (DESIGN.md principle 2): `primary` for the one main action of a view, `secondary`
 * (the default) for most actions, `ghost` for toolbars and `danger` only inside a confirmation.
 * It is `type="button"` unless a type is given, so it never submits a form by accident.
 */
export function Button({
  variant = "secondary",
  size = "md",
  className = "",
  type = "button",
  ref,
  ...rest
}: NativeButton & { variant?: ButtonVariant; size?: "md" | "sm"; ref?: Ref<HTMLButtonElement> }) {
  return <button ref={ref} type={type} className={`${buttonClass(variant, size)} ${className}`.trim()} {...rest} />;
}

/**
 * An icon-only button. `label` is required: it is the accessible name and the tooltip text, so the
 * two never drift apart (DESIGN.md "Tooltips"). Pass `shortcut` to show the key in the tooltip.
 */
export function IconButton({
  label,
  shortcut,
  tipSide,
  size = "md",
  className = "",
  type = "button",
  children,
  ref,
  ...rest
}: Omit<NativeButton, "aria-label" | "children"> & {
  label: string;
  shortcut?: string;
  tipSide?: "top" | "right" | "bottom" | "left";
  size?: "md" | "sm";
  children: ReactNode;
  ref?: Ref<HTMLButtonElement>;
}): ReactElement {
  return (
    <Tip label={label} shortcut={shortcut} side={tipSide}>
      <button ref={ref} type={type} aria-label={label} className={`${size === "sm" ? iconBtnSm : iconBtn} ${className}`.trim()} {...rest}>
        {children}
      </button>
    </Tip>
  );
}

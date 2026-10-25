/**
 * Mobile touch ergonomics and virtual keyboard dismissal helpers.
 * Tailored for iOS Safari and mobile touch browsers.
 */

/**
 * Dismisses any active input or textarea when tapping on non-interactive
 * background/padding, while ensuring taps on interactive children
 * (buttons, inputs, menus) proceed without premature blur.
 */
export function dismissActiveInput(target?: EventTarget | null): void {
  if (target instanceof HTMLElement) {
    if (
      target.closest(
        "input, textarea, button, select, [role='button'], [role='menuitem'], [role='combobox'], [role='option'], [contenteditable='true'], label, a",
      )
    ) {
      return;
    }
  }
  if (
    document.activeElement instanceof HTMLElement &&
    (document.activeElement.tagName === "INPUT" || document.activeElement.tagName === "TEXTAREA")
  ) {
    document.activeElement.blur();
  }
}

/** True when the user asked the OS for less motion; gestures then skip drag animation and springs. */
export function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/** A short vibration where the device supports it (Android browsers); a silent no-op elsewhere. */
export function haptic(ms: number): void {
  try {
    if (typeof navigator !== "undefined" && typeof navigator.vibrate === "function") navigator.vibrate(ms);
  } catch {
    // Vibration may be blocked by permissions policy; it is only a cue.
  }
}

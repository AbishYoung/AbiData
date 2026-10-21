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

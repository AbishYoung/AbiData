/**
 * Copies text to the clipboard. The async Clipboard API needs a secure context (https or
 * localhost); on a plain-http LAN address the older copy command is used instead.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall back below
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  const prev = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  document.body.appendChild(area);
  area.select();
  let ok: boolean;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  area.remove();
  prev?.focus({ preventScroll: true });
  return ok;
}

/** Reads text from the clipboard, or null when the browser does not allow it (then Mod+V still works). */
export async function readClipboardText(): Promise<string | null> {
  try {
    if (navigator.clipboard?.readText) return await navigator.clipboard.readText();
  } catch {
    // denied or unavailable
  }
  return null;
}

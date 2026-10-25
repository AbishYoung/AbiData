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

/**
 * Copies several flavors at once (e.g. text/plain, text/html and an app-specific type). Browsers
 * that refuse the app-specific type get the standard flavors; where the multi-flavor API is
 * blocked or missing, the plain text is copied as usual.
 */
export async function copyRich(formats: Readonly<Record<string, string>>): Promise<boolean> {
  const plain = formats["text/plain"] ?? "";
  if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
    const attempts: Record<string, Blob>[] = [
      // Chromium carries app-specific types under the "web " prefix.
      Object.fromEntries(
        Object.entries(formats).map(([m, v]) => {
          const type = m.startsWith("text/") ? m : `web ${m}`;
          return [type, new Blob([v], { type })];
        }),
      ),
      Object.fromEntries(Object.entries(formats).filter(([m]) => m.startsWith("text/")).map(([m, v]) => [m, new Blob([v], { type: m })])),
    ];
    for (const items of attempts) {
      try {
        await navigator.clipboard.write([new ClipboardItem(items)]);
        return true;
      } catch {
        // try the next set of flavors
      }
    }
  }
  return copyText(plain);
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

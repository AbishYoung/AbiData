import { describe, expect, it } from "vitest";
import { SHORTCUTS, ariaKeyShortcuts, detectPlatform, formatKeys, matchesShortcut } from "./shortcuts";

function key(k: string, mods: Partial<Record<"metaKey" | "ctrlKey" | "shiftKey" | "altKey", boolean>> = {}) {
  return { key: k, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...mods };
}

describe("shortcut registry", () => {
  it("has unique ids and no reserved browser chords", () => {
    expect(new Set(SHORTCUTS.map((s) => s.id)).size).toBe(SHORTCUTS.length);
    for (const s of SHORTCUTS) {
      expect(s.keys).not.toBe("F1");
      expect(s.keys).not.toMatch(/^Mod\+[NTWELP]$/);
    }
  });

  it("formats keys per platform", () => {
    expect(formatKeys("Mod+Shift+N", "other")).toBe("Ctrl+Shift+N");
    expect(formatKeys("Mod+Shift+N", "mac")).toBe("⌘ ⇧ N");
    expect(formatKeys("Alt+Up", "mac")).toBe("⌥ ↑");
    expect(formatKeys("Alt+Up", "other")).toBe("Alt+↑");
  });

  it("detects the platform", () => {
    expect(detectPlatform({ platform: "MacIntel", userAgent: "" })).toBe("mac");
    expect(detectPlatform({ platform: "Win32", userAgent: "Windows NT" })).toBe("other");
  });

  it("builds aria-keyshortcuts values", () => {
    expect(ariaKeyShortcuts("Mod+Shift+N")).toBe("Control+Shift+N Meta+Shift+N");
    expect(ariaKeyShortcuts("Alt+Up")).toBe("Alt+ArrowUp");
    expect(ariaKeyShortcuts("Esc")).toBe("Escape");
  });
});

describe("matchesShortcut", () => {
  it("matches Mod with either Ctrl or Cmd, and nothing extra", () => {
    expect(matchesShortcut(key("k", { ctrlKey: true }), "palette")).toBe(true);
    expect(matchesShortcut(key("K", { metaKey: true }), "palette")).toBe(true);
    expect(matchesShortcut(key("k"), "palette")).toBe(false);
    expect(matchesShortcut(key("k", { ctrlKey: true, shiftKey: true }), "palette")).toBe(false);
    expect(matchesShortcut(key("k", { ctrlKey: true, altKey: true }), "palette")).toBe(false);
  });

  it("matches the save chord", () => {
    expect(matchesShortcut(key("s", { metaKey: true }), "design.save")).toBe(true);
    expect(matchesShortcut(key("s"), "design.save")).toBe(false);
  });

  it("matches ? even though it is typed with Shift", () => {
    expect(matchesShortcut(key("?", { shiftKey: true }), "docs")).toBe(true);
    expect(matchesShortcut(key("?"), "docs")).toBe(true);
    expect(matchesShortcut(key("?", { ctrlKey: true }), "docs")).toBe(false);
  });

  it("matches named keys exactly", () => {
    expect(matchesShortcut(key("Enter", { shiftKey: true }), "grid.save")).toBe(true);
    expect(matchesShortcut(key("Enter"), "grid.save")).toBe(false);
  });
});

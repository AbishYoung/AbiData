import { afterEach, describe, expect, it, vi } from "vitest";
import { applyTheme, loadThemePref, useThemeStore } from "./theme";

afterEach(() => {
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("data-theme");
});

describe("theme preference", () => {
  it("falls back to system when localStorage throws on read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(loadThemePref()).toBe("system");
  });

  it("still applies the theme when localStorage throws on write", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    useThemeStore.getState().setPref("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(useThemeStore.getState().pref).toBe("dark");
  });

  it("removes the override for system", () => {
    applyTheme("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    applyTheme("system");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  });
});

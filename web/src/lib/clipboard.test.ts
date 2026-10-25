import { afterEach, describe, expect, it, vi } from "vitest";
import { copyRich } from "./clipboard";

class FakeItem {
  constructor(readonly items: Record<string, Blob>) {}
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const formats = { "text/plain": "a\tb", "text/html": "<table></table>", "application/x-abidata-cells": "{}" };

describe("copyRich", () => {
  it("writes every flavor, the app-specific one under the web prefix", async () => {
    const write = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("ClipboardItem", FakeItem);
    vi.stubGlobal("navigator", { clipboard: { write } });
    expect(await copyRich(formats)).toBe(true);
    const item = write.mock.calls[0]?.[0][0] as FakeItem;
    expect(Object.keys(item.items).sort()).toEqual(["text/html", "text/plain", "web application/x-abidata-cells"]);
  });

  it("retries with the standard flavors when the custom one is refused", async () => {
    const write = vi.fn().mockRejectedValueOnce(new Error("unsupported")).mockResolvedValue(undefined);
    vi.stubGlobal("ClipboardItem", FakeItem);
    vi.stubGlobal("navigator", { clipboard: { write } });
    expect(await copyRich(formats)).toBe(true);
    expect(write).toHaveBeenCalledTimes(2);
    const item = write.mock.calls[1]?.[0][0] as FakeItem;
    expect(Object.keys(item.items).sort()).toEqual(["text/html", "text/plain"]);
  });

  it("falls back to plain text when writing is blocked", async () => {
    const write = vi.fn().mockRejectedValue(new Error("blocked"));
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("ClipboardItem", FakeItem);
    vi.stubGlobal("navigator", { clipboard: { write, writeText } });
    expect(await copyRich(formats)).toBe(true);
    expect(writeText).toHaveBeenCalledWith("a\tb");
  });
});

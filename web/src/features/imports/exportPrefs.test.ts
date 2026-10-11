import { afterEach, describe, expect, it, vi } from "vitest";
import { loadCsvBom, saveCsvBom } from "./exportPrefs";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubStorage(store: Map<string, string>): void {
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
  });
}

describe("csv bom preference", () => {
  it("defaults to on", () => {
    stubStorage(new Map());
    expect(loadCsvBom()).toBe(true);
  });

  it("remembers the last choice", () => {
    stubStorage(new Map());
    saveCsvBom(false);
    expect(loadCsvBom()).toBe(false);
    saveCsvBom(true);
    expect(loadCsvBom()).toBe(true);
  });

  it("survives storage that throws", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    });
    expect(() => saveCsvBom(false)).not.toThrow();
    expect(loadCsvBom()).toBe(true);
  });
});

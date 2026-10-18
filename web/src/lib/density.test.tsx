import { afterEach, describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { loadDensity, rowHeightFor, useDensityStore, useRowHeight } from "./density";

describe("density", () => {
  afterEach(() => {
    window.localStorage.clear();
    act(() => useDensityStore.setState({ density: "compact" }));
  });

  it("defaults to compact 32px rows", () => {
    expect(loadDensity()).toBe("compact");
    expect(rowHeightFor("compact")).toBe(32);
    expect(rowHeightFor("comfortable")).toBe(40);
  });

  it("remembers the choice and resizes rows", () => {
    const { result } = renderHook(() => useRowHeight());
    expect(result.current).toBe(32);
    act(() => useDensityStore.getState().setDensity("comfortable"));
    expect(result.current).toBe(40);
    expect(loadDensity()).toBe("comfortable");
  });

  it("ignores an unknown stored value", () => {
    window.localStorage.setItem("abidata.density", "huge");
    expect(loadDensity()).toBe("compact");
  });
});

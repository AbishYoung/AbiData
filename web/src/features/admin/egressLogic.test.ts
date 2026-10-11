import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api";
import { errorTarget, isDuplicate, normalizePattern, patternError, portsError } from "./egressLogic";

describe("patternError (mirrors the server's ValidPattern)", () => {
  it("accepts hosts, wildcards and IP literals", () => {
    for (const p of ["api.example.com", "*.example.com", "1.2.3.4", "[2606:4700::1]", " API.Example.com. ", "localhost", "a-b.c1.test"]) {
      expect(patternError(p), p).toBeNull();
    }
  });
  it("rejects what the server rejects", () => {
    for (const p of ["", "*", "*.", "a..b", "exa mple.com", "http://x.com", "x.com/path", "*.1.2.3.4", "a_b.com", "*.*.com", "x.com:8080", "u@x.com"]) {
      expect(patternError(p), p).not.toBeNull();
    }
  });
  it("normalises like the server", () => {
    expect(normalizePattern("  API.Example.com. ")).toBe("api.example.com");
  });
});

describe("portsError", () => {
  it("accepts empty and valid lists", () => {
    for (const p of ["", "  ", "443", "443, 8443", "1,65535"]) expect(portsError(p), p).toBeNull();
  });
  it("rejects bad lists", () => {
    for (const p of ["0", "443,x", "70000", "443,", "-1", "4.5"]) expect(portsError(p), p).not.toBeNull();
  });
});

describe("isDuplicate", () => {
  it("flags only the later of two equal patterns", () => {
    const rows = [{ pattern: "A.com" }, { pattern: "b.com" }, { pattern: "a.com." }, { pattern: "" }, { pattern: "" }];
    expect(rows.map((_, i) => isDuplicate(rows, i))).toEqual([false, false, true, false, false]);
  });
});

describe("errorTarget", () => {
  it("reads the row and field from server details", () => {
    const e = new ApiError({ status: 400, code: "BAD_REQUEST", title: "t", message: "m", details: { index: 2, field: "ports" } } as never);
    expect(errorTarget(e)).toEqual({ index: 2, field: "ports" });
    expect(errorTarget(new Error("x"))).toBeNull();
  });
});

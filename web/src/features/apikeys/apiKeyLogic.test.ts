import { describe, expect, it } from "vitest";
import { EXPIRY_OPTIONS, ROLE_OPTIONS, keyNameError, keyStatus } from "./apiKeyLogic";

describe("keyStatus", () => {
  it("prefers revoked over expired", () => {
    expect(keyStatus({ revoked: true, expired: true })).toBe("revoked");
    expect(keyStatus({ revoked: false, expired: true })).toBe("expired");
    expect(keyStatus({ revoked: false, expired: false })).toBe("active");
  });
});

describe("keyNameError", () => {
  it("accepts ordinary names", () => {
    expect(keyNameError("Nightly sync")).toBeNull();
    expect(keyNameError("  Åsa's bot  ")).toBeNull();
  });
  it("rejects empty, long and unsafe names", () => {
    expect(keyNameError("   ")).not.toBeNull();
    expect(keyNameError("x".repeat(101))).not.toBeNull();
    expect(keyNameError('say "hi"')).not.toBeNull();
    expect(keyNameError("a\nb")).not.toBeNull();
  });
});

describe("options", () => {
  it("never offers the owner role", () => {
    expect(ROLE_OPTIONS.map((r) => r.value)).toEqual(["viewer", "editor", "builder"]);
  });
  it("includes a non-expiring choice", () => {
    expect(EXPIRY_OPTIONS.some((o) => o.days === null)).toBe(true);
  });
});

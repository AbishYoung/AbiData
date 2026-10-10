import { describe, expect, it } from "vitest";
import { formatSize, newWorkspaceNameError, relativeTime, restoreConfirmed } from "./backupLogic";

describe("restoreConfirmed", () => {
  it("needs the exact workspace name", () => {
    expect(restoreConfirmed("Sales", "Sales")).toBe(true);
    expect(restoreConfirmed("  Sales ", "Sales")).toBe(true);
    expect(restoreConfirmed("sales", "Sales")).toBe(false);
    expect(restoreConfirmed("Sale", "Sales")).toBe(false);
    expect(restoreConfirmed("", "Sales")).toBe(false);
  });
  it("never confirms against an unknown (empty) workspace name", () => {
    expect(restoreConfirmed("", "")).toBe(false);
    expect(restoreConfirmed("x", "")).toBe(false);
  });
});

describe("newWorkspaceNameError", () => {
  it("rejects blank and over-long names", () => {
    expect(newWorkspaceNameError("   ")).not.toBeNull();
    expect(newWorkspaceNameError("a".repeat(101))).not.toBeNull();
    expect(newWorkspaceNameError("Copy of Sales")).toBeNull();
  });
});

describe("formatting", () => {
  it("formats sizes", () => {
    expect(formatSize(12)).toBe("12 B");
    expect(formatSize(2048)).toBe("2.0 KiB");
    expect(formatSize(5 * 1024 * 1024)).toBe("5.0 MiB");
  });
  it("formats relative times", () => {
    const now = Date.parse("2026-01-10T12:00:00Z");
    expect(relativeTime("2026-01-10T11:59:50Z", now)).toBe("just now");
    expect(relativeTime("2026-01-10T11:55:00Z", now)).toBe("5 minutes ago");
    expect(relativeTime("2026-01-09T12:00:00Z", now)).toBe("yesterday");
  });
});

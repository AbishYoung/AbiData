import { describe, expect, it } from "vitest";
import { parseAdminTab } from "./AdminPage";

describe("parseAdminTab", () => {
  it("accepts known tabs", () => {
    expect(parseAdminTab("users")).toBe("users");
    expect(parseAdminTab("api-keys")).toBe("api-keys");
  });
  it("falls back to overview", () => {
    expect(parseAdminTab(undefined)).toBe("overview");
    expect(parseAdminTab("nope")).toBe("overview");
    expect(parseAdminTab(3)).toBe("overview");
  });
});

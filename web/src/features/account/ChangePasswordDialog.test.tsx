import { describe, expect, it } from "vitest";
import { validatePasswordChange } from "./ChangePasswordDialog";

describe("validatePasswordChange", () => {
  it("accepts a long enough matching password", () => {
    expect(validatePasswordChange("old-password", "a-new-passphrase", "a-new-passphrase")).toEqual({});
  });

  it("requires the current password", () => {
    expect(validatePasswordChange("", "a-new-passphrase", "a-new-passphrase").current).toBeDefined();
  });

  it("enforces the server's length rules", () => {
    expect(validatePasswordChange("x", "123456789", "123456789").next).toMatch(/at least 10/);
    expect(validatePasswordChange("x", "1234567890", "1234567890").next).toBeUndefined();
    expect(validatePasswordChange("x", "a".repeat(1025), "a".repeat(1025)).next).toMatch(/at most/);
    // Length counts bytes, as on the server: five two-byte characters make ten.
    expect(validatePasswordChange("x", "ééééé", "ééééé").next).toBeUndefined();
  });

  it("requires the confirmation to match and the password to change", () => {
    expect(validatePasswordChange("x", "a-new-passphrase", "a-new-passphras").confirm).toBeDefined();
    expect(validatePasswordChange("same-password", "same-password", "same-password").next).toMatch(/different/);
  });
});

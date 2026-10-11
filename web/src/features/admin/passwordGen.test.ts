import { describe, expect, it } from "vitest";
import { GENERATED_PASSWORD_LENGTH, PASSWORD_ALPHABET, generatePassword } from "./passwordGen";

describe("generatePassword", () => {
  it("makes 20 characters from the unambiguous alphabet", () => {
    const pw = generatePassword();
    expect(pw).toHaveLength(GENERATED_PASSWORD_LENGTH);
    for (const ch of pw) expect(PASSWORD_ALPHABET).toContain(ch);
    expect(pw).not.toMatch(/[Il1O0]/);
  });

  it("differs between calls", () => {
    expect(generatePassword()).not.toBe(generatePassword());
  });

  it("rejects out-of-range draws instead of biasing the alphabet", () => {
    let call = 0;
    const fill = (a: Uint32Array) => {
      // First batch is all above the unbiased limit, so none may be used.
      a.fill(call++ === 0 ? 0xffffffff : 1);
      return a;
    };
    expect(generatePassword(5, fill)).toBe(PASSWORD_ALPHABET.charAt(1).repeat(5));
  });

  it("honours the length", () => {
    expect(generatePassword(32)).toHaveLength(32);
  });
});

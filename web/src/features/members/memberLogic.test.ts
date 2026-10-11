import { describe, expect, it } from "vitest";
import { activeKeysCreatedBy, emailError, initials, isDemotion, isLastOwner } from "./memberLogic";
import type { ApiKey } from "@/features/apikeys/apiKeysApi";
import type { Member } from "./membersApi";

const m = (user_id: string, role: Member["role"]): Member => ({ user_id, email: `${user_id}@x.co`, display_name: user_id, role });

describe("memberLogic", () => {
  it("detects demotions", () => {
    expect(isDemotion("owner", "builder")).toBe(true);
    expect(isDemotion("viewer", "editor")).toBe(false);
    expect(isDemotion("editor", "editor")).toBe(false);
  });
  it("makes initials", () => {
    expect(initials("Ada Lovelace", "a@x.co")).toBe("AL");
    expect(initials("", "bob@x.co")).toBe("BO");
    expect(initials("", "")).toBe("?");
  });
  it("finds the last owner", () => {
    expect(isLastOwner([m("a", "owner"), m("b", "editor")], m("a", "owner"))).toBe(true);
    expect(isLastOwner([m("a", "owner"), m("b", "owner")], m("a", "owner"))).toBe(false);
  });
  it("lists only active keys by creator", () => {
    const k = (id: string, by: string, revoked = false, expired = false) => ({ id, created_by: by, revoked, expired }) as ApiKey;
    expect(activeKeysCreatedBy([k("1", "u"), k("2", "u", true), k("3", "u", false, true), k("4", "v")], "u").map((x) => x.id)).toEqual(["1"]);
  });
  it("validates emails", () => {
    expect(emailError("")).not.toBeNull();
    expect(emailError("nope")).not.toBeNull();
    expect(emailError("a@b.co")).toBeNull();
  });
});

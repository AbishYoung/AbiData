import type { ApiKey } from "@/features/apikeys/apiKeysApi";
import type { Member, MemberRole } from "./membersApi";

export const MEMBER_ROLES: readonly { value: MemberRole; label: string }[] = [
  { value: "viewer", label: "Viewer" },
  { value: "editor", label: "Editor" },
  { value: "builder", label: "Builder" },
  { value: "owner", label: "Owner" },
];

const RANK: Record<MemberRole, number> = { viewer: 0, editor: 1, builder: 2, owner: 3 };

export function roleLabel(r: MemberRole): string {
  return MEMBER_ROLES.find((x) => x.value === r)?.label ?? r;
}

export function isDemotion(from: MemberRole, to: MemberRole): boolean {
  return RANK[to] < RANK[from];
}

/** Up to two initials for the avatar; falls back to the email, then "?". */
export function initials(name: string, email: string): string {
  const src = name.trim() || (email.split("@")[0] ?? "").trim();
  const parts = src.split(/[\s@._-]+/u).filter(Boolean);
  const [a = "?", b] = parts;
  const out = b ? (a.charAt(0) + b.charAt(0)) : a.slice(0, 2);
  return out.toUpperCase();
}

export function isLastOwner(members: Member[], m: Member): boolean {
  return m.role === "owner" && members.filter((x) => x.role === "owner").length <= 1;
}

/** Active (not revoked, not expired) keys that `userId` created. */
export function activeKeysCreatedBy(keys: ApiKey[], userId: string): ApiKey[] {
  return keys.filter((k) => k.created_by === userId && !k.revoked && !k.expired);
}

export function emailError(email: string): string | null {
  const t = email.trim();
  if (t === "") return "Enter the person's email address.";
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/u.test(t)) return "Enter a valid email address.";
  return null;
}

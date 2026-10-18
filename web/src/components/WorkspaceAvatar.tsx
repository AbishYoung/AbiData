/** Deep fills (tokens.css --avatar-*) that keep white initials at AA contrast in both themes. */
const AVATAR_FILLS = 7;

/** A coloured tile with the workspace's initials; the colour is stable per name. */
export function WorkspaceAvatar({
  name,
  size = "md",
}: {
  name: string;
  size?: "md" | "lg" | "xl";
}) {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  const initials =
    name
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]?.toUpperCase())
      .join("") || "?";
  const box =
    size === "xl"
      ? "h-11 w-11 rounded-xl text-base"
      : size === "lg"
        ? "h-10 w-10 rounded-lg text-sm"
        : "h-7 w-7 rounded-md text-xs";
  return (
    <span
      aria-hidden="true"
      style={{ backgroundColor: `var(--avatar-${(h % AVATAR_FILLS) + 1})` }}
      // eslint-disable-next-line no-restricted-syntax -- documented exception: white initials on a solid avatar fill.
      className={`inline-flex shrink-0 items-center justify-center font-semibold text-white ${box}`}
    >
      {initials}
    </span>
  );
}

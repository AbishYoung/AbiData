/** What a link inside rendered docs HTML points to. */
export type DocLink =
  | { kind: "internal"; slug: string; anchor: string | null }
  | { kind: "anchor"; anchor: string }
  | { kind: "external" }
  | { kind: "other" };

const DOCS_PREFIX = "/docs/";

function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

/** Splits "guide/backups#restore" (a target with no leading /docs/) into slug and anchor. */
export function parseDocsTarget(target: string): { slug: string; anchor: string | null } {
  const hash = target.indexOf("#");
  const path = hash >= 0 ? target.slice(0, hash) : target;
  const anchor = hash >= 0 ? target.slice(hash + 1) : "";
  return { slug: path.replace(/^\/+|\/+$/g, ""), anchor: anchor === "" ? null : safeDecode(anchor) };
}

/** Joins a slug and anchor into a target string. */
export function docsTarget(slug: string, anchor?: string | null): string {
  return anchor ? `${slug}#${anchor}` : slug;
}

/**
 * Classifies a link `href` found in docs HTML. `origin` is the app's origin
 * (e.g. "https://abidata.example.com"); absolute links to it count as local.
 */
export function classifyDocLink(href: string | null, origin: string): DocLink {
  if (href === null || href.trim() === "") return { kind: "other" };
  const value = href.trim();
  if (value.startsWith("#")) {
    return value.length > 1 ? { kind: "anchor", anchor: safeDecode(value.slice(1)) } : { kind: "other" };
  }
  let local = value;
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) {
    let url: URL;
    try {
      url = new URL(value);
    } catch {
      return { kind: "other" };
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") return { kind: "other" };
    if (url.origin !== origin) return { kind: "external" };
    local = url.pathname + url.hash;
  } else if (value.startsWith("//")) {
    return { kind: "external" };
  }
  if (!local.startsWith(DOCS_PREFIX)) return { kind: "other" };
  const { slug, anchor } = parseDocsTarget(local.slice(DOCS_PREFIX.length));
  return slug === "" ? { kind: "other" } : { kind: "internal", slug, anchor };
}

/**
 * Prepares trusted docs HTML for display: external links open in a new tab
 * with rel="noopener noreferrer", and tables and code blocks become keyboard-scrollable.
 */
export function prepareDocHtml(html: string, origin: string): string {
  const doc = new DOMParser().parseFromString(`<body>${html}</body>`, "text/html");
  for (const a of doc.body.querySelectorAll("a[href]")) {
    if (classifyDocLink(a.getAttribute("href"), origin).kind === "external") {
      a.setAttribute("target", "_blank");
      a.setAttribute("rel", "noopener noreferrer");
    }
  }
  for (const table of doc.body.querySelectorAll("table")) {
    const wrap = doc.createElement("div");
    wrap.className = "docs-scroll";
    wrap.tabIndex = 0;
    wrap.setAttribute("role", "region");
    wrap.setAttribute("aria-label", "Table");
    table.replaceWith(wrap);
    wrap.append(table);
  }
  for (const pre of doc.body.querySelectorAll("pre")) {
    pre.tabIndex = 0;
  }
  return doc.body.innerHTML;
}

/** The anchor id for an error code in the generated error reference. */
export function errorAnchor(code: string): string {
  return code.toLowerCase().replaceAll("_", "-");
}

/** The docs target of an error code's section. */
export function errorDocsTarget(code: string): string {
  return docsTarget("reference/errors", errorAnchor(code));
}

/** The anchor id for a Lua function or module name in the generated API reference ("db.query" becomes "db-query"). */
export function luaAnchor(name: string): string {
  return name.toLowerCase().replaceAll(/[._:]/g, "-");
}

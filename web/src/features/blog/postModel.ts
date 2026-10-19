// Pure rules of the blog editor (ADR 0017): slug following and locking, the actions a post's
// state allows, chip labels and the Markdown toolbar's edits. The server stays authoritative.

import type { Chip, PostStatus } from "./blogApi";

export const MAX_TITLE = 200;
export const MAX_SUMMARY = 300;
export const MAX_BODY_BYTES = 200 * 1024;
export const MAX_SLUG = 80;
export const MAX_COVER_ALT = 200;

const TRANSLIT: Record<string, string> = { ß: "ss", æ: "ae", Æ: "ae", ø: "o", Ø: "o", œ: "oe", Œ: "oe", đ: "d", Đ: "d", ł: "l", Ł: "l", þ: "th", Þ: "th", ð: "d", Ð: "d", ı: "i", "&": " and " };

/** The client's copy of the server's Slugify, for showing the address while typing. */
export function slugify(s: string): string {
  const pre = Array.from(s, (ch) => TRANSLIT[ch] ?? ch).join("");
  let out = "";
  let hyphen = false;
  for (const ch of pre.normalize("NFD")) {
    if (/\p{Mn}/u.test(ch)) continue;
    const lower = ch.toLowerCase();
    if (/^[a-z0-9]$/.test(lower) && /^[A-Za-z0-9]$/.test(ch)) {
      if (hyphen && out.length > 0) out += "-";
      out += lower;
      hyphen = false;
    } else {
      hyphen = true;
    }
  }
  if (out.length > MAX_SLUG) {
    out = out.slice(0, MAX_SLUG);
    const i = out.lastIndexOf("-");
    if (i > MAX_SLUG / 2) out = out.slice(0, i);
  }
  return out.replace(/^-+|-+$/g, "");
}

export interface SlugState {
  slug: string;
  /** Once the user edits the slug, or the post is first published, it no longer follows the title. */
  locked: boolean;
}

export function initialSlug(slug: string, everPublished: boolean, isNew: boolean): SlugState {
  return { slug, locked: everPublished || !isNew };
}

export function followTitle(state: SlugState, title: string): SlugState {
  return state.locked ? state : { slug: slugify(title), locked: false };
}

export function editSlug(slug: string): SlugState {
  return { slug, locked: true };
}

export type PostAction = "save_draft" | "publish" | "publish_unlisted" | "update" | "unlist" | "unpublish" | "save";

export const ACTION_LABEL: Record<PostAction, string> = {
  save_draft: "Save draft",
  publish: "Publish",
  publish_unlisted: "Publish unlisted",
  update: "Update",
  unlist: "Unlist",
  unpublish: "Unpublish",
  save: "Save",
};

/** The actions a post's state offers, the primary one first. */
export function actionsFor(status: PostStatus | "new", takenDown: boolean): PostAction[] {
  if (takenDown) return ["save"];
  switch (status) {
    case "published":
      return ["update", "unlist", "unpublish"];
    case "unlisted":
      return ["update", "publish", "unpublish"];
    default:
      return ["save_draft", "publish", "publish_unlisted"];
  }
}

/** The status an action moves the post to, or null for a plain save. */
export function targetStatus(action: PostAction): PostStatus | null {
  switch (action) {
    case "publish":
      return "published";
    case "publish_unlisted":
    case "unlist":
      return "unlisted";
    case "unpublish":
      return "draft";
  }
  return null;
}

export const CHIP_LABEL: Record<Chip, string> = {
  draft: "Draft",
  published: "Published",
  unlisted: "Unlisted",
  taken_down: "Taken down",
  needs_attention: "Needs attention",
};

export const CHIP_TONE: Record<Chip, "neutral" | "accent" | "success" | "warning" | "danger"> = {
  draft: "neutral",
  published: "success",
  unlisted: "accent",
  taken_down: "danger",
  needs_attention: "warning",
};

export function byteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

/** A note on the body's size once it is near the limit (90 %), else null. */
export function bodySizeNote(bytes: number): string | null {
  if (bytes < MAX_BODY_BYTES * 0.9) return null;
  const kb = (n: number) => `${Math.ceil(n / 1024).toLocaleString("en")} KB`;
  return bytes > MAX_BODY_BYTES ? `${kb(bytes)} of ${kb(MAX_BODY_BYTES)}: too long to save.` : `${kb(bytes)} of ${kb(MAX_BODY_BYTES)}.`;
}

export type MarkdownKind = "heading" | "bold" | "italic" | "link" | "list" | "quote" | "code" | "image";

export interface TextEdit {
  text: string;
  selStart: number;
  selEnd: number;
}

/** Applies a toolbar button to the textarea's text and selection. */
export function applyMarkdown(kind: MarkdownKind, text: string, selStart: number, selEnd: number): TextEdit {
  const sel = text.slice(selStart, selEnd);
  const wrap = (before: string, after: string, placeholder: string): TextEdit => {
    const inner = sel || placeholder;
    const next = text.slice(0, selStart) + before + inner + after + text.slice(selEnd);
    return { text: next, selStart: selStart + before.length, selEnd: selStart + before.length + inner.length };
  };
  const linePrefix = (prefix: string, placeholder: string): TextEdit => {
    const lineStart = text.lastIndexOf("\n", selStart - 1) + 1;
    const block = text.slice(lineStart, selEnd) || placeholder;
    const lines = block.split("\n").map((l) => prefix + l);
    const replaced = lines.join("\n");
    const next = text.slice(0, lineStart) + replaced + text.slice(Math.max(selEnd, lineStart));
    return { text: next, selStart: lineStart, selEnd: lineStart + replaced.length };
  };
  switch (kind) {
    case "bold":
      return wrap("**", "**", "bold text");
    case "italic":
      return wrap("_", "_", "italic text");
    case "code":
      return sel.includes("\n") ? wrap("```\n", "\n```", "code") : wrap("`", "`", "code");
    case "link": {
      const label = sel || "link text";
      const next = text.slice(0, selStart) + `[${label}](https://)` + text.slice(selEnd);
      const urlStart = selStart + label.length + 3;
      return { text: next, selStart: urlStart, selEnd: urlStart + "https://".length };
    }
    case "image": {
      const alt = sel || "description";
      const next = text.slice(0, selStart) + `![${alt}](attachment:FILE_ID)` + text.slice(selEnd);
      const idStart = selStart + alt.length + 4 + "attachment:".length;
      return { text: next, selStart: idStart, selEnd: idStart + "FILE_ID".length };
    }
    case "heading":
      return linePrefix("## ", "Heading");
    case "list":
      return linePrefix("- ", "Item");
    case "quote":
      return linePrefix("> ", "Quote");
  }
}

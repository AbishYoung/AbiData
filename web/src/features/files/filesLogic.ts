// Pure logic of file attachments (ADR 0011): the value of an attachments cell is an ordered list
// of file ids, at most `max_files` long. Nothing here touches the network or the DOM.

/** The server's defaults (internal/catalog/attachments.go). */
export const DEFAULT_MAX_FILES = 20;
export const MAX_MAX_FILES = 100;
/** Default for the server's ABIDATA_MAX_FILE_BYTES; only used to word an upload that was cut off. */
export const DEFAULT_MAX_FILE_BYTES = 25 * 1024 * 1024;

/** The ids in a cell value: an array of ids, or the stored JSON text of one; anything else is empty. */
export function parseIds(value: unknown): string[] {
  let list: unknown = value;
  if (typeof value === "string") {
    const t = value.trim();
    if (!t.startsWith("[")) return [];
    try {
      list = JSON.parse(t);
    } catch {
      return [];
    }
  }
  if (!Array.isArray(list)) return [];
  const out: string[] = [];
  for (const x of list) if (typeof x === "string" && x !== "" && !out.includes(x)) out.push(x);
  return out;
}

/** The API value for a list of ids: an empty list is stored as null. */
export function toValue(ids: readonly string[]): string[] | null {
  return ids.length === 0 ? null : [...ids];
}

/** Whether two cell values hold the same ids in the same order. */
export function sameIds(a: unknown, b: unknown): boolean {
  const x = parseIds(a);
  const y = parseIds(b);
  return x.length === y.length && x.every((id, i) => id === y[i]);
}

/** The per-cell limit of an attachments column's type options. */
export function maxFilesOf(options: { max_files?: number | undefined } | undefined): number {
  const n = options?.max_files;
  return typeof n === "number" && Number.isInteger(n) && n >= 1 ? Math.min(n, MAX_MAX_FILES) : DEFAULT_MAX_FILES;
}

/** How many more files fit, counting uploads still running. Never negative. */
export function slotsLeft(max: number, count: number, uploading = 0): number {
  return Math.max(0, max - count - uploading);
}

/** Splits chosen files into those that fit the free slots (in order) and those that do not. */
export function planFiles<T>(files: readonly T[], slots: number): { accept: T[]; rejected: T[] } {
  const n = Math.max(0, slots);
  return { accept: files.slice(0, n), rejected: files.slice(n) };
}

/** Appends new ids (dropping ones already present), never beyond `max`. `dropped` counts ids that did not fit. */
export function addIds(ids: readonly string[], add: readonly string[], max: number): { ids: string[]; dropped: number } {
  const out = [...ids];
  let dropped = 0;
  for (const id of add) {
    if (out.includes(id)) continue;
    if (out.length >= max) {
      dropped++;
      continue;
    }
    out.push(id);
  }
  return { ids: out, dropped };
}

export function removeId(ids: readonly string[], id: string): string[] {
  return ids.filter((x) => x !== id);
}

/** Moves an id by `delta` places (negative = earlier), staying inside the list. */
export function moveId(ids: readonly string[], id: string, delta: number): string[] {
  const from = ids.indexOf(id);
  if (from < 0) return [...ids];
  const to = Math.min(Math.max(from + delta, 0), ids.length - 1);
  const next = [...ids];
  next.splice(from, 1);
  next.splice(to, 0, id);
  return next;
}

/** Moves an id to the place of `targetId` (drag and drop). */
export function moveIdTo(ids: readonly string[], id: string, targetId: string): string[] {
  const to = ids.indexOf(targetId);
  if (to < 0 || !ids.includes(id) || id === targetId) return [...ids];
  const next = ids.filter((x) => x !== id);
  next.splice(to, 0, id);
  return next;
}

/** "1 file", "3 files". */
export function countLabel(n: number): string {
  return `${n} ${n === 1 ? "file" : "files"}`;
}

/** Display text of a cell value, for places that show plain text (merge dialog, history, filters). */
export function summaryOf(value: unknown): string {
  const ids = parseIds(value);
  return ids.length === 0 ? "" : countLabel(ids.length);
}

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  return `${v >= 10 ? Math.round(v) : Math.round(v * 10) / 10} ${units[u]}`;
}

/** Image types the server serves inline (and the viewer can show). */
const INLINE_IMAGES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

export function isInlineImage(contentType: string): boolean {
  return INLINE_IMAGES.has(contentType.toLowerCase().split(";")[0]?.trim() ?? "");
}

export type FileKind = "image" | "pdf" | "text" | "sheet" | "archive" | "audio" | "video" | "file";

/** The icon family of a file: from the sniffed content type, with the name's extension as a fallback. */
export function fileKind(contentType: string, name: string): FileKind {
  const ct = contentType.toLowerCase();
  const ext = name.includes(".") ? (name.split(".").pop() ?? "").toLowerCase() : "";
  if (ct.startsWith("image/") || ["heic", "heif", "png", "jpg", "jpeg", "gif", "webp"].includes(ext)) return "image";
  if (ct === "application/pdf" || ext === "pdf") return "pdf";
  if (ct.startsWith("audio/")) return "audio";
  if (ct.startsWith("video/")) return "video";
  if (ct === "text/csv" || ["csv", "xls", "xlsx", "ods", "tsv"].includes(ext) || ct.includes("spreadsheet") || ct.includes("excel")) return "sheet";
  if (ct.startsWith("text/") || ["txt", "md", "doc", "docx", "odt", "rtf"].includes(ext) || ct.includes("word")) return "text";
  if (ct.includes("zip") || ct.includes("tar") || ct.includes("gzip") || ["zip", "gz", "tar", "7z"].includes(ext)) return "archive";
  return "file";
}

/** The label a file shows to assistive technology and tooltips: "name.png, 1.2 MB". */
export function fileLabel(name: string, size: number | undefined): string {
  return size === undefined ? name : `${name}, ${formatBytes(size)}`;
}

/** Wording for an upload that failed without an answer from the server (the connection was cut). */
export function cutOffMessage(fileSize: number): string {
  return fileSize > DEFAULT_MAX_FILE_BYTES
    ? `This file is ${formatBytes(fileSize)}, which is probably larger than the server accepts (${formatBytes(DEFAULT_MAX_FILE_BYTES)} unless your administrator changed it).`
    : "The upload was interrupted. Check your connection and try again.";
}

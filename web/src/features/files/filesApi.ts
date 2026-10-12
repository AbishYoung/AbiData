import { useQueries, type QueryClient } from "@tanstack/react-query";
import type { components } from "@/generated/api";
import { ApiError, api, errorFromResponse } from "@/lib/api";
import { useSessionStore } from "@/lib/sessionStore";
import { cutOffMessage } from "./filesLogic";

export type FileMeta = components["schemas"]["Meta"];

const base = (ws: string): string => `/api/v1/w/${encodeURIComponent(ws)}/files`;

/** The bytes of a file; images are shown in place. */
export function contentUrl(ws: string, id: string): string {
  return `${base(ws)}/${encodeURIComponent(id)}/content`;
}

/** The bytes as a download, whatever the type. */
export function downloadUrl(ws: string, id: string): string {
  return `${contentUrl(ws, id)}?download=true`;
}

/** A JPEG of at most 320 px; only PNG, JPEG and GIF files have one (FileMeta.has_thumbnail). */
export function thumbnailUrl(ws: string, id: string): string {
  return `${base(ws)}/${encodeURIComponent(id)}/thumbnail`;
}

export const fileKeys = {
  meta: (ws: string, id: string) => ["files", ws, "meta", id] as const,
};

// ---- batched metadata lookup ----------------------------------------------------------------
// A datasheet page, a form or a report asks for the metadata of many files at once. Every ask goes
// through one batcher per workspace that waits a moment, then sends GET /files?ids=… for all of
// them (at most 100 ids each), instead of one request per file.

const BATCH_DELAY_MS = 12;
const BATCH_SIZE = 100;

interface Waiter {
  resolve: (m: FileMeta | null) => void;
  reject: (e: unknown) => void;
}

class Batcher {
  private waiting = new Map<string, Waiter[]>();
  private timer: ReturnType<typeof setTimeout> | undefined;
  constructor(private readonly ws: string) {}

  load(id: string): Promise<FileMeta | null> {
    return new Promise((resolve, reject) => {
      const list = this.waiting.get(id);
      if (list) list.push({ resolve, reject });
      else this.waiting.set(id, [{ resolve, reject }]);
      this.timer ??= setTimeout(() => this.flush(), BATCH_DELAY_MS);
    });
  }

  private flush(): void {
    this.timer = undefined;
    const batch = this.waiting;
    this.waiting = new Map();
    const all = [...batch.keys()];
    for (let i = 0; i < all.length; i += BATCH_SIZE) {
      const ids = all.slice(i, i + BATCH_SIZE);
      void this.send(ids, batch);
    }
  }

  private async send(ids: string[], batch: Map<string, Waiter[]>): Promise<void> {
    try {
      const res = await api.requestData(
        (c, init) => c.GET("/api/v1/w/{ws}/files", { ...init, params: { path: { ws: this.ws }, query: { ids: ids.join(",") } } }),
        { class: "background", label: "Loading file details" },
      );
      const found = new Map((res.items ?? []).map((m) => [m.id, m] as const));
      for (const id of ids) for (const w of batch.get(id) ?? []) w.resolve(found.get(id) ?? null);
    } catch (e) {
      for (const id of ids) for (const w of batch.get(id) ?? []) w.reject(e);
    }
  }
}

const batchers = new Map<string, Batcher>();

/** The metadata of one file through the shared batcher; null when the file does not exist (purged). */
export function loadFileMeta(ws: string, id: string): Promise<FileMeta | null> {
  let b = batchers.get(ws);
  if (!b) {
    b = new Batcher(ws);
    batchers.set(ws, b);
  }
  return b.load(id);
}

export interface FileInfo {
  id: string;
  /** Undefined while loading (or when the lookup failed); null when the file no longer exists. */
  meta: FileMeta | null | undefined;
  loading: boolean;
  failed: boolean;
}

/**
 * Metadata for a list of file ids, cached by TanStack Query (a file's metadata never changes
 * for its id) and fetched in batches. Ids are kept in the order given.
 */
export function useFileInfos(ws: string, ids: readonly string[]): FileInfo[] {
  const results = useQueries({
    queries: ids.map((id) => ({
      queryKey: fileKeys.meta(ws, id),
      queryFn: () => loadFileMeta(ws, id),
      staleTime: Infinity,
      gcTime: 10 * 60_000,
    })),
  });
  return ids.map((id, i) => {
    const r = results[i];
    return { id, meta: r?.data, loading: r === undefined || r.isPending, failed: r?.isError === true };
  });
}

/** Puts freshly uploaded metadata in the cache so the new file shows without another request. */
export function seedFileMeta(qc: QueryClient, ws: string, meta: FileMeta): void {
  qc.setQueryData(fileKeys.meta(ws, meta.id), meta);
}

// ---- upload ----------------------------------------------------------------------------------

export interface UploadHandle {
  promise: Promise<FileMeta>;
  abort: () => void;
}

function canceled(): ApiError {
  return new ApiError({ status: 0, code: "INTERNAL", title: "Upload canceled", message: "The upload was canceled.", canceled: true });
}

/**
 * Uploads one file with an XHR (fetch cannot report upload progress): a raw body with the
 * X-File-Name header, as the API guide describes. `onProgress` gets bytes sent and the total.
 * Rejects with an ApiError (a server refusal keeps its code: QUOTA_EXCEEDED, BAD_REQUEST, FORBIDDEN …).
 */
export function uploadFile(ws: string, file: File, onProgress: (loaded: number, total: number) => void): UploadHandle {
  const xhr = new XMLHttpRequest();
  const promise = new Promise<FileMeta>((resolve, reject) => {
    xhr.open("POST", base(ws));
    xhr.withCredentials = true;
    xhr.setRequestHeader("X-File-Name", encodeURIComponent(file.name));
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    xhr.setRequestHeader("Accept", "application/json");
    const csrf = useSessionStore.getState().csrf;
    if (csrf) xhr.setRequestHeader("X-CSRF-Token", csrf);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded, e.total);
    };
    xhr.onload = () => {
      let body: unknown;
      try {
        body = xhr.responseText ? JSON.parse(xhr.responseText) : undefined;
      } catch {
        body = undefined;
      }
      if (xhr.status >= 200 && xhr.status < 300 && body && typeof body === "object") {
        onProgress(file.size, file.size);
        resolve(body as FileMeta);
        return;
      }
      const status = xhr.status >= 200 && xhr.status <= 599 ? xhr.status : 500;
      reject(errorFromResponse(new Response(null, { status }), body));
    };
    xhr.onerror = () =>
      reject(new ApiError({ status: 0, code: "INTERNAL", title: "Upload failed", message: cutOffMessage(file.size), recovery: "Try again." }));
    xhr.onabort = () => reject(canceled());
    xhr.send(file);
  });
  return { promise, abort: () => xhr.abort() };
}

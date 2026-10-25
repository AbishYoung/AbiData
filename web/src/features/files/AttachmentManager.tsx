import { useEffect, useId, useRef, useState, type ChangeEvent, type DragEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, Camera, Download, FolderOpen, Trash2, TriangleAlert, UploadCloud, X } from "lucide-react";
import { Tip } from "@/components/Tip";
import { btn, btnPrimary, iconBtnSm } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { useIsCompact } from "@/lib/useMediaQuery";
import { downloadUrl, seedFileMeta, uploadFile, useFileInfos, type FileInfo, type FileMeta, type UploadHandle } from "./filesApi";
import { KindGlyph, RemovedGlyph, Thumb, showsPicture } from "./FileParts";
import { ImageViewer } from "./ImageViewer";
import { addIds, countLabel, formatBytes, isInlineImage, moveId, moveIdTo, planFiles, removeId, slotsLeft } from "./filesLogic";

interface Upload {
  key: number;
  name: string;
  size: number;
  loaded: number;
  error?: string;
}

const DRAG_TYPE = "application/x-abidata-file";

function hasFiles(dt: DataTransfer | null): boolean {
  return dt !== null && Array.from(dt.types).includes("Files");
}

/** What the user is told for a failed upload. */
function uploadError(e: unknown): string {
  if (e instanceof ApiError) return e.recovery && !e.message.includes(e.recovery) ? `${e.message} ${e.recovery}` : e.message;
  return "The upload failed. Try again.";
}

export interface AttachmentManagerProps {
  ws: string;
  /** The ordered file ids of the cell. */
  value: readonly string[];
  onChange: (ids: string[]) => void;
  /** The field's `max_files`. */
  maxFiles: number;
  readOnly?: boolean;
  /** The field's name, used in the control's accessible name. */
  label: string;
  className?: string;
  /** Called with true while an upload is running (a caller can hold back Save until it is done). */
  onBusyChange?: (busy: boolean) => void;
}

/**
 * The attachments editor used everywhere (datasheet dialog, phone record sheet, forms): add files by
 * drag and drop, Browse or (on phones) Take photo, with per-file progress; a thumbnail grid with
 * remove, reorder (buttons and drag), download and a full-screen image viewer. It is controlled:
 * uploads add their ids through `onChange`; saving the record is up to the caller.
 */
export function AttachmentManager({ ws, value, onChange, maxFiles, readOnly = false, label, className = "", onBusyChange }: AttachmentManagerProps) {
  const qc = useQueryClient();
  const compact = useIsCompact();
  const uid = useId();
  const browseRef = useRef<HTMLInputElement>(null);
  const cameraRef = useRef<HTMLInputElement>(null);
  // The latest props for callbacks that run after awaiting an upload (kept in step after each render).
  const valueRef = useRef(value);
  const onChangeRef = useRef(onChange);
  const busyRef = useRef(onBusyChange);
  useEffect(() => {
    valueRef.current = value;
    onChangeRef.current = onChange;
    busyRef.current = onBusyChange;
  });
  const handles = useRef(new Map<number, UploadHandle>());
  const nextKey = useRef(1);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [notice, setNotice] = useState("");
  const [announce, setAnnounce] = useState("");
  const [over, setOver] = useState(false);
  const [dragId, setDragId] = useState<string | null>(null);
  const [viewerId, setViewerId] = useState<string | null>(null);
  const infos = useFileInfos(ws, value);

  const running = uploads.filter((u) => u.error === undefined).length;
  const free = slotsLeft(maxFiles, value.length, running);
  const full = free === 0;

  const busy = running > 0;
  useEffect(() => {
    busyRef.current?.(busy);
  }, [busy]);
  useEffect(() => () => busyRef.current?.(false), []);

  // Leaving (closing the dialog, switching records) stops uploads that are still running.
  useEffect(() => {
    const live = handles.current;
    return () => {
      for (const h of live.values()) h.abort();
      live.clear();
    };
  }, []);

  function patch(key: number, change: Partial<Upload>): void {
    setUploads((list) => list.map((u) => (u.key === key ? { ...u, ...change } : u)));
  }

  function drop(key: number): void {
    handles.current.delete(key);
    setUploads((list) => list.filter((u) => u.key !== key));
  }

  /** Files chosen together join the value in the order they were chosen, whichever upload finishes first. */
  interface Batch {
    /** undefined: still uploading; null: failed or canceled; otherwise the new file. */
    slots: (FileMeta | null | undefined)[];
    flushed: number;
  }

  function flush(batch: Batch): void {
    const added: string[] = [];
    while (batch.flushed < batch.slots.length) {
      const slot = batch.slots[batch.flushed];
      if (slot === undefined) break;
      batch.flushed++;
      if (slot === null) continue;
      const next = addIds(valueRef.current, [slot.id], maxFiles);
      if (next.dropped > 0) {
        setNotice(`${slot.name} was not added: this field holds at most ${countLabel(maxFiles)}.`);
        continue;
      }
      valueRef.current = next.ids;
      onChangeRef.current(next.ids);
      added.push(slot.name);
    }
    if (added.length > 0) setAnnounce(added.length === 1 ? `Added ${added[0]}` : `Added ${added.length} files`);
  }

  function runUpload(file: File, batch: Batch, slot: number): void {
    const key = nextKey.current++;
    setUploads((list) => [...list, { key, name: file.name, size: file.size, loaded: 0 }]);
    const handle = uploadFile(ws, file, (loaded) => patch(key, { loaded }));
    handles.current.set(key, handle);
    handle.promise.then(
      (meta) => {
        seedFileMeta(qc, ws, meta);
        drop(key);
        batch.slots[slot] = meta;
        flush(batch);
      },
      (e: unknown) => {
        batch.slots[slot] = null;
        if (e instanceof ApiError && e.canceled) drop(key);
        else {
          handles.current.delete(key);
          patch(key, { error: uploadError(e) });
        }
        flush(batch);
      },
    );
  }

  function addFiles(files: File[]): void {
    if (readOnly || files.length === 0) return;
    const { accept, rejected } = planFiles(files, slotsLeft(maxFiles, valueRef.current.length, handles.current.size));
    setNotice(
      rejected.length > 0
        ? `${accept.length === 0 ? "No" : `Only ${accept.length} of the ${files.length}`} files added: this field holds at most ${countLabel(maxFiles)}.`
        : "",
    );
    const batch: Batch = { slots: accept.map(() => undefined), flushed: 0 };
    accept.forEach((f, i) => runUpload(f, batch, i));
  }

  function onPick(e: ChangeEvent<HTMLInputElement>): void {
    addFiles(Array.from(e.target.files ?? []));
    e.target.value = "";
  }

  function onZoneDrag(e: DragEvent): void {
    if (readOnly || !hasFiles(e.dataTransfer)) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = full ? "none" : "copy";
    setOver(true);
  }

  function onZoneDrop(e: DragEvent): void {
    if (readOnly || !hasFiles(e.dataTransfer)) return;
    e.preventDefault();
    setOver(false);
    addFiles(Array.from(e.dataTransfer.files));
  }

  function change(next: string[], message: string): void {
    valueRef.current = next;
    onChangeRef.current(next);
    setAnnounce(message);
  }

  const nameOf = (i: FileInfo): string => i.meta?.name ?? "removed file";
  const images = infos.filter((i) => i.meta && isInlineImage(i.meta.content_type)).map((i) => ({ id: i.id, name: nameOf(i) }));
  const viewerIndex = viewerId === null ? -1 : images.findIndex((i) => i.id === viewerId);

  const files = (
    <ul role="list" aria-label={`${label} files`} className="m-0 grid list-none grid-cols-2 gap-2 p-0 sm:grid-cols-[repeat(auto-fill,minmax(8.5rem,1fr))]">
      {infos.map((info, n) => {
        const meta = info.meta;
        const name = nameOf(info);
        const isImage = meta ? isInlineImage(meta.content_type) : false;
        return (
          <li
            key={info.id}
            data-testid="attachment-tile"
            data-file-id={info.id}
            draggable={!readOnly}
            onDragStart={(e) => {
              setDragId(info.id);
              e.dataTransfer.setData(DRAG_TYPE, info.id);
              e.dataTransfer.effectAllowed = "move";
            }}
            onDragEnd={() => setDragId(null)}
            onDragOver={(e) => {
              if (dragId === null || dragId === info.id) return;
              e.preventDefault();
              e.dataTransfer.dropEffect = "move";
            }}
            onDrop={(e) => {
              const from = e.dataTransfer.getData(DRAG_TYPE);
              if (!from) return;
              e.preventDefault();
              e.stopPropagation();
              setDragId(null);
              if (from !== info.id) change(moveIdTo(valueRef.current, from, info.id), `Moved ${nameOf(infos.find((x) => x.id === from) ?? info)}`);
            }}
            className={`m-0 flex min-w-0 flex-col rounded-lg border border-border bg-surface shadow-xs ${dragId === info.id ? "opacity-50" : ""}`}
          >
            {info.loading && !info.failed ? (
              <div className="aspect-square w-full animate-shimmer rounded-t-lg bg-surface-3 motion-reduce:animate-none" role="status">
                <span className="sr-only">Loading file</span>
              </div>
            ) : meta === undefined ? (
              <div className="flex aspect-square w-full flex-col items-center justify-center gap-1 rounded-t-lg bg-surface-2 text-fg-muted">
                <Placeholder name="file" type="" />
                <span className="text-xs">Details unavailable</span>
              </div>
            ) : meta === null ? (
              <div className="flex aspect-square w-full flex-col items-center justify-center gap-1 rounded-t-lg bg-surface-2 text-fg-muted" data-testid="file-removed">
                <RemovedGlyph size={28} />
                <span className="text-xs">File removed</span>
              </div>
            ) : (
              isImage ? (
                <Tip label={`View ${name}`}>
                  <button type="button" aria-label={`View ${name}`} onClick={() => setViewerId(info.id)} className="block aspect-square w-full cursor-pointer overflow-hidden rounded-t-lg bg-surface-2">
                    {showsPicture(meta) ? <Thumb ws={ws} meta={meta} size={136} className="h-full w-full" /> : <Placeholder name={name} type={meta.content_type} />}
                  </button>
                </Tip>
              ) : (
                // The download button below does the same; this larger target is for pointers only.
                <a href={downloadUrl(ws, info.id)} download={name} tabIndex={-1} aria-hidden="true" className="block aspect-square w-full cursor-pointer overflow-hidden rounded-t-lg bg-surface-2">
                  <Placeholder name={name} type={meta.content_type} />
                </a>
              )
            )}
            <div className="min-w-0 px-2 pt-1.5">
              <p className="m-0 truncate text-xs font-medium text-fg">{meta === null ? "File removed" : name}</p>
              {meta && <p className="m-0 text-xs text-fg-muted">{formatBytes(meta.size)}</p>}
            </div>
            <div className="flex flex-wrap items-center justify-end gap-0.5 p-1">
              {!readOnly && (
                <>
                  <Tip label={`Move ${name} earlier`}>
                    <button
                      type="button"
                      className={iconBtnSm}
                      aria-label={`Move ${name} earlier`}
                      disabled={n === 0}
                      onClick={() => change(moveId(valueRef.current, info.id, -1), `Moved ${name} to position ${n}`)}
                    >
                      <ArrowLeft size={14} aria-hidden="true" />
                    </button>
                  </Tip>
                  <Tip label={`Move ${name} later`}>
                    <button
                      type="button"
                      className={iconBtnSm}
                      aria-label={`Move ${name} later`}
                      disabled={n === infos.length - 1}
                      onClick={() => change(moveId(valueRef.current, info.id, 1), `Moved ${name} to position ${n + 2}`)}
                    >
                      <ArrowRight size={14} aria-hidden="true" />
                    </button>
                  </Tip>
                </>
              )}
              {meta && (
                <Tip label={`Download ${name}`}>
                  <a href={downloadUrl(ws, info.id)} download={name} aria-label={`Download ${name}`} className={iconBtnSm}>
                    <Download size={14} aria-hidden="true" />
                  </a>
                </Tip>
              )}
              {!readOnly && (
                <Tip label={`Remove ${name}`}>
                  <button type="button" className={iconBtnSm} aria-label={`Remove ${name}`} onClick={() => change(removeId(valueRef.current, info.id), `Removed ${name}`)}>
                    <Trash2 size={14} aria-hidden="true" />
                  </button>
                </Tip>
              )}
            </div>
          </li>
        );
      })}
    </ul>
  );

  return (
    <section aria-label={`${label} attachments`} className={`flex min-w-0 flex-col gap-3 ${className}`} data-testid="attachment-manager">
      <div className="flex items-center justify-between gap-2 text-xs text-fg-muted">
        <span data-testid="attachment-count">
          {value.length} of {maxFiles} {maxFiles === 1 ? "file" : "files"}
        </span>
        {readOnly && <span>Read-only</span>}
      </div>

      {!readOnly && (
        <div
          role="group"
          aria-label={`Add files to ${label}`}
          onDragEnter={onZoneDrag}
          onDragOver={onZoneDrag}
          onDragLeave={() => setOver(false)}
          onDrop={onZoneDrop}
          data-testid="attachment-dropzone"
          className={`flex flex-col items-stretch gap-2 rounded-lg border-2 border-dashed px-3 py-3 text-sm transition-colors ${
            over ? "border-accent bg-accent-soft" : "border-border-strong/60 bg-surface-2/40"
          }`}
        >
          {compact && (
            <button type="button" className={`${btnPrimary} !h-12 w-full text-base`} disabled={full} onClick={() => cameraRef.current?.click()}>
              <Camera size={20} aria-hidden="true" />
              Take photo
            </button>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <UploadCloud size={18} aria-hidden="true" className="shrink-0 text-fg-muted" />
            <span className="min-w-0 flex-1 text-fg-muted" id={`${uid}-hint`}>
              {full ? `Limit reached: ${countLabel(maxFiles)}.` : compact ? "Or choose files from your device." : "Drag files here, or"}
            </span>
            <button type="button" className={btn} disabled={full} aria-describedby={`${uid}-hint`} onClick={() => browseRef.current?.click()}>
              <FolderOpen size={16} aria-hidden="true" />
              Browse
            </button>
          </div>
          <input ref={browseRef} type="file" multiple hidden tabIndex={-1} aria-label={`Choose files for ${label}`} data-testid="attachment-input" onChange={onPick} />
          {compact && (
            <input ref={cameraRef} type="file" accept="image/*" capture="environment" hidden tabIndex={-1} aria-label={`Take a photo for ${label}`} data-testid="take-photo-input" onChange={onPick} />
          )}
        </div>
      )}

      {notice && (
        <p role="status" className="m-0 flex items-start gap-1.5 rounded-md border border-warning/40 bg-warning-soft px-2 py-1.5 text-xs text-fg">
          <TriangleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0 text-warning" />
          {notice}
        </p>
      )}

      {uploads.length > 0 && (
        <ul role="list" aria-label="Uploads" className="m-0 flex list-none flex-col gap-1.5 p-0">
          {uploads.map((u) => (
            <li key={u.key} className="m-0 rounded-md border border-border bg-surface px-2 py-1.5 text-xs" data-testid="upload-row">
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate font-medium text-fg">{u.name}</span>
                {u.error === undefined ? (
                  <>
                    <span className="tabular-nums text-fg-muted">{u.size > 0 ? Math.min(100, Math.round((u.loaded / u.size) * 100)) : 0}%</span>
                    <Tip label={`Cancel upload of ${u.name}`}>
                      <button type="button" className={iconBtnSm} aria-label={`Cancel upload of ${u.name}`} onClick={() => handles.current.get(u.key)?.abort()}>
                        <X size={14} aria-hidden="true" />
                      </button>
                    </Tip>
                  </>
                ) : (
                  <Tip label={`Dismiss the error for ${u.name}`}>
                    <button type="button" className={iconBtnSm} aria-label={`Dismiss the error for ${u.name}`} onClick={() => drop(u.key)}>
                      <X size={14} aria-hidden="true" />
                    </button>
                  </Tip>
                )}
              </div>
              {u.error === undefined ? (
                <div
                  role="progressbar"
                  aria-label={`Uploading ${u.name}`}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={u.size > 0 ? Math.min(100, Math.round((u.loaded / u.size) * 100)) : 0}
                  className="mt-1 h-1.5 overflow-hidden rounded-full bg-surface-3"
                >
                  <div className="h-full bg-accent transition-[width]" style={{ width: `${u.size > 0 ? Math.min(100, (u.loaded / u.size) * 100) : 0}%` }} />
                </div>
              ) : (
                <p role="alert" className="m-0 mt-1 flex items-start gap-1 text-danger">
                  <TriangleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0" />
                  <span>
                    {u.name}: {u.error}
                  </span>
                </p>
              )}
            </li>
          ))}
        </ul>
      )}

      {value.length === 0 && uploads.length === 0 ? (
        <p className="m-0 py-2 text-center text-sm text-fg-muted" data-testid="attachment-empty">
          {readOnly ? "No files attached." : "No files yet."}
        </p>
      ) : (
        value.length > 0 && files
      )}

      <span className="sr-only" role="status" aria-live="polite">
        {announce}
      </span>

      {viewerId !== null && viewerIndex >= 0 && (
        <ImageViewer ws={ws} images={images} index={viewerIndex} onIndex={(i) => setViewerId(images[i]?.id ?? null)} onClose={() => setViewerId(null)} />
      )}
    </section>
  );
}

function Placeholder({ name, type }: { name: string; type: string }) {
  return (
    <span className="flex h-full w-full items-center justify-center text-fg-muted">
      <KindGlyph contentType={type} name={name} size={36} />
    </span>
  );
}

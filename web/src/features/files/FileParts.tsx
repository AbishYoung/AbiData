import { createContext, useContext, useState, type ReactNode } from "react";
import {
  File as FileIcon,
  FileArchive,
  FileAudio,
  FileImage,
  FileSpreadsheet,
  FileText,
  FileVideo,
  FileX,
} from "lucide-react";
import { badge } from "@/components/styles";
import { contentUrl, thumbnailUrl, useFileInfos, type FileInfo, type FileMeta } from "./filesApi";
import { countLabel, fileKind, isInlineImage, parseIds, type FileKind } from "./filesLogic";

/** The workspace whose files the surrounding view shows (datasheet, reports); forms pass it explicitly. */
const FilesWorkspaceContext = createContext<string | null>(null);

export function FilesWorkspaceProvider({ ws, children }: { ws: string; children: ReactNode }) {
  return <FilesWorkspaceContext.Provider value={ws}>{children}</FilesWorkspaceContext.Provider>;
}

/** The workspace id for file URLs. Only call inside a FilesWorkspaceProvider. */
export function useFilesWorkspace(): string {
  const ws = useContext(FilesWorkspaceContext);
  if (ws === null) throw new Error("useFilesWorkspace needs a FilesWorkspaceProvider");
  return ws;
}

const KIND_ICON: Record<FileKind, typeof FileIcon> = {
  image: FileImage,
  pdf: FileText,
  text: FileText,
  sheet: FileSpreadsheet,
  archive: FileArchive,
  audio: FileAudio,
  video: FileVideo,
  file: FileIcon,
};

/** The icon of a file type; decorative (the file's name is always shown next to it). */
export function KindGlyph({ contentType, name, size = 20, className = "" }: { contentType: string; name: string; size?: number; className?: string }) {
  const Icon = KIND_ICON[fileKind(contentType, name)];
  return <Icon size={size} aria-hidden="true" className={className} />;
}

export function RemovedGlyph({ size = 20, className = "" }: { size?: number; className?: string }) {
  return <FileX size={size} aria-hidden="true" className={className} />;
}

/** True when the file can be shown as a picture (thumbnail or the file itself). */
export function showsPicture(meta: FileMeta | null | undefined): boolean {
  return meta !== null && meta !== undefined && (meta.has_thumbnail || isInlineImage(meta.content_type));
}

/** A square picture of an image file (its thumbnail when it has one), or the file's icon. */
export function Thumb({ ws, meta, size, className = "" }: { ws: string; meta: FileMeta; size: number; className?: string }) {
  const [failed, setFailed] = useState(false);
  if (failed || !showsPicture(meta)) {
    return (
      <span className={`inline-flex items-center justify-center bg-surface-3 text-fg-muted ${className}`} style={{ width: size, height: size }}>
        <KindGlyph contentType={meta.content_type} name={meta.name} size={Math.round(size * 0.6)} />
      </span>
    );
  }
  return (
    <img
      src={meta.has_thumbnail ? thumbnailUrl(ws, meta.id) : contentUrl(ws, meta.id)}
      alt=""
      loading="lazy"
      draggable={false}
      width={size}
      height={size}
      onError={() => setFailed(true)}
      className={`object-cover ${className}`}
      style={{ width: size, height: size }}
    />
  );
}

/** The cell display of an attachments value: how many files, and small thumbnails of the first few. */
export function AttachmentThumbs({ ws, ids, max = 3, size = 22 }: { ws: string; ids: readonly string[]; max?: number; size?: number }) {
  const infos: FileInfo[] = useFileInfos(ws, ids.slice(0, max));
  if (ids.length === 0) return null;
  return (
    <span className="flex min-w-0 items-center gap-1.5" data-testid="attachment-cell">
      <span className={badge} aria-hidden="true">
        {ids.length}
      </span>
      <span className="sr-only">{countLabel(ids.length)}</span>
      <span className="flex items-center gap-1" aria-hidden="true">
        {infos.map((i) =>
          i.meta ? (
            <Thumb key={i.id} ws={ws} meta={i.meta} size={size} className="rounded-sm" />
          ) : (
            <span key={i.id} className="inline-block animate-pulse rounded-sm bg-surface-3 motion-reduce:animate-none" style={{ width: size, height: size }} />
          ),
        )}
        {ids.length > max && <span className="text-xs text-fg-muted">+{ids.length - max}</span>}
      </span>
    </span>
  );
}

/** An attachments cell value (datasheet grid, cards) as count and thumbnails. */
export function AttachmentCell({ value, max = 3 }: { value: unknown; max?: number }) {
  return <AttachmentThumbs ws={useFilesWorkspace()} ids={parseIds(value)} max={max} />;
}

/** File names for places that list files as text (history): "photo.jpg, notes.pdf", with "file removed" for purged ones. */
export function FileNames({ ws, ids }: { ws: string; ids: readonly string[] }) {
  const infos = useFileInfos(ws, ids);
  if (ids.length === 0) return <span>(empty)</span>;
  return (
    <span data-testid="file-names">
      {infos.map((i, n) => (
        <span key={i.id}>
          {n > 0 && ", "}
          {i.meta ? <span>{i.meta.name}</span> : i.meta === null ? <em>file removed</em> : <span className="text-fg-muted">…</span>}
        </span>
      ))}
    </span>
  );
}

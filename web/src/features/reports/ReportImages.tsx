import { contentUrl, useFileInfos } from "@/features/files/filesApi";
import { useFilesWorkspace } from "@/features/files/FileParts";
import { isInlineImage, parseIds } from "@/features/files/filesLogic";

/** Columns of an image gallery for `n` pictures: 1, then 2 across up to 4, 3 up to 9, else 4. */
export function galleryColumns(n: number): number {
  if (n <= 1) return 1;
  if (n <= 4) return 2;
  return n <= 9 ? 3 : 4;
}

/**
 * The image control of a report: the first image of an attachments field, or a gallery of up to
 * `max` images, as plain `<img>` elements on the content URL so they print with the page.
 * Files that are not pictures are skipped.
 */
export function ReportImages({ value, max, label }: { value: unknown; max: number; label: string }) {
  const ws = useFilesWorkspace();
  const ids = parseIds(value);
  const infos = useFileInfos(ws, ids);
  const pictures = infos.filter((i) => i.meta && isInlineImage(i.meta.content_type)).slice(0, Math.max(1, max));
  if (pictures.length === 0) return null;
  const cols = galleryColumns(pictures.length);
  return (
    <div
      data-testid="report-image"
      role="group"
      aria-label={label}
      style={{ display: "grid", gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`, gap: 4, width: "100%", height: "100%" }}
    >
      {pictures.map((p) => (
        <img
          key={p.id}
          src={contentUrl(ws, p.id)}
          alt={p.meta?.name ?? ""}
          loading="eager"
          decoding="sync"
          style={{ width: "100%", height: "100%", minHeight: 0, objectFit: "contain", objectPosition: "left top" }}
        />
      ))}
    </div>
  );
}

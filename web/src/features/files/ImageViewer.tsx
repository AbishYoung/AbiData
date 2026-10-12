import { useEffect, useRef } from "react";
import { Dialog } from "radix-ui";
import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import { Tip } from "@/components/Tip";
import { contentUrl, downloadUrl } from "./filesApi";

export interface ViewerImage {
  id: string;
  name: string;
}

const round =
  "inline-flex h-10 w-10 items-center justify-center rounded-full bg-white/15 text-white transition-colors hover:bg-white/25 disabled:cursor-not-allowed disabled:opacity-40";

/** Full-screen image viewer with previous/next buttons and Left/Right arrow keys. Mount only while open. */
export function ImageViewer({
  ws,
  images,
  index,
  onIndex,
  onClose,
}: {
  ws: string;
  images: readonly ViewerImage[];
  index: number;
  onIndex: (i: number) => void;
  onClose: () => void;
}) {
  // The button that opened the viewer gets focus back on close (Radix has no trigger to return to).
  const opener = useRef<Element | null>(typeof document === "undefined" ? null : document.activeElement);
  const current = images[index];
  const last = images.length - 1;
  const go = (i: number): void => onIndex(Math.min(Math.max(i, 0), last));

  // Arrow keys work wherever focus is: it can be on <body> after Next disables itself at the last image.
  const step = useRef(go);
  useEffect(() => {
    step.current = go;
  });
  useEffect(() => {
    function onKey(e: KeyboardEvent): void {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
      e.preventDefault();
      step.current(index + (e.key === "ArrowLeft" ? -1 : 1));
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [index]);

  if (!current) return null;
  return (
    <Dialog.Root open onOpenChange={(o) => (o ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[65] bg-black/90" />
        <Dialog.Content
          aria-describedby={undefined}
          data-presentation="fullscreen"
          data-testid="image-viewer"
          onCloseAutoFocus={(e) => {
            if (opener.current instanceof HTMLElement && opener.current.isConnected) {
              e.preventDefault();
              opener.current.focus({ preventScroll: true });
            }
          }}
          className="fixed inset-0 z-[65] flex flex-col text-white outline-none"
        >
          <div className="flex shrink-0 items-center gap-2 px-3 pb-2 pt-[max(0.5rem,env(safe-area-inset-top))]">
            <Dialog.Title className="m-0 min-w-0 flex-1 truncate text-sm font-semibold">{current.name}</Dialog.Title>
            <span className="text-sm tabular-nums" aria-live="polite" data-testid="viewer-position">
              {index + 1} of {images.length}
            </span>
            <Tip label="Download" side="bottom">
              <a href={downloadUrl(ws, current.id)} download={current.name} aria-label={`Download ${current.name}`} className={round}>
                <Download size={18} aria-hidden="true" />
              </a>
            </Tip>
            <Tip label="Close viewer" side="bottom">
              <Dialog.Close className={round} aria-label="Close viewer">
                <X size={18} aria-hidden="true" />
              </Dialog.Close>
            </Tip>
          </div>
          <div className="relative flex min-h-0 flex-1 items-center justify-center px-14 pb-[max(1rem,env(safe-area-inset-bottom))]">
            <Tip label="Previous image" shortcut="Left" side="right">
              <button type="button" className={`${round} absolute left-2 top-1/2 -translate-y-1/2`} aria-label="Previous image" disabled={index <= 0} onClick={() => go(index - 1)}>
                <ChevronLeft size={22} aria-hidden="true" />
              </button>
            </Tip>
            <img
              key={current.id}
              src={contentUrl(ws, current.id)}
              alt={current.name}
              data-testid="viewer-image"
              className="max-h-full max-w-full rounded-sm object-contain"
            />
            <Tip label="Next image" shortcut="Right" side="left">
              <button type="button" className={`${round} absolute right-2 top-1/2 -translate-y-1/2`} aria-label="Next image" disabled={index >= last} onClick={() => go(index + 1)}>
                <ChevronRight size={22} aria-hidden="true" />
              </button>
            </Tip>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

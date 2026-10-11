import { useState } from "react";
import { Dialog } from "radix-ui";
import { BookOpen, Check, Copy, TriangleAlert } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/lib/api";
import { useErrorStore } from "@/lib/errorStore";
import { errorDocsTarget } from "@/features/docs/docsLinks";
import { openDocs } from "@/features/docs/docsStore";
import {
  btn,
  btnGhostSm,
  btnPrimary,
  dialogFooter,
  dialogOverlay,
  dialogPanel,
  dialogTitle,
  iconChip,
} from "./styles";

interface ErrorDialogProps {
  error: ApiError;
  onClose: () => void;
  onRetry?: () => void;
}

export function ErrorDialog({ error, onClose, onRetry }: ErrorDialogProps) {
  const [copied, setCopied] = useState(false);

  async function copyId() {
    try {
      await navigator.clipboard.writeText(error.requestId);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className={dialogOverlay} />
        <Dialog.Content
          className={`${dialogPanel} w-[min(32rem,calc(100vw-2rem))] p-5`}
        >
          <div className="flex items-start gap-3">
            <span className={`${iconChip} bg-danger-soft text-danger`}>
              <TriangleAlert size={18} aria-hidden="true" />
            </span>
            <div className="min-w-0 flex-1">
              <Dialog.Title className={`${dialogTitle} pt-1`}>
                {error.title}
              </Dialog.Title>
              <Dialog.Description className="mt-1.5 text-sm text-fg">
                {error.message}
              </Dialog.Description>
              {error.recovery && (
                <p className="mt-2 text-sm text-fg-muted">{error.recovery}</p>
              )}
            </div>
          </div>
          <dl className="mt-4 space-y-2 rounded-lg border border-border bg-surface-2/60 px-3 py-2.5 text-xs text-fg-muted">
            <div className="flex items-center gap-2">
              <dt className="w-20 shrink-0">Error code</dt>
              <dd className="m-0 break-all font-mono text-fg">{error.code}</dd>
            </div>
            {error.requestId && (
              <div className="flex flex-wrap items-center gap-2">
                <dt className="w-20 shrink-0">Request ID</dt>
                <dd className="m-0 min-w-0 break-all font-mono text-fg">
                  {error.requestId}
                </dd>
                <button
                  type="button"
                  className={`${btnGhostSm} ml-auto`}
                  onClick={() => void copyId()}
                >
                  {copied ? (
                    <Check size={14} aria-hidden="true" />
                  ) : (
                    <Copy size={14} aria-hidden="true" />
                  )}
                  {copied ? "Copied" : "Copy request ID"}
                </button>
              </div>
            )}
          </dl>
          <p className="mt-3 text-sm">
            <button
              type="button"
              className="inline-flex items-center gap-1 font-medium text-accent hover:underline"
              onClick={() => openDocs(errorDocsTarget(error.code))}
            >
              <BookOpen size={14} aria-hidden="true" />
              Learn more<span className="sr-only"> about {error.code}</span>
            </button>
          </p>
          <div className={dialogFooter}>
            {onRetry && (
              <button type="button" className={btnPrimary} onClick={onRetry}>
                Retry safely
              </button>
            )}
            <Dialog.Close className={btn}>Close</Dialog.Close>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** Renders the globally stored error, if any. Mount once near the app root. */
export function ErrorHost() {
  const error = useErrorStore((s) => s.error);
  const dismiss = useErrorStore((s) => s.dismiss);
  const show = useErrorStore((s) => s.show);
  const queryClient = useQueryClient();

  if (!error) return null;
  const retry = error.retry;

  function handleRetry() {
    dismiss();
    if (!retry) return;
    retry().then(
      () => void queryClient.invalidateQueries(),
      (e: unknown) => {
        if (e instanceof ApiError && !e.canceled) show(e);
      },
    );
  }

  return (
    <ErrorDialog
      error={error}
      onClose={dismiss}
      onRetry={retry ? handleRetry : undefined}
    />
  );
}

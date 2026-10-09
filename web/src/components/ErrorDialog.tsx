import { useState } from "react";
import { Dialog } from "radix-ui";
import { Check, Copy, TriangleAlert } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/lib/api";
import { useErrorStore } from "@/lib/errorStore";
import { btn, btnPrimary } from "./styles";

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
        <Dialog.Overlay className="fixed inset-0 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 w-[min(32rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
          <Dialog.Title className="flex items-center gap-2 text-base font-semibold">
            <TriangleAlert size={18} aria-hidden="true" className="text-danger" />
            {error.title}
          </Dialog.Title>
          <Dialog.Description className="mt-2 text-sm">{error.message}</Dialog.Description>
          {error.recovery && <p className="mt-2 text-sm text-fg-muted">{error.recovery}</p>}
          <dl className="mt-3 space-y-1 text-xs text-fg-muted">
            <div className="flex items-center gap-2">
              <dt>Error code</dt>
              <dd className="font-mono">{error.code}</dd>
            </div>
            {error.requestId && (
              <div className="flex items-center gap-2">
                <dt>Request ID</dt>
                <dd className="font-mono">{error.requestId}</dd>
                <button type="button" className={`${btn} !px-2 !py-0.5 text-xs`} onClick={() => void copyId()}>
                  {copied ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
                  {copied ? "Copied" : "Copy request ID"}
                </button>
              </div>
            )}
          </dl>
          <p className="mt-3 text-sm">
            <a
              className="text-accent underline"
              href={`/docs/troubleshooting/errors#${error.code.toLowerCase()}`}
              target="_blank"
              rel="noreferrer"
            >
              Troubleshooting for {error.code} (opens in a new tab)
            </a>
          </p>
          <div className="mt-4 flex justify-end gap-2">
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

  return <ErrorDialog error={error} onClose={dismiss} onRetry={retry ? handleRetry : undefined} />;
}

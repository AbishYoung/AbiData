import { create } from "zustand";
import { ConfirmDialog } from "./Dialog";

// An awaitable confirmation in place of window.confirm (ADR 0015): `if (!(await askToConfirm({…}))) return;`.
// One request is shown at a time by the ConfirmHost mounted near the app root.

export interface ConfirmRequest {
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
}

interface ConfirmState {
  request: (ConfirmRequest & { resolve: (ok: boolean) => void }) | null;
  ask: (r: ConfirmRequest) => Promise<boolean>;
  answer: (ok: boolean) => void;
}

export const useConfirmStore = create<ConfirmState>()((set, get) => ({
  request: null,
  ask: (r) =>
    new Promise<boolean>((resolve) => {
      // A newer question replaces an unanswered one; the older caller gets "no".
      get().request?.resolve(false);
      set({ request: { ...r, resolve } });
    }),
  answer: (ok) => {
    const r = get().request;
    set({ request: null });
    r?.resolve(ok);
  },
}));

/** Asks the user to confirm; resolves to true only when they choose the confirm button. */
export function askToConfirm(r: ConfirmRequest): Promise<boolean> {
  return useConfirmStore.getState().ask(r);
}

/** Shows the pending confirmation, if any. Mount once near the app root. */
export function ConfirmHost() {
  const request = useConfirmStore((s) => s.request);
  const answer = useConfirmStore((s) => s.answer);
  if (!request) return null;
  return (
    <ConfirmDialog
      title={request.title}
      message={request.message}
      confirmLabel={request.confirmLabel}
      cancelLabel={request.cancelLabel}
      danger={request.danger}
      onConfirm={() => answer(true)}
      onCancel={() => answer(false)}
    />
  );
}

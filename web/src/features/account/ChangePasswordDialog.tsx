import { useId, useRef, useState, type FormEvent } from "react";
import { Dialog } from "radix-ui";
import { useMutation } from "@tanstack/react-query";
import { KeyRound, X } from "lucide-react";
import { create } from "zustand";
import { ApiError, api } from "@/lib/api";
import { Tip } from "@/components/Tip";
import { toast } from "@/components/AppToasts";
import {
  alertBox,
  btn,
  btnPrimary,
  dialogDescription,
  dialogFooter,
  dialogOverlay,
  dialogPanel,
  dialogTitle,
  fieldError,
  hint as hintClass,
  iconBtn,
  iconChip,
  input,
  label as labelClass,
} from "@/components/styles";

export const MIN_PASSWORD = 10;
export const MAX_PASSWORD = 1024;

interface AccountDialogState {
  changePassword: boolean;
  setChangePassword: (open: boolean) => void;
}

/** Whether the change-password dialog is open; the menus and the command palette open it from anywhere. */
export const useAccountDialogs = create<AccountDialogState>()((set) => ({
  changePassword: false,
  setChangePassword: (open) => set({ changePassword: open }),
}));

export function openChangePassword(): void {
  useAccountDialogs.getState().setChangePassword(true);
}

export interface PasswordErrors {
  current?: string;
  next?: string;
  confirm?: string;
}

/** The same rules the server applies (length counts bytes, as it does), plus the confirmation match. */
export function validatePasswordChange(current: string, next: string, confirm: string): PasswordErrors {
  const errors: PasswordErrors = {};
  if (current === "") errors.current = "Enter your current password.";
  const bytes = new TextEncoder().encode(next).length;
  if (next === "") errors.next = "Enter a new password.";
  else if (bytes < MIN_PASSWORD) errors.next = `Use at least ${MIN_PASSWORD} characters.`;
  else if (bytes > MAX_PASSWORD) errors.next = "Use at most 1,024 characters.";
  else if (next === current) errors.next = "Choose a password different from the current one.";
  if (confirm !== next) errors.confirm = "The two passwords do not match.";
  return errors;
}

export const PASSWORD_CHANGED = "Password changed. Your other sessions on other devices and browsers were signed out.";

function serverMessage(e: unknown): string {
  if (e instanceof ApiError) return e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return "Something went wrong. Try again.";
}

function PasswordField({
  id,
  label,
  value,
  onChange,
  autoComplete,
  error,
  hint,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  autoComplete: string;
  error: string | undefined;
  hint?: string;
}) {
  const [shown, setShown] = useState(false);
  const describedBy = [hint ? `${id}-hint` : null, error ? `${id}-err` : null].filter(Boolean).join(" ");
  return (
    <div>
      <label htmlFor={id} className={labelClass}>
        {label}
      </label>
      <div className="relative">
        <input
          id={id}
          type={shown ? "text" : "password"}
          autoComplete={autoComplete}
          className={`${input} pr-16`}
          value={value}
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy || undefined}
          onChange={(e) => onChange(e.target.value)}
        />
        <Tip label={shown ? `Hide ${label.toLowerCase()}` : `Show ${label.toLowerCase()}`}>
          <button
            type="button"
            aria-controls={id}
            onClick={() => setShown((v) => !v)}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded px-1.5 py-0.5 text-xs font-medium text-fg-muted hover:bg-surface-2 hover:text-fg"
          >
            {shown ? "Hide" : "Show"}
            <span className="sr-only"> {label.toLowerCase()}</span>
          </button>
        </Tip>
      </div>
      {hint && (
        <p id={`${id}-hint`} className={hintClass}>
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-err`} className={fieldError}>
          {error}
        </p>
      )}
    </div>
  );
}

function ChangePasswordForm({ onClose, onChanged }: { onClose: () => void; onChanged: () => void }) {
  const id = useId();
  const currentRef = useRef<HTMLDivElement>(null);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [errors, setErrors] = useState<PasswordErrors>({});
  const [topError, setTopError] = useState<string | null>(null);
  const change = useMutation({
    meta: { inline: true },
    mutationFn: () =>
      api.request(
        (c, init) => c.POST("/api/v1/auth/password", { ...init, body: { current_password: current, new_password: next } }),
        { label: "Changing password" },
      ),
    onSuccess: onChanged,
    onError: (e) => {
      setTopError(serverMessage(e));
      if (e instanceof ApiError && e.status === 400 && /current password/i.test(e.message)) {
        setErrors({ current: "The current password is incorrect." });
        currentRef.current?.querySelector("input")?.focus();
      }
    },
  });

  function submit(e: FormEvent): void {
    e.preventDefault();
    setTopError(null);
    const found = validatePasswordChange(current, next, confirm);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    change.mutate();
  }

  return (
    <form onSubmit={submit} noValidate className="mt-4 space-y-4">
      {topError && (
        <div role="alert" className={alertBox}>
          {topError}
        </div>
      )}
      <div ref={currentRef}>
        <PasswordField
          id={`${id}-current`}
          label="Current password"
          value={current}
          onChange={setCurrent}
          autoComplete="current-password"
          error={errors.current}
        />
      </div>
      <PasswordField
        id={`${id}-new`}
        label="New password"
        value={next}
        onChange={setNext}
        autoComplete="new-password"
        error={errors.next}
        hint={`At least ${MIN_PASSWORD} characters. A long passphrase is best.`}
      />
      <PasswordField
        id={`${id}-confirm`}
        label="Confirm new password"
        value={confirm}
        onChange={setConfirm}
        autoComplete="new-password"
        error={errors.confirm}
      />
      <div className={dialogFooter}>
        <button type="button" className={btn} onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className={btnPrimary} disabled={change.isPending}>
          {change.isPending ? "Changing…" : "Change password"}
        </button>
      </div>
    </form>
  );
}

/** Mount once near the app root: the change-password dialog and its success toast. */
export function ChangePasswordHost() {
  const open = useAccountDialogs((s) => s.changePassword);
  const setOpen = useAccountDialogs((s) => s.setChangePassword);
  const contentRef = useRef<HTMLDivElement>(null);
  return (
    <>
      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className={dialogOverlay} />
          <Dialog.Content
            ref={contentRef}
            className={`${dialogPanel} w-[min(26rem,calc(100vw-2rem))] p-5`}
            onOpenAutoFocus={(e) => {
              // Start in the first field rather than on the close button.
              e.preventDefault();
              contentRef.current?.querySelector("input")?.focus();
            }}
          >
            <div className="flex items-start gap-3">
              <span className={`${iconChip} bg-accent-soft text-accent`}>
                <KeyRound size={16} aria-hidden="true" />
              </span>
              <div className="min-w-0 flex-1">
                <Dialog.Title className={dialogTitle}>Change password</Dialog.Title>
                <Dialog.Description className={dialogDescription}>
                  You stay signed in here. Every other session of your account is signed out.
                </Dialog.Description>
              </div>
              <Tip label="Close">
                <Dialog.Close className={iconBtn} aria-label="Close">
                  <X size={16} aria-hidden="true" />
                </Dialog.Close>
              </Tip>
            </div>
            <ChangePasswordForm
              onClose={() => setOpen(false)}
              onChanged={() => {
                setOpen(false);
                toast("success", PASSWORD_CHANGED);
              }}
            />
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}

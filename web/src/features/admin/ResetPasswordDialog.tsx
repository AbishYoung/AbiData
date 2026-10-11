import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Dices, Eye, EyeOff } from "lucide-react";
import { ApiError } from "@/lib/api";
import { Tip } from "@/components/Tip";
import { toast } from "@/components/AppToasts";
import { Modal } from "@/features/queries/Modal";
import { alertBox, btn, btnPrimary, iconBtn, input } from "@/components/styles";
import { resetUserPassword, type AdminUser } from "./adminApi";
import { generatePassword } from "./passwordGen";
import { Field } from "./ui";

const MIN_PASSWORD = 10;
const MAX_PASSWORD = 1024;

/** Sets a new password for another user and signs them out everywhere. */
export function ResetPasswordDialog({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const [password, setPassword] = useState("");
  const [shown, setShown] = useState(false);
  const [copied, setCopied] = useState("");
  const [error, setError] = useState<string | undefined>();
  const [topError, setTopError] = useState<string | null>(null);

  const reset = useMutation({
    meta: { inline: true },
    mutationFn: () => resetUserPassword(user.id, password),
    onSuccess: () => {
      toast("success", `Password reset for ${user.email}. Their sessions were signed out.`);
      onClose();
    },
    onError: (e) => setTopError(e instanceof ApiError ? e.message : "The password could not be reset."),
  });

  const copy = () => {
    if (password === "") return;
    navigator.clipboard.writeText(password).then(
      () => setCopied("Copied to the clipboard."),
      () => setCopied("Copy failed. Select the password and copy it manually."),
    );
  };

  return (
    <Modal title="Reset password" description={`Set a new password for ${user.email}.`} onClose={onClose}>
      <form
        noValidate
        aria-label="Reset password"
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          setTopError(null);
          if (password.length < MIN_PASSWORD) return setError(`Use at least ${MIN_PASSWORD} characters.`);
          if (password.length > MAX_PASSWORD) return setError("Use at most 1,024 characters.");
          setError(undefined);
          reset.mutate();
        }}
      >
        {topError && (
          <div role="alert" className={alertBox}>
            {topError}
          </div>
        )}
        <Field
          id="reset-password"
          label="New password"
          hint={`At least ${MIN_PASSWORD} characters. Share it with the user over a secure channel; it is not shown again after you save.`}
          error={error}
        >
          {(aria) => (
            <div className="flex items-center gap-1.5">
              <input
                className={`${input} font-mono`}
                type={shown ? "text" : "password"}
                autoComplete="new-password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  setCopied("");
                }}
                {...aria}
              />
              <Tip label={shown ? "Hide password" : "Show password"}>
                <button type="button" className={iconBtn} aria-label={shown ? "Hide password" : "Show password"} aria-pressed={shown} onClick={() => setShown((v) => !v)}>
                  {shown ? <EyeOff size={16} aria-hidden="true" /> : <Eye size={16} aria-hidden="true" />}
                </button>
              </Tip>
              <Tip label="Copy password">
                <button type="button" className={iconBtn} aria-label="Copy password" disabled={password === ""} onClick={copy}>
                  {copied.startsWith("Copied") ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
                </button>
              </Tip>
            </div>
          )}
        </Field>
        <div>
          <button
            type="button"
            className={btn}
            onClick={() => {
              setPassword(generatePassword());
              setShown(true);
              setError(undefined);
              setCopied("");
            }}
          >
            <Dices size={14} aria-hidden="true" />
            Generate a strong password
          </button>
          <span role="status" aria-live="polite" className="ml-2 text-xs text-fg-muted">
            {copied}
          </span>
        </div>
        <p className="m-0 text-sm text-fg-muted">
          Saving signs {user.email} out of every device they are signed in on, and clears any sign-in lockout. They can then sign in with the new password.
        </p>
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={reset.isPending}>
            Reset password
          </button>
        </div>
      </form>
    </Modal>
  );
}

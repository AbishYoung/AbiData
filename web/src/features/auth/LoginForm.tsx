import { useState, type FormEvent, type KeyboardEvent } from "react";
import { TriangleAlert } from "lucide-react";
import { ApiError } from "@/lib/api";
import { Tip } from "@/components/Tip";
import { alertBox, btnPrimary, input, label } from "@/components/styles";

interface LoginFormProps {
  onSubmit: (email: string, password: string) => Promise<void>;
}

export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return "Something went wrong. Try again.";
}

export function LoginForm({ onSubmit }: LoginFormProps) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [capsLock, setCapsLock] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function trackCapsLock(e: KeyboardEvent<HTMLInputElement>) {
    setCapsLock(e.getModifierState("CapsLock"));
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await onSubmit(email, password);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4" noValidate>
      {error && (
        <div role="alert" className={alertBox}>
          {error}
        </div>
      )}
      <div>
        <label htmlFor="login-email" className={label}>
          Email
        </label>
        <input
          id="login-email"
          type="email"
          autoComplete="username"
          required
          className={input}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
      </div>
      <div>
        <label htmlFor="login-password" className={label}>
          Password
        </label>
        <div className="relative">
          <input
            id="login-password"
            type={showPassword ? "text" : "password"}
            autoComplete="current-password"
            required
            className={`${input} pr-16`}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={trackCapsLock}
            onKeyUp={trackCapsLock}
            onBlur={() => setCapsLock(false)}
          />
          <Tip label={showPassword ? "Hide password" : "Show password"}>
            <button
              type="button"
              aria-controls="login-password"
              onClick={() => setShowPassword((v) => !v)}
              className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded px-1.5 py-0.5 text-xs font-medium text-fg-muted hover:bg-surface-2 hover:text-fg"
            >
              {showPassword ? "Hide" : "Show"}
              <span className="sr-only"> password</span>
            </button>
          </Tip>
        </div>
        {capsLock && (
          <p role="status" className="mt-1 flex items-center gap-1 text-xs font-medium text-fg">
            <TriangleAlert size={12} aria-hidden="true" className="shrink-0 text-warning" />
            Caps Lock is on.
          </p>
        )}
      </div>
      <button type="submit" className={`${btnPrimary} w-full`} disabled={busy}>
        {busy ? "Signing in…" : "Sign in"}
      </button>
    </form>
  );
}

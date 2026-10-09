import { useState, type FormEvent } from "react";
import { ApiError } from "@/lib/api";
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
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

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
        <input
          id="login-password"
          type="password"
          autoComplete="current-password"
          required
          className={input}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>
      <button type="submit" className={`${btnPrimary} w-full`} disabled={busy}>
        {busy ? "Signing in…" : "Sign in"}
      </button>
    </form>
  );
}

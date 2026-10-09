import { useState, type FormEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { queryClient } from "@/app/queryClient";
import { adoptSession, completeSetup } from "@/lib/session";
import { alertBox, btnPrimary, input, label } from "@/components/styles";
import { AuthLayout } from "./AuthLayout";
import { errorText } from "./LoginForm";

export function SetupPage() {
  const navigate = useNavigate();
  const [token, setToken] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      setError("The two passwords do not match.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const me = await completeSetup(token, name, password);
      adoptSession(queryClient, me);
      await navigate({ to: "/" });
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthLayout title="First-run setup">
      <p className="mb-4 text-sm text-fg-muted">
        Create the first administrator. The setup token is printed in the server log on first start.
      </p>
      <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4" noValidate>
        {error && (
          <div role="alert" className={alertBox}>
            {error}
          </div>
        )}
        <div>
          <label htmlFor="setup-token" className={label}>
            Setup token
          </label>
          <input id="setup-token" required autoComplete="off" className={input} value={token} onChange={(e) => setToken(e.target.value)} />
        </div>
        <div>
          <label htmlFor="setup-name" className={label}>
            Display name
          </label>
          <input id="setup-name" required autoComplete="name" className={input} value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div>
          <label htmlFor="setup-password" className={label}>
            Password
          </label>
          <input id="setup-password" type="password" required autoComplete="new-password" className={input} value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        <div>
          <label htmlFor="setup-confirm" className={label}>
            Confirm password
          </label>
          <input id="setup-confirm" type="password" required autoComplete="new-password" className={input} value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </div>
        <button type="submit" className={`${btnPrimary} w-full`} disabled={busy}>
          {busy ? "Creating…" : "Create administrator"}
        </button>
      </form>
    </AuthLayout>
  );
}

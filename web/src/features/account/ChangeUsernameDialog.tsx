import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AtSign } from "lucide-react";
import { create } from "zustand";
import { ApiError, api } from "@/lib/api";
import { ME_KEY, useMe } from "@/lib/session";
import { toast } from "@/components/AppToasts";
import { alertBox, iconChip } from "@/components/styles";
import { Button, Dialog, Field, Input } from "@/components/ui";
import { normalizeUsername, usernameProblem } from "./username";

const useUsernameDialog = create<{ open: boolean; setOpen: (open: boolean) => void }>()((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
}));

export function openChangeUsername(): void {
  useUsernameDialog.getState().setOpen(true);
}

function ChangeUsernameForm({ current, onClose }: { current: string; onClose: () => void }) {
  const qc = useQueryClient();
  const [value, setValue] = useState(current);
  const [error, setError] = useState<string | null>(null);
  const [topError, setTopError] = useState<string | null>(null);
  const change = useMutation({
    meta: { inline: true },
    mutationFn: (username: string) =>
      api.requestData((c, init) => c.PUT("/api/v1/auth/username", { ...init, body: { username } }), { label: "Changing username" }),
    onSuccess: async (u) => {
      await qc.invalidateQueries({ queryKey: ME_KEY });
      onClose();
      toast("success", `Your username is now ${u.username}.`);
    },
    onError: (e) => {
      if (e instanceof ApiError && (e.code === "DUPLICATE_VALUE" || e.code === "BAD_REQUEST")) setError(e.message);
      else setTopError(e instanceof ApiError ? e.message : "Something went wrong. Try again.");
    },
  });

  function submit(e: FormEvent): void {
    e.preventDefault();
    setTopError(null);
    const problem = usernameProblem(value);
    setError(problem);
    if (problem) return;
    const next = normalizeUsername(value);
    if (next === current) {
      onClose();
      return;
    }
    change.mutate(next);
  }

  return (
    <form onSubmit={submit} noValidate className="space-y-4" aria-label="Change username">
      {topError && (
        <div role="alert" className={alertBox}>
          {topError}
        </div>
      )}
      <Field
        label="Username"
        hint="3 to 30 letters a-z, digits and hyphens. You can change it once every 30 days; links to the old name keep working for 90 days."
        error={error ?? undefined}
      >
        <Input autoFocus autoComplete="username" spellCheck={false} value={value} onChange={(e) => setValue(e.target.value)} />
      </Field>
      <div className="flex justify-end gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Button type="submit" variant="primary" disabled={change.isPending}>
          {change.isPending ? "Saving…" : "Save username"}
        </Button>
      </div>
    </form>
  );
}

/** Mount once near the app root: the change-username dialog, opened from the user menu. */
export function ChangeUsernameHost() {
  const open = useUsernameDialog((s) => s.open);
  const setOpen = useUsernameDialog((s) => s.setOpen);
  const me = useMe();
  if (!open || !me.data) return null;
  return (
    <Dialog
      onClose={() => setOpen(false)}
      title="Change username"
      description="Your username is public: it is part of your blog's address. You still sign in with your email."
      icon={
        <span className={`${iconChip} bg-accent-soft text-accent`}>
          <AtSign size={16} aria-hidden="true" />
        </span>
      }
      width="sm"
    >
      <ChangeUsernameForm current={me.data.user.username} onClose={() => setOpen(false)} />
    </Dialog>
  );
}

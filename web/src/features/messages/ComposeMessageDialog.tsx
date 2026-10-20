import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Building2, MailPlus, Send, UserPlus, X } from "lucide-react";
import { Button, Dialog, Field, Input, Select, Textarea } from "@/components/ui";
import { alertBox, badgeAccent, iconChip } from "@/components/styles";
import type { Me } from "@/lib/session";
import {
  createMessageThread,
  listMessageRecipients,
  messageKeys,
  type MessageThreadDetail,
} from "./messagesApi";

export function ComposeMessageDialog({
  me,
  defaultWorkspaceId,
  onClose,
  onCreated,
}: {
  me: Me;
  defaultWorkspaceId?: string;
  onClose: () => void;
  onCreated: (thread: MessageThreadDetail) => void;
}) {
  const queryClient = useQueryClient();
  const directory = useQuery({
    queryKey: messageKeys.recipients,
    queryFn: listMessageRecipients,
  });

  const fallbackWorkspaces = (me.memberships ?? [])
    .filter((m) => !m.system_kind && m.status === "active")
    .map((m) => ({ id: m.workspace_id, name: m.workspace_name, role: m.role }));
  const workspaces = directory.data?.workspaces ?? fallbackWorkspaces;
  const users = (directory.data?.users ?? []).filter((u) => u.id !== me.user.id);

  const [kind, setKind] = useState<"direct" | "workspace">(
    defaultWorkspaceId ? "workspace" : "direct",
  );
  const [workspaceId, setWorkspaceId] = useState<string>(
    defaultWorkspaceId ?? workspaces[0]?.id ?? "",
  );
  const [recipients, setRecipients] = useState<string[]>([]);
  const [recipientDraft, setRecipientDraft] = useState("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  const selectedWsId = workspaceId || workspaces[0]?.id || "";

  function addRecipient(raw: string) {
    const clean = raw.trim().replace(/^@/, "");
    if (!clean) return;
    if (!recipients.includes(clean)) {
      setRecipients((prev) => [...prev, clean]);
    }
    setRecipientDraft("");
    setFormError(null);
  }

  function removeRecipient(target: string) {
    setRecipients((prev) => prev.filter((r) => r !== target));
  }

  const sendMutation = useMutation({
    mutationFn: () => {
      const pending = recipientDraft.trim().replace(/^@/, "");
      const finalRecipients =
        pending && !recipients.includes(pending)
          ? [...recipients, pending]
          : recipients;
      return createMessageThread(
        kind === "workspace"
          ? {
              kind: "workspace",
              workspace_id: selectedWsId,
              subject: subject.trim(),
              body: body.trim(),
            }
          : {
              kind: "direct",
              recipients: finalRecipients,
              subject: subject.trim(),
              body: body.trim(),
            },
      );
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: messageKeys.all });
      onCreated(created);
      onClose();
    },
    onError: (err) => {
      setFormError(err instanceof Error ? err.message : "Could not send message.");
    },
  });

  function handleSubmit(e?: FormEvent) {
    e?.preventDefault();
    setFormError(null);
    const pending = recipientDraft.trim().replace(/^@/, "");
    const finalRecipients =
      pending && !recipients.includes(pending)
        ? [...recipients, pending]
        : recipients;

    if (kind === "direct" && finalRecipients.length === 0) {
      setFormError("Select or type at least one recipient.");
      return;
    }
    if (kind === "workspace" && !selectedWsId) {
      setFormError("Choose a workspace for this thread.");
      return;
    }
    if (!subject.trim()) {
      setFormError("Give the message a subject.");
      return;
    }
    if (!body.trim()) {
      setFormError("Write a message before sending.");
      return;
    }
    sendMutation.mutate();
  }

  const draftLower = recipientDraft.trim().toLowerCase().replace(/^@/, "");
  const matchingUsers = users
    .filter((u) => {
      const key = u.username || u.email;
      if (recipients.includes(key) || recipients.includes(u.id)) return false;
      if (!draftLower) return true;
      return (
        u.username.toLowerCase().includes(draftLower) ||
        u.display_name.toLowerCase().includes(draftLower) ||
        u.email.toLowerCase().includes(draftLower)
      );
    })
    .slice(0, 6);

  return (
    <Dialog
      onClose={onClose}
      title="New message"
      description="Start a direct user-to-user conversation or post a thread to a workspace."
      width="lg"
      icon={
        <span className={`${iconChip} bg-accent-soft text-accent`}>
          <MailPlus size={18} aria-hidden="true" />
        </span>
      }
      footer={
        <>
          <Button onClick={onClose} disabled={sendMutation.isPending}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={() => handleSubmit()}
            disabled={sendMutation.isPending}
          >
            <Send size={15} aria-hidden="true" />
            Send message
          </Button>
        </>
      }
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        <div
          role="group"
          aria-label="Message kind"
          className="inline-flex rounded-lg border border-border bg-surface-2 p-0.5"
        >
          <button
            type="button"
            aria-pressed={kind === "direct"}
            onClick={() => {
              setKind("direct");
              setFormError(null);
            }}
            className={`inline-flex h-7 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors ${
              kind === "direct"
                ? "bg-accent text-accent-fg shadow-xs"
                : "text-fg-muted hover:text-fg"
            }`}
          >
            <UserPlus size={14} aria-hidden="true" />
            Direct message
          </button>
          <button
            type="button"
            aria-pressed={kind === "workspace"}
            onClick={() => {
              setKind("workspace");
              setFormError(null);
            }}
            className={`inline-flex h-7 items-center gap-1.5 rounded-md px-3 text-xs font-medium transition-colors ${
              kind === "workspace"
                ? "bg-accent text-accent-fg shadow-xs"
                : "text-fg-muted hover:text-fg"
            }`}
          >
            <Building2 size={14} aria-hidden="true" />
            Workspace thread
          </button>
        </div>

        {kind === "direct" ? (
          <div className="space-y-2">
            <Field
              label="Recipients"
              hint="Pick colleagues below or type a @username or email and press Enter."
            >
              <Input
                value={recipientDraft}
                placeholder="Type @username or email…"
                onChange={(e) => setRecipientDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === ",") {
                    if (recipientDraft.trim()) {
                      e.preventDefault();
                      addRecipient(recipientDraft);
                    }
                  }
                }}
              />
            </Field>
            {recipients.length > 0 && (
              <div
                aria-label="Selected recipients"
                className="flex flex-wrap items-center gap-1.5"
              >
                {recipients.map((r) => (
                  <span key={r} className={`${badgeAccent} gap-1 pr-1`}>
                    <span>@{r.replace(/^@/, "")}</span>
                    <button
                      type="button"
                      aria-label={`Remove ${r}`}
                      onClick={() => removeRecipient(r)}
                      className="inline-flex h-4 w-4 items-center justify-center rounded-full hover:bg-surface-3"
                    >
                      <X size={11} aria-hidden="true" />
                    </button>
                  </span>
                ))}
              </div>
            )}
            {matchingUsers.length > 0 && (
              <div className="rounded-lg border border-border bg-surface-2/60 p-2">
                <p className="m-0 mb-1.5 text-xs font-medium text-fg-muted">
                  Suggested recipients
                </p>
                <div className="flex flex-wrap gap-1.5">
                  {matchingUsers.map((u) => {
                    const handle = u.username || u.email;
                    return (
                      <button
                        key={u.id}
                        type="button"
                        onClick={() => addRecipient(handle)}
                        className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface px-2 py-1 text-xs text-fg transition-colors hover:border-accent/50 hover:bg-surface-3"
                      >
                        <span className="font-medium">
                          {u.display_name || u.email}
                        </span>
                        {u.username && (
                          <span className="text-fg-muted">@{u.username}</span>
                        )}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        ) : (
          <Field
            label="Workspace"
            hint="All members of the workspace will receive this thread and can reply."
          >
            <Select
              value={selectedWsId}
              onChange={(e) => setWorkspaceId(e.target.value)}
            >
              {workspaces.length === 0 ? (
                <option value="">No active workspaces available</option>
              ) : (
                workspaces.map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name} ({w.role})
                  </option>
                ))
              )}
            </Select>
          </Field>
        )}

        <Field label="Subject" required>
          <Input
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder="e.g., Q3 inventory audit questions"
            maxLength={200}
          />
        </Field>

        <Field label="Message" required>
          <Textarea
            rows={5}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="Write your message…"
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault();
                handleSubmit();
              }
            }}
          />
        </Field>

        {formError && (
          <div role="alert" className={alertBox}>
            {formError}
          </div>
        )}
      </form>
    </Dialog>
  );
}

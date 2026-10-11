import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { UserMinus, UserPlus } from "lucide-react";
import { ApiError } from "@/lib/api";
import { useMe } from "@/lib/session";
import { Modal, selectClass } from "@/features/queries/Modal";
import { alertBox, badge, badgeAccent, btn, btnDanger, btnPrimary, input, label } from "@/components/styles";
import { Tip } from "@/components/Tip";
import { apiKeyKeys, listApiKeys, revokeApiKey } from "@/features/apikeys/apiKeysApi";
import { MEMBER_ROLES, activeKeysCreatedBy, emailError, initials, isDemotion, isLastOwner, roleLabel } from "./memberLogic";
import { addMember, listMembers, memberKeys, removeMember, updateMemberRole, type Member, type MemberRole } from "./membersApi";

function message(e: unknown, fallback: string): string {
  return e instanceof ApiError ? e.message : fallback;
}

type Pending = { kind: "remove"; member: Member } | { kind: "role"; member: Member; role: MemberRole };

function AddDialog({ ws, onClose, onAdded }: { ws: string; onClose: () => void; onAdded: (m: Member) => void }) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<MemberRole>("editor");
  const [emailErr, setEmailErr] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const add = useMutation({
    meta: { inline: true },
    mutationFn: () => addMember(ws, { email: email.trim(), role }),
    onSuccess: onAdded,
    onError: (e) => setError(message(e, "The member could not be added.")),
  });
  function submit(e: FormEvent) {
    e.preventDefault();
    const problem = emailError(email);
    setEmailErr(problem);
    setError(null);
    if (problem) return;
    add.mutate();
  }
  return (
    <Modal
      title="Add a member"
      description="Only people who already have an account can be added. A platform administrator creates accounts."
      onClose={() => (add.isPending ? undefined : onClose())}
    >
      <form noValidate aria-label="Add member" onSubmit={submit} className="flex flex-col gap-3">
        <div>
          <label htmlFor="member-email" className={label}>
            Email
          </label>
          <input
            id="member-email"
            type="email"
            className={input}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="off"
            placeholder="name@example.com"
            aria-invalid={emailErr ? true : undefined}
            aria-describedby={emailErr ? "member-email-err" : undefined}
            disabled={add.isPending}
          />
          {emailErr && (
            <p id="member-email-err" role="alert" className="m-0 mt-1 text-sm text-danger">
              {emailErr}
            </p>
          )}
        </div>
        <div>
          <label htmlFor="member-role" className={label}>
            Role
          </label>
          <select id="member-role" className={selectClass} value={role} onChange={(e) => setRole(e.target.value as MemberRole)} disabled={add.isPending}>
            {MEMBER_ROLES.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </div>
        {error && (
          <div role="alert" className={alertBox}>
            {error}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose} disabled={add.isPending}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={add.isPending} aria-busy={add.isPending}>
            {add.isPending ? "Adding…" : "Add member"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function ConfirmDialog({
  ws,
  pending,
  self,
  lastOwner,
  keys,
  onClose,
  onDone,
}: {
  ws: string;
  pending: Pending;
  self: boolean;
  lastOwner: boolean;
  keys: { id: string; name: string }[];
  onClose: () => void;
  onDone: (summary: string) => void;
}) {
  const m = pending.member;
  const who = m.display_name || m.email;
  const removing = pending.kind === "remove";
  const [revokeKeys, setRevokeKeys] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const run = useMutation({
    meta: { inline: true },
    mutationFn: async () => {
      if (pending.kind === "remove") await removeMember(ws, m.user_id);
      else await updateMemberRole(ws, m.user_id, pending.role);
      let revoked = 0;
      if (revokeKeys) {
        for (const k of keys) {
          await revokeApiKey(ws, k.id);
          revoked++;
        }
      }
      return revoked;
    },
    onSuccess: (revoked) => {
      const base = pending.kind === "remove" ? `Removed ${who}.` : `${who} is now ${roleLabel(pending.role)}.`;
      onDone(revoked > 0 ? `${base} Revoked ${revoked} API ${revoked === 1 ? "key" : "keys"}.` : base);
    },
    onError: (e) => setError(message(e, removing ? "The member could not be removed." : "The role could not be changed.")),
  });
  const title = removing ? `Remove ${who}?` : `Change ${who} to ${roleLabel(pending.kind === "role" ? pending.role : m.role)}?`;
  return (
    <Modal title={title} onClose={() => (run.isPending ? undefined : onClose())}>
      <div className="flex flex-col gap-3">
        <p className="m-0">
          {removing
            ? `${who} will lose access to this workspace right away.`
            : `${who} will lose the permissions of the ${roleLabel(m.role)} role right away.`}
        </p>
        {self && <p className="m-0 font-medium">This is your own account. {removing ? "You will lose access to this workspace." : "You may no longer be able to manage members."}</p>}
        {lastOwner && <p className="m-0 text-fg-muted">This is the only owner. A workspace must keep at least one owner, so the server will refuse this.</p>}
        {keys.length > 0 && (
          <div className="flex items-start gap-2">
            <input
              id="member-revoke-keys"
              type="checkbox"
              checked={revokeKeys}
              onChange={(e) => setRevokeKeys(e.target.checked)}
              disabled={run.isPending}
              className="mt-1"
              aria-describedby="member-revoke-keys-list"
            />
            <div className="text-sm">
              <label htmlFor="member-revoke-keys" className="font-medium">
                Also revoke {keys.length} API {keys.length === 1 ? "key" : "keys"} they created
              </label>
              <p className="m-0 text-fg-muted">API keys keep working after their creator leaves or is demoted, unless you revoke them.</p>
              <ul id="member-revoke-keys-list" className="m-0 mt-1 list-disc pl-5">
                {keys.map((k) => (
                  <li key={k.id}>{k.name}</li>
                ))}
              </ul>
            </div>
          </div>
        )}
        {error && (
          <div role="alert" className={alertBox}>
            {error}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose} disabled={run.isPending}>
            Cancel
          </button>
          <button
            type="button"
            className={removing ? btnDanger : btnPrimary}
            onClick={() => {
              setError(null);
              run.mutate();
            }}
            disabled={run.isPending}
            aria-busy={run.isPending}
          >
            {run.isPending ? "Working…" : removing ? "Remove member" : "Change role"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

/** Members section of the workspace settings. Owners only: the server enforces it, and non-owners see nothing. */
export function MembersPanel({ ws }: { ws: string }) {
  const me = useMe();
  const role = me.data?.memberships?.find((x) => x.workspace_id === ws)?.role;
  if (role !== "owner") return null;
  return <MembersPanelInner ws={ws} selfId={me.data?.user.id ?? ""} isPlatformAdmin={!!me.data?.user.is_platform_admin} />;
}

function MembersPanelInner({ ws, selfId, isPlatformAdmin }: { ws: string; selfId: string; isPlatformAdmin: boolean }) {
  const qc = useQueryClient();
  const [adding, setAdding] = useState(false);
  const [pending, setPending] = useState<Pending | null>(null);
  const [status, setStatus] = useState("");
  const [rowError, setRowError] = useState<string | null>(null);
  const list = useQuery({ queryKey: memberKeys.list(ws), queryFn: () => listMembers(ws) });
  const keyList = useQuery({ queryKey: apiKeyKeys.list(ws), queryFn: () => listApiKeys(ws) });
  const rows = list.data ?? [];

  const changeRole = useMutation({
    meta: { inline: true },
    mutationFn: (v: { member: Member; role: MemberRole }) => updateMemberRole(ws, v.member.user_id, v.role),
    onSuccess: (_d, v) => {
      setStatus(`${v.member.display_name || v.member.email} is now ${roleLabel(v.role)}.`);
      void qc.invalidateQueries({ queryKey: memberKeys.list(ws) });
    },
    onError: (e) => {
      setRowError(message(e, "The role could not be changed."));
      void qc.invalidateQueries({ queryKey: memberKeys.list(ws) });
    },
  });

  function pickRole(m: Member, next: MemberRole) {
    setRowError(null);
    if (next === m.role) return;
    if (isDemotion(m.role, next)) setPending({ kind: "role", member: m, role: next });
    else changeRole.mutate({ member: m, role: next });
  }

  function refreshAll() {
    void qc.invalidateQueries({ queryKey: memberKeys.list(ws) });
    void qc.invalidateQueries({ queryKey: apiKeyKeys.list(ws) });
    // Removing or demoting yourself changes what you may see.
    void qc.invalidateQueries({ queryKey: ["me"] });
  }

  return (
    <section aria-labelledby="members-heading" data-testid="members-panel" className="mt-6">
      <h3 id="members-heading" className="mb-2 text-sm font-semibold">
        Members
      </h3>
      <p className="m-0 mb-3 text-sm text-fg-muted">
        Everyone who can open this workspace, and what they may do. You can only add people who already have an account: a platform administrator creates
        accounts
        {isPlatformAdmin ? (
          <>
            {" "}
            (in <Link to="/admin" search={{ tab: "users" }} className="text-accent underline">Admin console &rsaquo; Users</Link>)
          </>
        ) : null}
        .
      </p>
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <button type="button" className={btnPrimary} onClick={() => setAdding(true)}>
          <UserPlus size={14} aria-hidden="true" />
          Add member
        </button>
        <span role="status" aria-live="polite" className="text-sm text-fg-muted">
          {status}
        </span>
      </div>
      {(list.isError || rowError) && (
        <div role="alert" className={`${alertBox} mb-3`}>
          {rowError ?? message(list.error, "The members could not be loaded.")}
          {list.isError && (
            <button type="button" className={`${btn} ml-2`} onClick={() => void list.refetch()}>
              Try again
            </button>
          )}
        </div>
      )}
      {list.isPending && (
        <p role="status" className="m-0 text-fg-muted">
          Loading members…
        </p>
      )}
      {list.data && (
        <table className="stack-table w-full border-collapse text-sm" role="table">
          <caption className="sr-only">Workspace members</caption>
          <thead>
            <tr role="row" className="text-left">
              <th scope="col" role="columnheader" className="px-2 py-1 font-semibold">Member</th>
              <th scope="col" role="columnheader" className="px-2 py-1 font-semibold">Email</th>
              <th scope="col" role="columnheader" className="px-2 py-1 font-semibold">Role</th>
              <th scope="col" role="columnheader" className="px-2 py-1 font-semibold">Change role</th>
              <th scope="col" role="columnheader" className="px-2 py-1">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((m) => {
              const name = m.display_name || m.email;
              return (
                <tr key={m.user_id} role="row" data-member-id={m.user_id} data-email={m.email} className="border-t border-border align-middle">
                  <th scope="row" role="rowheader" className="px-2 py-1.5 text-left font-medium">
                    <span className="flex items-center gap-2">
                      <span
                        aria-hidden="true"
                        className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-surface-3 text-xs font-semibold text-fg"
                      >
                        {initials(m.display_name, m.email)}
                      </span>
                      <span>
                        {name}
                        {m.user_id === selfId && <span className="ml-1 text-xs font-normal text-fg-muted">(you)</span>}
                      </span>
                    </span>
                  </th>
                  <td role="cell" data-label="Email" className="px-2 py-1.5 [overflow-wrap:anywhere]">{m.email}</td>
                  <td role="cell" data-label="Role" className="px-2 py-1.5">
                    <span className={m.role === "owner" ? badgeAccent : badge}>{roleLabel(m.role)}</span>
                  </td>
                  <td role="cell" data-label="Change role" className="px-2 py-1.5">
                    <select
                      aria-label={`Role for ${name}`}
                      className={`${selectClass} max-w-36`}
                      value={m.role}
                      disabled={changeRole.isPending}
                      onChange={(e) => pickRole(m, e.target.value as MemberRole)}
                    >
                      {MEMBER_ROLES.map((r) => (
                        <option key={r.value} value={r.value}>
                          {r.label}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td role="cell" data-stack="full" className="px-2 py-1.5">
                    <div className="flex justify-end">
                      <Tip label="Remove from this workspace">
                        <button
                          type="button"
                          className={btn}
                          aria-label={`Remove ${name}`}
                          onClick={() => {
                            setRowError(null);
                            setPending({ kind: "remove", member: m });
                          }}
                        >
                          <UserMinus size={14} aria-hidden="true" />
                          Remove
                        </button>
                      </Tip>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {adding && (
        <AddDialog
          ws={ws}
          onClose={() => setAdding(false)}
          onAdded={(m) => {
            setAdding(false);
            setStatus(`Added ${m.display_name || m.email} as ${roleLabel(m.role)}.`);
            void qc.invalidateQueries({ queryKey: memberKeys.list(ws) });
          }}
        />
      )}
      {pending && (
        <ConfirmDialog
          ws={ws}
          pending={pending}
          self={pending.member.user_id === selfId}
          lastOwner={isLastOwner(rows, pending.member)}
          keys={activeKeysCreatedBy(keyList.data ?? [], pending.member.user_id).map((k) => ({ id: k.id, name: k.name }))}
          onClose={() => setPending(null)}
          onDone={(summary) => {
            setStatus(summary);
            setPending(null);
            refreshAll();
          }}
        />
      )}
    </section>
  );
}

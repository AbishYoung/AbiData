import { useState } from "react";
import { ContextMenu, DropdownMenu } from "radix-ui";
import { useQuery } from "@tanstack/react-query";
import { Ellipsis, Eye, Pencil, Trash2, type LucideIcon } from "lucide-react";
import { useTabs } from "@/app/tabs";
import { queryClient } from "@/app/queryClient";
import { DeleteJobDialog } from "@/features/jobs/JobDialogs";
import { jobKeys, listJobs, type Job } from "@/features/jobs/jobsApi";
import { RenameJobDialog } from "./JobCrudDialogs";

/** Jobs matching the search text, sorted by name ignoring case. */
export function sortedJobs(jobs: readonly Job[] | null | undefined, query: string): Job[] {
  const q = query.trim().toLowerCase();
  return [...(jobs ?? [])]
    .filter((j) => q === "" || j.name.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
}

interface JobAction {
  id: string;
  label: string;
  icon: LucideIcon;
  run: () => void;
}

const contentClass = "z-50 min-w-44 rounded-md border border-border bg-surface p-1 text-sm text-fg shadow-lg";
const itemClass =
  "flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-fg";

function JobNavItem({ job, active, actions, onOpen }: { job: Job; active: boolean; actions: JobAction[]; onOpen: () => void }) {
  return (
    <li className="m-0 list-none">
      <ContextMenu.Root>
        <ContextMenu.Trigger asChild>
          <div className="group flex items-center pr-1 hover:bg-surface-2">
            <button
              type="button"
              aria-current={active ? "true" : undefined}
              onClick={onOpen}
              className={`min-w-0 flex-1 cursor-pointer truncate py-1.5 pl-9 pr-2 text-left text-sm ${active ? "font-semibold" : ""}`}
            >
              {job.name}
            </button>
            {!job.enabled && <span className="shrink-0 pr-1 text-xs text-fg-muted">Disabled</span>}
            <DropdownMenu.Root>
              <DropdownMenu.Trigger asChild>
                <button
                  type="button"
                  aria-label={`More actions for ${job.name}`}
                  className="inline-flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-md hover:bg-surface"
                >
                  <Ellipsis size={16} aria-hidden="true" />
                </button>
              </DropdownMenu.Trigger>
              <DropdownMenu.Portal>
                <DropdownMenu.Content align="end" className={contentClass}>
                  {actions.map((a) => (
                    <DropdownMenu.Item key={a.id} className={itemClass} onSelect={a.run}>
                      <a.icon size={14} aria-hidden="true" />
                      {a.label}
                    </DropdownMenu.Item>
                  ))}
                </DropdownMenu.Content>
              </DropdownMenu.Portal>
            </DropdownMenu.Root>
          </div>
        </ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Content className={contentClass}>
            {actions.map((a) => (
              <ContextMenu.Item key={a.id} className={itemClass} onSelect={a.run}>
                <a.icon size={14} aria-hidden="true" />
                {a.label}
              </ContextMenu.Item>
            ))}
          </ContextMenu.Content>
        </ContextMenu.Portal>
      </ContextMenu.Root>
    </li>
  );
}

type Dialog = { kind: "rename" | "delete"; job: Job };

/** Body of the Jobs navigation group (builder and owner roles only). */
export function JobsGroupBody({ ws, panelId, query, activeKey }: { ws: string; panelId: string; query: string; activeKey: string | null }) {
  const openTab = useTabs((s) => s.open);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const jobs = useQuery({ queryKey: jobKeys.list(ws), queryFn: () => listJobs(ws) }, queryClient);
  const shown = sortedJobs(jobs.data, query);
  const searching = query.trim() !== "";

  function open(j: Job): void {
    openTab(ws, { key: `job:${j.id}`, kind: "job", objectId: j.id, view: "design", title: j.name });
  }

  return (
    <div id={panelId}>
      {shown.length > 0 ? (
        <ul className="m-0 p-0">
          {shown.map((j) => (
            <JobNavItem
              key={j.id}
              job={j}
              active={activeKey === `job:${j.id}`}
              onOpen={() => open(j)}
              actions={[
                { id: "open", label: "Open", icon: Eye, run: () => open(j) },
                { id: "rename", label: "Rename", icon: Pencil, run: () => setDialog({ kind: "rename", job: j }) },
                { id: "delete", label: "Delete", icon: Trash2, run: () => setDialog({ kind: "delete", job: j }) },
              ]}
            />
          ))}
        </ul>
      ) : (
        <p className="px-9 py-1 text-sm text-fg-muted">
          {jobs.isError
            ? "Jobs could not be loaded"
            : jobs.isPending
              ? "Loading jobs…"
              : searching
                ? `No jobs match “${query.trim()}”`
                : "No jobs yet"}
        </p>
      )}
      {dialog?.kind === "rename" && <RenameJobDialog ws={ws} job={dialog.job} onClose={() => setDialog(null)} />}
      {dialog?.kind === "delete" && <DeleteJobDialog ws={ws} job={dialog.job} onClose={() => setDialog(null)} />}
    </div>
  );
}

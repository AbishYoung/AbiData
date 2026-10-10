import { useState } from "react";
import { ContextMenu, DropdownMenu } from "radix-ui";
import { useQuery } from "@tanstack/react-query";
import { Ellipsis, Eye, Pencil, Trash2, type LucideIcon } from "lucide-react";
import { useTabs } from "@/app/tabs";
import { queryClient } from "@/app/queryClient";
import { DeleteScriptDialog } from "@/features/scripts/DeleteScriptDialog";
import { listScripts, scriptKeys, type Script } from "@/features/scripts/scriptsApi";
import { RenameScriptDialog } from "./ScriptCrudDialogs";

export const SCRIPT_KIND_LABEL: Readonly<Record<string, string>> = { module: "Module", job: "Job" };

/** Scripts matching the search text, sorted by name ignoring case. */
export function sortedScripts(scripts: readonly Script[] | null | undefined, query: string): Script[] {
  const q = query.trim().toLowerCase();
  return [...(scripts ?? [])]
    .filter((s) => q === "" || s.name.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
}

interface ScriptAction {
  id: string;
  label: string;
  icon: LucideIcon;
  run: () => void;
}

const contentClass = "z-50 min-w-44 rounded-md border border-border bg-surface p-1 text-sm text-fg shadow-lg";
const itemClass =
  "flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-fg";

function ScriptNavItem({
  script,
  active,
  actions,
  onOpen,
}: {
  script: Script;
  active: boolean;
  actions: ScriptAction[];
  onOpen: () => void;
}) {
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
              {script.name}
            </button>
            <span className="shrink-0 pr-1 text-xs text-fg-muted">{SCRIPT_KIND_LABEL[script.kind] ?? script.kind}</span>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger asChild>
                <button
                  type="button"
                  aria-label={`More actions for ${script.name}`}
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

type Dialog = { kind: "rename" | "delete"; script: Script };

/** Body of the Scripts navigation group (builder and owner roles only). */
export function ScriptsGroupBody({
  ws,
  panelId,
  query,
  activeKey,
}: {
  ws: string;
  panelId: string;
  query: string;
  activeKey: string | null;
}) {
  const openTab = useTabs((s) => s.open);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const scripts = useQuery({ queryKey: scriptKeys.list(ws), queryFn: () => listScripts(ws) }, queryClient);
  const shown = sortedScripts(scripts.data, query);
  const searching = query.trim() !== "";

  function open(s: Script): void {
    openTab(ws, { key: `script:${s.id}`, kind: "script", objectId: s.id, view: "design", title: s.name });
  }

  return (
    <div id={panelId}>
      {shown.length > 0 ? (
        <ul className="m-0 p-0">
          {shown.map((s) => (
            <ScriptNavItem
              key={s.id}
              script={s}
              active={activeKey === `script:${s.id}`}
              onOpen={() => open(s)}
              actions={[
                { id: "open", label: "Open", icon: Eye, run: () => open(s) },
                { id: "rename", label: "Rename", icon: Pencil, run: () => setDialog({ kind: "rename", script: s }) },
                { id: "delete", label: "Delete", icon: Trash2, run: () => setDialog({ kind: "delete", script: s }) },
              ]}
            />
          ))}
        </ul>
      ) : (
        <p className="px-9 py-1 text-sm text-fg-muted">
          {scripts.isError
            ? "Scripts could not be loaded"
            : scripts.isPending
              ? "Loading scripts…"
              : searching
                ? `No scripts match “${query.trim()}”`
                : "No scripts yet"}
        </p>
      )}
      {dialog?.kind === "rename" && <RenameScriptDialog ws={ws} script={dialog.script} onClose={() => setDialog(null)} />}
      {dialog?.kind === "delete" && <DeleteScriptDialog ws={ws} script={dialog.script} onClose={() => setDialog(null)} />}
    </div>
  );
}

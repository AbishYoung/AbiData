import { Tip } from "@/components/Tip";
import { useState } from "react";
import { navEmpty, navItemButton, navMenu, navMenuItem, navMoreButton, navRow, navRowActive } from "./navStyles";
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
          <div className={`${navRow} ${active ? navRowActive : ""}`}>
            <button
              type="button"
              aria-current={active ? "true" : undefined}
              onClick={onOpen}
              className={navItemButton(active)}
            >
              {script.name}
            </button>
            <span className="shrink-0 pr-1 text-xs text-fg-muted">{SCRIPT_KIND_LABEL[script.kind] ?? script.kind}</span>
            <DropdownMenu.Root>
              <Tip side="right" label={`More actions for ${script.name}`}>
                <DropdownMenu.Trigger asChild>
                  <button
                    type="button"
                    aria-label={`More actions for ${script.name}`}
                    className={navMoreButton}
                  >
                    <Ellipsis size={15} aria-hidden="true" />
                  </button>
                </DropdownMenu.Trigger>
              </Tip>
              <DropdownMenu.Portal>
                <DropdownMenu.Content align="end" className={navMenu}>
                  {actions.map((a) => (
                    <DropdownMenu.Item key={a.id} className={navMenuItem} onSelect={a.run}>
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
          <ContextMenu.Content className={navMenu}>
            {actions.map((a) => (
              <ContextMenu.Item key={a.id} className={navMenuItem} onSelect={a.run}>
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
        <p className={navEmpty}>
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

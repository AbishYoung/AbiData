import { ContextMenu, DropdownMenu } from "radix-ui";
import { navItemButton, navMenu, navMenuItem, navMoreButton, navRow, navRowActive } from "./navStyles";
import { Code, Download, Ellipsis, Eye, Pencil, Ruler, Trash2, type LucideIcon } from "lucide-react";
import type { Saved } from "@/features/queries/savedQueries";

export interface QueryAction {
  id: string;
  label: string;
  icon: LucideIcon;
  run: () => void;
}

/** The actions available to a role on one saved query. */
export function queryActions(
  canDesign: boolean,
  handlers: { open: () => void; design: () => void; sql: () => void; rename: () => void; remove: () => void; export?: () => void },
): QueryAction[] {
  const actions: QueryAction[] = [{ id: "open", label: "Open", icon: Eye, run: handlers.open }];
  if (handlers.export) actions.push({ id: "export", label: "Export…", icon: Download, run: handlers.export });
  if (canDesign) {
    actions.push(
      { id: "design", label: "Design view", icon: Ruler, run: handlers.design },
      { id: "sql", label: "SQL view", icon: Code, run: handlers.sql },
      { id: "rename", label: "Rename", icon: Pencil, run: handlers.rename },
      { id: "delete", label: "Delete", icon: Trash2, run: handlers.remove },
    );
  }
  return actions;
}


export function QueryNavItem({
  query,
  active,
  actions,
  onOpen,
}: {
  query: Saved;
  active: boolean;
  actions: QueryAction[];
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
              {query.name}
            </button>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger asChild>
                <button
                  type="button"
                  aria-label={`More actions for ${query.name}`}
                  className={navMoreButton}
                >
                  <Ellipsis size={15} aria-hidden="true" />
                </button>
              </DropdownMenu.Trigger>
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

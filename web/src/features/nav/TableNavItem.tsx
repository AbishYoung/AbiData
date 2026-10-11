import { Tip } from "@/components/Tip";
import { ContextMenu, DropdownMenu } from "radix-ui";
import { navItemButton, navMenu, navMenuItem, navMoreButton, navRow, navRowActive } from "./navStyles";
import { Download, Ellipsis, Eye, Pencil, Ruler, Trash2, Upload, type LucideIcon } from "lucide-react";
import type { Table } from "@/lib/schema";

export interface TableAction {
  id: string;
  label: string;
  icon: LucideIcon;
  run: () => void;
}

/** The actions available to a role on one table. */
export function tableActions(
  canDesign: boolean,
  handlers: { open: () => void; design: () => void; rename: () => void; remove: () => void; export?: () => void; importInto?: () => void },
): TableAction[] {
  const actions: TableAction[] = [{ id: "open", label: "Open", icon: Eye, run: handlers.open }];
  if (handlers.export) actions.push({ id: "export", label: "Export…", icon: Download, run: handlers.export });
  if (canDesign) {
    if (handlers.importInto) actions.push({ id: "import", label: "Import into this table…", icon: Upload, run: handlers.importInto });
    actions.push(
      { id: "design", label: "Design view", icon: Ruler, run: handlers.design },
      { id: "rename", label: "Rename", icon: Pencil, run: handlers.rename },
      { id: "delete", label: "Delete", icon: Trash2, run: handlers.remove },
    );
  }
  return actions;
}


export function TableNavItem({
  table,
  active,
  actions,
  onOpen,
}: {
  table: Table;
  active: boolean;
  actions: TableAction[];
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
              {table.display_name}
            </button>
            <DropdownMenu.Root>
              <Tip side="right" label={`More actions for ${table.display_name}`}>
                <DropdownMenu.Trigger asChild>
                  <button
                    type="button"
                    aria-label={`More actions for ${table.display_name}`}
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

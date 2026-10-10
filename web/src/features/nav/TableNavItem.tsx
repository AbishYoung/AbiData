import { ContextMenu, DropdownMenu } from "radix-ui";
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

const contentClass = "z-50 min-w-44 rounded-md border border-border bg-surface p-1 text-sm text-fg shadow-lg";
const itemClass =
  "flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-fg";

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
          <div className="group flex items-center pr-1 hover:bg-surface-2">
            <button
              type="button"
              aria-current={active ? "true" : undefined}
              onClick={onOpen}
              className={`min-w-0 flex-1 cursor-pointer truncate py-1.5 pl-9 pr-2 text-left text-sm ${active ? "font-semibold" : ""}`}
            >
              {table.display_name}
            </button>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger asChild>
                <button
                  type="button"
                  aria-label={`More actions for ${table.display_name}`}
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

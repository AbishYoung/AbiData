import { useId, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Clock,
  FileText,
  FormInput,
  Plus,
  Search,
  Table2,
  Terminal,
  type LucideIcon,
} from "lucide-react";
import { input } from "@/components/styles";
import { useTabs, useWorkspaceTabs, type TableView } from "@/app/tabs";
import { useSchema, type Table } from "@/lib/schema";
import { DeleteTableDialog, RenameTableDialog } from "./TableCrudDialogs";
import { TableNavItem, tableActions } from "./TableNavItem";

interface NavGroup {
  id: string;
  label: string;
  noun: string;
  icon: LucideIcon;
}

const GROUPS: NavGroup[] = [
  { id: "tables", label: "Tables", noun: "tables", icon: Table2 },
  { id: "queries", label: "Queries", noun: "queries", icon: Search },
  { id: "forms", label: "Forms", noun: "forms", icon: FormInput },
  { id: "reports", label: "Reports", noun: "reports", icon: FileText },
  { id: "scripts", label: "Scripts", noun: "scripts", icon: Terminal },
  { id: "jobs", label: "Jobs", noun: "jobs", icon: Clock },
];

function canDesign(role: string | undefined): boolean {
  return role === "owner" || role === "builder";
}

export function sortedTables(tables: readonly Table[] | null | undefined, query: string): Table[] {
  const q = query.trim().toLowerCase();
  return [...(tables ?? [])]
    .filter((t) => q === "" || t.display_name.toLowerCase().includes(q))
    .sort((a, b) => a.display_name.localeCompare(b.display_name, undefined, { sensitivity: "base" }));
}

type TableDialog = { kind: "rename" | "delete"; table: Table };

export function NavPane({ ws, role }: { ws: string; role: string | undefined }) {
  const searchId = useId();
  const [query, setQuery] = useState("");
  const [dialog, setDialog] = useState<TableDialog | null>(null);
  const schema = useSchema(ws);
  const { tabs, active } = useWorkspaceTabs(ws);
  const openTab = useTabs((s) => s.open);
  const design = canDesign(role);
  const tables = sortedTables(schema.data?.tables, query);

  function openTable(t: Table, view: TableView): void {
    openTab(ws, { key: `table:${t.id}`, kind: "table", objectId: t.id, view, title: t.display_name });
  }

  function newTable(): void {
    let n = 1;
    while (tabs.some((t) => t.key === `table:new:${n}`)) n++;
    openTab(ws, { key: `table:new:${n}`, kind: "table", objectId: null, view: "design", title: n === 1 ? "New table" : `New table ${n}` });
  }
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

  function toggle(id: string) {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  const searching = query.trim() !== "";

  return (
    <nav aria-label="Navigation pane" data-ws={ws} data-role={role} className="flex w-64 shrink-0 flex-col border-r border-border bg-surface">
      <div className="border-b border-border p-2">
        <label htmlFor={searchId} className="sr-only">
          Search objects
        </label>
        <div className="relative">
          <Search size={14} aria-hidden="true" className="pointer-events-none absolute left-2 top-2.5 text-fg-muted" />
          <input
            id={searchId}
            type="search"
            placeholder="Search objects"
            className={`${input} pl-7`}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
      </div>
      <div className="flex-1 overflow-y-auto py-1">
        {GROUPS.map((g) => {
          const open = !collapsed.has(g.id);
          const Icon = g.icon;
          const panelId = `nav-group-${g.id}`;
          return (
            <section key={g.id} aria-label={g.label}>
              <div className="flex items-center">
                <h2 className="m-0 min-w-0 flex-1 text-sm font-semibold">
                  <button
                    type="button"
                    aria-expanded={open}
                    aria-controls={panelId}
                    onClick={() => toggle(g.id)}
                    className="flex w-full cursor-pointer items-center gap-2 px-3 py-1.5 text-left hover:bg-surface-2"
                  >
                    {open ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
                    <Icon size={16} aria-hidden="true" />
                    {g.label}
                  </button>
                </h2>
                {g.id === "tables" && design && (
                  <button
                    type="button"
                    onClick={newTable}
                    aria-label="New table"
                    title="New table"
                    className="mr-1 inline-flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-md hover:bg-surface-2"
                  >
                    <Plus size={16} aria-hidden="true" />
                  </button>
                )}
              </div>
              {open && g.id === "tables" && (
                <div id={panelId}>
                  {tables.length > 0 ? (
                    <ul className="m-0 p-0">
                      {tables.map((t) => (
                        <TableNavItem
                          key={t.id}
                          table={t}
                          active={active === `table:${t.id}`}
                          onOpen={() => openTable(t, "datasheet")}
                          actions={tableActions(design, {
                            open: () => openTable(t, "datasheet"),
                            design: () => openTable(t, "design"),
                            rename: () => setDialog({ kind: "rename", table: t }),
                            remove: () => setDialog({ kind: "delete", table: t }),
                          })}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className="px-9 py-1 text-sm text-fg-muted">
                      {schema.isError
                        ? "Tables could not be loaded"
                        : schema.isPending
                          ? "Loading tables…"
                          : searching
                            ? `No ${g.noun} match “${query.trim()}”`
                            : `No ${g.noun} yet`}
                    </p>
                  )}
                </div>
              )}
              {open && g.id !== "tables" && (
                <p id={panelId} className="px-9 py-1 text-sm text-fg-muted">
                  {searching ? `No ${g.noun} match “${query.trim()}”` : `No ${g.noun} yet`}
                </p>
              )}
            </section>
          );
        })}
      </div>
      {dialog?.kind === "rename" && (
        <RenameTableDialog ws={ws} table={dialog.table} schema={schema.data} onClose={() => setDialog(null)} />
      )}
      {dialog?.kind === "delete" && (
        <DeleteTableDialog ws={ws} table={dialog.table} schema={schema.data} onClose={() => setDialog(null)} />
      )}
    </nav>
  );
}

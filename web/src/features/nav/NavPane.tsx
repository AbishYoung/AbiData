import { useId, useState, type ReactNode } from "react";
import { ChevronDown, Plus, Search, SquareTerminal, Upload } from "lucide-react";
import { input } from "@/components/styles";
import { Tip } from "@/components/Tip";
import { KindIcon } from "@/components/objectKinds";
import { navEmpty } from "./navStyles";
import { useTabs, useWorkspaceTabs, type TabKind, type TableView } from "@/app/tabs";
import { useSchema, type Table } from "@/lib/schema";
import { DeleteTableDialog, RenameTableDialog } from "./TableCrudDialogs";
import { ScriptsGroupBody } from "./ScriptNavItem";
import { ReportsGroupBody } from "./ReportNavItem";
import { FormsGroupBody } from "./FormNavItem";
import { JobsGroupBody } from "./JobNavItem";
import { TableNavItem, tableActions } from "./TableNavItem";
import { useSavedQueries, type Saved } from "@/features/queries/savedQueries";
import { DeleteQueryDialog, RenameQueryDialog } from "./QueryCrudDialogs";
import { QueryNavItem, queryActions } from "./QueryNavItem";
import { ImportWizard } from "@/features/imports/ImportWizard";
import { ExportDialog, type ExportSource } from "@/features/imports/ExportDialog";
import { effectiveParams } from "@/features/queries/params";

interface NavGroup {
  id: string;
  label: string;
  noun: string;
  kind: TabKind;
}

const GROUPS: NavGroup[] = [
  { id: "tables", label: "Tables", noun: "tables", kind: "table" },
  { id: "queries", label: "Queries", noun: "queries", kind: "query" },
  { id: "forms", label: "Forms", noun: "forms", kind: "form" },
  { id: "reports", label: "Reports", noun: "reports", kind: "report" },
  { id: "scripts", label: "Scripts", noun: "scripts", kind: "script" },
  { id: "jobs", label: "Jobs", noun: "jobs", kind: "job" },
];

const groupAction =
  "inline-flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded text-fg-subtle transition-colors hover:bg-surface-3 hover:text-fg";

function GroupAction({ label, onClick, children }: { label: string; onClick: () => void; children: ReactNode }) {
  return (
    <Tip label={label}>
      <button type="button" onClick={onClick} aria-label={label} className={groupAction}>
        {children}
      </button>
    </Tip>
  );
}

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
type QueryDialog = { kind: "rename" | "delete"; query: Saved };

export function sortedQueries(queries: readonly Saved[] | null | undefined, query: string): Saved[] {
  const q = query.trim().toLowerCase();
  return [...(queries ?? [])]
    .filter((s) => q === "" || s.name.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
}

export function NavPane({ ws, role, variant = "sidebar" }: { ws: string; role: string | undefined; variant?: "sidebar" | "drawer" }) {
  const searchId = useId();
  const [query, setQuery] = useState("");
  const [dialog, setDialog] = useState<TableDialog | null>(null);
  const schema = useSchema(ws);
  const savedQueries = useSavedQueries(ws);
  const [queryDialog, setQueryDialog] = useState<QueryDialog | null>(null);
  const [importing, setImporting] = useState<{ intoTableId?: string } | null>(null);
  const [exporting, setExporting] = useState<ExportSource | null>(null);
  const { tabs, active } = useWorkspaceTabs(ws);
  const openTab = useTabs((s) => s.open);
  const design = canDesign(role);
  const tables = sortedTables(schema.data?.tables, query);
  const queries = sortedQueries(savedQueries.data, query);

  function openQuery(q: Saved, view: TableView): void {
    openTab(ws, { key: `query:${q.id}`, kind: "query", objectId: q.id, view, title: q.name });
  }

  function newQuery(view: TableView = "design"): void {
    let n = 1;
    while (tabs.some((t) => t.key === `query:new:${n}`)) n++;
    const base = view === "sql" ? "SQL console" : "New query";
    openTab(ws, { key: `query:new:${n}`, kind: "query", objectId: null, view, title: n === 1 ? base : `${base} ${n}` });
  }

  function openTable(t: Table, view: TableView): void {
    openTab(ws, { key: `table:${t.id}`, kind: "table", objectId: t.id, view, title: t.display_name });
  }

  function newTable(): void {
    let n = 1;
    while (tabs.some((t) => t.key === `table:new:${n}`)) n++;
    openTab(ws, { key: `table:new:${n}`, kind: "table", objectId: null, view: "design", title: n === 1 ? "New table" : `New table ${n}` });
  }

  function newScript(): void {
    let n = 1;
    while (tabs.some((t) => t.key === `script:new:${n}`)) n++;
    openTab(ws, { key: `script:new:${n}`, kind: "script", objectId: null, view: "design", title: n === 1 ? "New script" : `New script ${n}` });
  }

  function newJob(): void {
    let n = 1;
    while (tabs.some((t) => t.key === `job:new:${n}`)) n++;
    openTab(ws, { key: `job:new:${n}`, kind: "job", objectId: null, view: "design", title: n === 1 ? "New job" : `New job ${n}` });
  }

  function newForm(): void {
    let n = 1;
    while (tabs.some((t) => t.key === `form:new:${n}`)) n++;
    openTab(ws, { key: `form:new:${n}`, kind: "form", objectId: null, view: "design", title: n === 1 ? "New form" : `New form ${n}` });
  }
  function newReport(): void {
    let n = 1;
    while (tabs.some((t) => t.key === `report:new:${n}`)) n++;
    openTab(ws, { key: `report:new:${n}`, kind: "report", objectId: null, view: "design", title: n === 1 ? "New report" : `New report ${n}` });
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
    <nav aria-label="Navigation pane" data-ws={ws} data-role={role} className={variant === "drawer" ? "flex min-h-0 flex-1 flex-col bg-surface" : "flex w-64 shrink-0 flex-col border-r border-border bg-surface"}>
      <div className="p-2.5 pb-1.5">
        <label htmlFor={searchId} className="sr-only">
          Search objects
        </label>
        <div className="relative">
          <Search size={14} aria-hidden="true" className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-subtle" />
          <input
            id={searchId}
            type="search"
            placeholder="Search objects"
            className={`${input} bg-surface-2 pl-8`}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape" && query !== "") {
                e.preventDefault();
                setQuery("");
              }
            }}
          />
        </div>
      </div>
      <div className="flex-1 space-y-1 overflow-y-auto pb-3 pt-1">
        {GROUPS.filter((g) => (g.id !== "scripts" && g.id !== "jobs") || design).map((g) => {
          const open = !collapsed.has(g.id);
          const panelId = `nav-group-${g.id}`;
          return (
            <section key={g.id} aria-label={g.label}>
              <div className="group/head mx-2 flex items-center gap-0.5 pr-0.5">
                <h2 className="m-0 min-w-0 flex-1">
                  <button
                    type="button"
                    aria-expanded={open}
                    aria-controls={panelId}
                    onClick={() => toggle(g.id)}
                    className="flex w-full cursor-pointer items-center gap-2 rounded-md px-1 py-1.5 text-left text-xs font-semibold uppercase tracking-wider text-fg-subtle transition-colors hover:text-fg"
                  >
                    <KindIcon kind={g.kind} size="sm" />
                    <span className="flex-1">{g.label}</span>
                    <ChevronDown size={14} aria-hidden="true" className={`transition-transform ${open ? "" : "-rotate-90"}`} />
                  </button>
                </h2>
                {g.id === "queries" && design && (
                  <GroupAction label="New query" onClick={() => newQuery()}>
                    <Plus size={15} aria-hidden="true" />
                  </GroupAction>
                )}
                {g.id === "queries" && design && (
                  <GroupAction label="SQL console" onClick={() => newQuery("sql")}>
                    <SquareTerminal size={14} aria-hidden="true" />
                  </GroupAction>
                )}
                {(g.id === "forms" || g.id === "reports") && design && (
                  <GroupAction label={g.id === "forms" ? "New form" : "New report"} onClick={g.id === "forms" ? newForm : newReport}>
                    <Plus size={15} aria-hidden="true" />
                  </GroupAction>
                )}
                {g.id === "jobs" && design && (
                  <GroupAction label="New job" onClick={newJob}>
                    <Plus size={15} aria-hidden="true" />
                  </GroupAction>
                )}
                {g.id === "tables" && design && (
                  <GroupAction label="Import…" onClick={() => setImporting({})}>
                    <Upload size={14} aria-hidden="true" />
                  </GroupAction>
                )}
                {(g.id === "tables" || g.id === "scripts") && design && (
                  <GroupAction label={g.id === "tables" ? "New table" : "New script"} onClick={g.id === "tables" ? newTable : newScript}>
                    <Plus size={15} aria-hidden="true" />
                  </GroupAction>
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
                            export: () => setExporting({ kind: "table", id: t.id, name: t.display_name }),
                            importInto: () => setImporting({ intoTableId: t.id }),
                          })}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className={navEmpty}>
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
              {open && g.id === "queries" && (
                <div id={panelId}>
                  {queries.length > 0 ? (
                    <ul className="m-0 p-0">
                      {queries.map((q) => (
                        <QueryNavItem
                          key={q.id}
                          query={q}
                          active={active === `query:${q.id}`}
                          onOpen={() => openQuery(q, "datasheet")}
                          actions={queryActions(design, {
                            open: () => openQuery(q, "datasheet"),
                            design: () => openQuery(q, "design"),
                            sql: () => openQuery(q, "sql"),
                            rename: () => setQueryDialog({ kind: "rename", query: q }),
                            remove: () => setQueryDialog({ kind: "delete", query: q }),
                            export: () =>
                              setExporting({ kind: "query", id: q.id, name: q.name, params: effectiveParams(q.parameters ?? [], q.sql_text ?? "") }),
                          })}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className={navEmpty}>
                      {savedQueries.isError
                        ? "Queries could not be loaded"
                        : savedQueries.isPending
                          ? "Loading queries…"
                          : searching
                            ? `No ${g.noun} match “${query.trim()}”`
                            : `No ${g.noun} yet`}
                    </p>
                  )}
                </div>
              )}
              {open && g.id === "scripts" && (
                <ScriptsGroupBody ws={ws} panelId={panelId} query={query} activeKey={active} />
              )}
              {open && g.id === "forms" && (
                <FormsGroupBody ws={ws} panelId={panelId} query={query} activeKey={active} design={design} />
              )}
              {open && g.id === "reports" && (
                <ReportsGroupBody ws={ws} panelId={panelId} query={query} activeKey={active} design={design} />
              )}
              {open && g.id === "jobs" && <JobsGroupBody ws={ws} panelId={panelId} query={query} activeKey={active} />}
              {open && g.id !== "tables" && g.id !== "queries" && g.id !== "scripts" && g.id !== "forms" && g.id !== "reports" && g.id !== "jobs" && (
                <p id={panelId} className={navEmpty}>
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
      {queryDialog?.kind === "rename" && (
        <RenameQueryDialog ws={ws} query={queryDialog.query} onClose={() => setQueryDialog(null)} />
      )}
      {queryDialog?.kind === "delete" && (
        <DeleteQueryDialog ws={ws} query={queryDialog.query} onClose={() => setQueryDialog(null)} />
      )}
      {importing && <ImportWizard ws={ws} {...(importing.intoTableId ? { intoTableId: importing.intoTableId } : {})} onClose={() => setImporting(null)} />}
      {exporting && <ExportDialog ws={ws} source={exporting} onClose={() => setExporting(null)} />}
    </nav>
  );
}

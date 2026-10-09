import { useId, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Clock,
  FileText,
  FormInput,
  Search,
  Table2,
  Terminal,
  type LucideIcon,
} from "lucide-react";
import { input } from "@/components/styles";

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

export function NavPane({ ws, role }: { ws: string; role: string | undefined }) {
  const searchId = useId();
  const [query, setQuery] = useState("");
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
              <h2 className="m-0 text-sm font-semibold">
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
              {open && (
                <p id={panelId} className="px-9 py-1 text-sm text-fg-muted">
                  {searching ? `No ${g.noun} match “${query.trim()}”` : `No ${g.noun} yet`}
                </p>
              )}
            </section>
          );
        })}
      </div>
    </nav>
  );
}

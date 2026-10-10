import { useState } from "react";
import { ContextMenu, DropdownMenu } from "radix-ui";
import { useQuery } from "@tanstack/react-query";
import { Ellipsis, Eye, Pencil, Ruler, Trash2, type LucideIcon } from "lucide-react";
import { useTabs } from "@/app/tabs";
import { queryClient } from "@/app/queryClient";
import { formKeys, listForms, type Form } from "@/features/forms/formsApi";
import { DeleteFormDialog, RenameFormDialog } from "./FormCrudDialogs";

export interface FormAction {
  id: string;
  label: string;
  icon: LucideIcon;
  run: () => void;
}

/** Forms matching the search text, sorted by name ignoring case. */
export function sortedForms(forms: readonly Form[] | null | undefined, query: string): Form[] {
  const q = query.trim().toLowerCase();
  return [...(forms ?? [])]
    .filter((f) => q === "" || f.name.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
}

/** The actions available to a role on one form: everyone opens it, builders also design, rename and delete. */
export function formActions(
  canDesign: boolean,
  handlers: { open: () => void; design: () => void; rename: () => void; remove: () => void },
): FormAction[] {
  const actions: FormAction[] = [{ id: "open", label: "Open", icon: Eye, run: handlers.open }];
  if (canDesign) {
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

export function FormNavItem({
  form,
  active,
  actions,
  onOpen,
}: {
  form: Form;
  active: boolean;
  actions: FormAction[];
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
              {form.name}
            </button>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger asChild>
                <button
                  type="button"
                  aria-label={`More actions for ${form.name}`}
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

type Dialog = { kind: "rename" | "delete"; form: Form };

/** Body of the Forms navigation group. */
export function FormsGroupBody({
  ws,
  panelId,
  query,
  activeKey,
  design,
}: {
  ws: string;
  panelId: string;
  query: string;
  activeKey: string | null;
  design: boolean;
}) {
  const openTab = useTabs((s) => s.open);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const forms = useQuery({ queryKey: formKeys.list(ws), queryFn: () => listForms(ws) }, queryClient);
  const shown = sortedForms(forms.data, query);
  const searching = query.trim() !== "";

  function open(f: Form, view: "form" | "design"): void {
    openTab(ws, { key: `form:${f.id}`, kind: "form", objectId: f.id, view, title: f.name });
  }

  return (
    <div id={panelId}>
      {shown.length > 0 ? (
        <ul className="m-0 p-0">
          {shown.map((f) => (
            <FormNavItem
              key={f.id}
              form={f}
              active={activeKey === `form:${f.id}`}
              onOpen={() => open(f, "form")}
              actions={formActions(design, {
                open: () => open(f, "form"),
                design: () => open(f, "design"),
                rename: () => setDialog({ kind: "rename", form: f }),
                remove: () => setDialog({ kind: "delete", form: f }),
              })}
            />
          ))}
        </ul>
      ) : (
        <p className="px-9 py-1 text-sm text-fg-muted">
          {forms.isError
            ? "Forms could not be loaded"
            : forms.isPending
              ? "Loading forms…"
              : searching
                ? `No forms match “${query.trim()}”`
                : "No forms yet"}
        </p>
      )}
      {dialog?.kind === "rename" && <RenameFormDialog ws={ws} form={dialog.form} onClose={() => setDialog(null)} />}
      {dialog?.kind === "delete" && <DeleteFormDialog ws={ws} form={dialog.form} onClose={() => setDialog(null)} />}
    </div>
  );
}

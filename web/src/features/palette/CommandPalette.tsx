import { useEffect, useId, useMemo, useState, type KeyboardEvent } from "react";
import { Dialog } from "radix-ui";
import { ChevronDown, CornerDownLeft, Search } from "lucide-react";
import { dialogOverlay, dialogPanel, kbd } from "@/components/styles";
import { KindIcon } from "@/components/objectKinds";
import { useReturnFocus } from "@/components/ui";
import { navRowActive } from "@/features/nav/navStyles";
import { ImportWizard } from "@/features/workspace/lazyFeatures";
import { useImportRequest, usePalette } from "./commands";
import { buildRows, moreLabel, moveActive, type GroupId, type PaletteItem, type Row } from "./match";
import { usePaletteItems } from "./usePaletteItems";

/** Mod+K command palette. Mount once near the app root. */
export function CommandPalette() {
  const open = usePalette((s) => s.open);
  const setOpen = usePalette((s) => s.setOpen);
  const importWs = useImportRequest((s) => s.ws);
  const requestImport = useImportRequest((s) => s.request);
  const returnFocus = useReturnFocus(undefined, true);
  return (
    <>
      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className={dialogOverlay} />
          <Dialog.Content
            aria-describedby={undefined}
            data-presentation="top"
            {...returnFocus}
            className={`${dialogPanel} !top-[14vh] compact:!top-0 !-translate-y-0 w-[min(36rem,calc(100vw-2rem))] overflow-hidden`}
          >
            <Palette onDone={() => setOpen(false)} />
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
      {importWs && <ImportWizard ws={importWs} onClose={() => requestImport(null)} />}
    </>
  );
}

type Option = Exclude<Row, { type: "heading" }>;

function Palette({ onDone }: { onDone: () => void }) {
  const id = useId();
  const listId = `${id}-list`;
  const items = usePaletteItems();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [expanded, setExpanded] = useState<ReadonlySet<GroupId>>(new Set());
  const { rows, options, total } = useMemo(() => buildRows(items, query, expanded), [items, query, expanded]);
  const at = Math.min(active, Math.max(options.length - 1, 0));
  const current = options[at];
  const optionId = (o: Option): string => (o.type === "item" ? `${id}-opt-${o.item.id}` : `${id}-more-${o.group}`);

  useEffect(() => {
    if (current) document.getElementById(optionId(current))?.scrollIntoView?.({ block: "nearest" });
    // optionId depends only on id and the option.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current, id]);

  function choose(o: Option | undefined) {
    if (!o) return;
    if (o.type === "more") {
      setExpanded((prev) => new Set(prev).add(o.group));
      return;
    }
    onDone();
    // After the dialog has closed, so the command can move focus (for example into the docs panel).
    setTimeout(o.item.run, 0);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    const el = e.currentTarget;
    const move = (key: Parameters<typeof moveActive>[2]) => {
      e.preventDefault();
      setActive(moveActive(at, options.length, key));
    };
    if (e.key === "ArrowDown") move("next");
    else if (e.key === "ArrowUp") move("prev");
    else if (e.key === "PageDown") move("pageNext");
    else if (e.key === "PageUp") move("pagePrev");
    // Home and End move the text cursor first; once it is at the edge they jump through the list.
    else if (e.key === "Home" && el.selectionStart === 0 && el.selectionEnd === 0) move("first");
    else if (e.key === "End" && el.selectionStart === el.value.length && el.selectionEnd === el.value.length) move("last");
    else if (e.key === "Enter") {
      e.preventDefault();
      choose(current);
    }
  }

  return (
    <>
      <Dialog.Title className="sr-only">Command palette</Dialog.Title>
      <div className="flex items-center gap-2 border-b border-border px-4">
        <Search size={16} aria-hidden="true" className="shrink-0 text-fg-subtle" />
        <label htmlFor={`${id}-input`} className="sr-only">
          Type a command
        </label>
        <input
          id={`${id}-input`}
          role="combobox"
          aria-expanded="true"
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={current ? optionId(current) : undefined}
          autoComplete="off"
          placeholder="Type a command"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
            setExpanded(new Set());
          }}
          onKeyDown={onKeyDown}
          className="h-12 w-full bg-transparent text-sm text-fg placeholder:text-fg-subtle focus:outline-none"
        />
      </div>
      <ul id={listId} role="listbox" aria-label="Commands" className="m-0 max-h-[min(24rem,55dvh)] list-none overflow-y-auto p-1.5">
        {rows.map((row) => {
          if (row.type === "heading") {
            return (
              <li key={`h:${row.group}`} role="presentation" className="px-2.5 pb-1 pt-2 text-xs font-semibold uppercase tracking-wider text-fg-subtle first:pt-1">
                {row.label}
              </li>
            );
          }
          const on = row.index === at;
          return (
            <li
              key={optionId(row)}
              id={optionId(row)}
              role="option"
              aria-selected={on}
              onClick={() => choose(row)}
              onMouseMove={() => {
                if (!on) setActive(row.index);
              }}
              className={`flex cursor-pointer items-center gap-3 relative rounded-md px-2.5 py-2 text-sm text-fg transition-colors ${on ? navRowActive : ""}`}
            >
              {row.type === "item" ? <ItemBody item={row.item} on={on} /> : <MoreBody group={row.group} hidden={row.hidden} />}
              {on && <CornerDownLeft size={14} aria-hidden="true" className="shrink-0 text-fg-subtle" />}
            </li>
          );
        })}
      </ul>
      <div className="flex items-center justify-between gap-3 border-t border-border bg-surface-2/50 px-4 py-2 text-xs text-fg-muted">
        <p role="status" aria-live="polite" className="m-0">
          {total === 0 ? "No matching results" : `${total} ${total === 1 ? "result" : "results"}`}
        </p>
        <p className="m-0 hidden items-center gap-1.5 sm:flex" aria-hidden="true">
          <kbd className={kbd}>↑</kbd>
          <kbd className={kbd}>↓</kbd> navigate
          <kbd className={kbd}>↵</kbd> run
          <kbd className={kbd}>Esc</kbd> close
        </p>
      </div>
    </>
  );
}

function ItemBody({ item, on }: { item: PaletteItem; on: boolean }) {
  const Icon = item.icon;
  return (
    <>
      {item.kind ? (
        <KindIcon kind={item.kind} />
      ) : (
        Icon && (
          <span className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md ${on ? "bg-surface text-accent" : "bg-surface-3 text-fg-muted"}`}>
            <Icon size={14} aria-hidden="true" />
          </span>
        )
      )}
      <span className="min-w-0 flex-1 truncate flex items-center gap-2">
        <span className="truncate">{item.label}</span>
        {item.sublabel && (
          <span className="shrink-0 inline-flex items-center rounded bg-surface-3 px-1.5 py-0.5 text-xs font-normal text-fg-muted">
            {item.sublabel}
          </span>
        )}
        {item.kind && item.group !== "recent" && <span className="sr-only">, {item.kind}</span>}
      </span>
      {item.hint && <span className="shrink-0 text-xs text-fg-muted">{item.hint}</span>}
    </>
  );
}

function MoreBody({ group, hidden }: { group: GroupId; hidden: number }) {
  return (
    <>
      <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-surface-3 text-fg-muted">
        <ChevronDown size={14} aria-hidden="true" />
      </span>
      <span className="min-w-0 flex-1 text-fg-muted">{moreLabel(group, hidden)}</span>
    </>
  );
}

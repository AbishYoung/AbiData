import { useId, useMemo, useState, type KeyboardEvent } from "react";
import { Dialog } from "radix-ui";
import { CornerDownLeft, Search } from "lucide-react";
import { dialogOverlay, dialogPanel, kbd } from "@/components/styles";
import {
  baseCommands,
  filterCommands,
  usePalette,
  type Command,
} from "./commands";

/** Mod+K command palette. Mount once near the app root. */
export function CommandPalette() {
  const open = usePalette((s) => s.open);
  const setOpen = usePalette((s) => s.setOpen);
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Portal>
        <Dialog.Overlay className={dialogOverlay} />
        <Dialog.Content
          aria-describedby={undefined}
          className={`${dialogPanel} !top-[14vh] !-translate-y-0 w-[min(36rem,calc(100vw-2rem))] overflow-hidden`}
        >
          <Palette onDone={() => setOpen(false)} />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function Palette({ onDone }: { onDone: () => void }) {
  const id = useId();
  const listId = `${id}-list`;
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const commands = useMemo(
    () => filterCommands(baseCommands(), query),
    [query],
  );
  const at = Math.min(active, Math.max(commands.length - 1, 0));
  const current = commands[at];

  function run(c: Command | undefined) {
    if (!c) return;
    onDone();
    // After the dialog has closed, so the command can move focus (for example into the docs panel).
    setTimeout(c.run, 0);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(commands.length === 0 ? 0 : (at + 1) % commands.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(
        commands.length === 0
          ? 0
          : (at - 1 + commands.length) % commands.length,
      );
    } else if (e.key === "Enter") {
      e.preventDefault();
      run(current);
    }
  }

  return (
    <>
      <Dialog.Title className="sr-only">Command palette</Dialog.Title>
      <div className="flex items-center gap-2 border-b border-border px-4">
        <Search
          size={16}
          aria-hidden="true"
          className="shrink-0 text-fg-subtle"
        />
        <label htmlFor={`${id}-input`} className="sr-only">
          Type a command
        </label>
        <input
          id={`${id}-input`}
          role="combobox"
          aria-expanded="true"
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={
            current ? `${id}-opt-${current.id}` : undefined
          }
          autoComplete="off"
          placeholder="Type a command"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          className="h-12 w-full bg-transparent text-sm text-fg placeholder:text-fg-subtle focus:outline-none"
        />
      </div>
      <ul
        id={listId}
        role="listbox"
        aria-label="Commands"
        className="m-0 max-h-72 list-none overflow-y-auto p-1.5"
      >
        {commands.map((c, i) => {
          const Icon = c.icon;
          return (
            <li
              key={c.id}
              id={`${id}-opt-${c.id}`}
              role="option"
              aria-selected={i === at}
              onClick={() => run(c)}
              onMouseMove={() => setActive(i)}
              className={`flex cursor-pointer items-center gap-3 rounded-md px-2.5 py-2 text-sm transition-colors ${
                i === at ? "bg-accent-soft text-fg" : "text-fg"
              }`}
            >
              {Icon && (
                <span
                  className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md ${i === at ? "bg-surface text-accent" : "bg-surface-3 text-fg-muted"}`}
                >
                  <Icon size={14} aria-hidden="true" />
                </span>
              )}
              <span className="flex-1">{c.label}</span>
              {i === at && (
                <CornerDownLeft
                  size={14}
                  aria-hidden="true"
                  className="text-fg-subtle"
                />
              )}
            </li>
          );
        })}
      </ul>
      <div className="flex items-center justify-between gap-3 border-t border-border bg-surface-2/50 px-4 py-2 text-xs text-fg-muted">
        <p role="status" aria-live="polite" className="m-0">
          {commands.length === 0
            ? "No matching commands"
            : `${commands.length} ${commands.length === 1 ? "command" : "commands"}`}
        </p>
        <p
          className="m-0 hidden items-center gap-1.5 sm:flex"
          aria-hidden="true"
        >
          <kbd className={kbd}>↑</kbd>
          <kbd className={kbd}>↓</kbd> navigate
          <kbd className={kbd}>↵</kbd> run
          <kbd className={kbd}>Esc</kbd> close
        </p>
      </div>
    </>
  );
}

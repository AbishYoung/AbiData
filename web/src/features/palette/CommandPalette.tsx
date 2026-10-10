import { useId, useMemo, useState, type KeyboardEvent } from "react";
import { Dialog } from "radix-ui";
import { input } from "@/components/styles";
import { baseCommands, filterCommands, usePalette, type Command } from "./commands";

/** Mod+K command palette. Mount once near the app root. */
export function CommandPalette() {
  const open = usePalette((s) => s.open);
  const setOpen = usePalette((s) => s.setOpen);
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40" />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed left-1/2 top-[15vh] z-50 w-[min(34rem,calc(100vw-2rem))] -translate-x-1/2 rounded-lg border border-border bg-surface p-3 text-fg shadow-xl"
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
  const commands = useMemo(() => filterCommands(baseCommands(), query), [query]);
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
      setActive(commands.length === 0 ? 0 : (at - 1 + commands.length) % commands.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      run(current);
    }
  }

  return (
    <>
      <Dialog.Title className="m-0 mb-2 text-sm font-semibold">Command palette</Dialog.Title>
      <label htmlFor={`${id}-input`} className="sr-only">
        Type a command
      </label>
      <input
        id={`${id}-input`}
        role="combobox"
        aria-expanded="true"
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={current ? `${id}-opt-${current.id}` : undefined}
        autoComplete="off"
        placeholder="Type a command"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setActive(0);
        }}
        onKeyDown={onKeyDown}
        className={input}
      />
      <ul id={listId} role="listbox" aria-label="Commands" className="m-0 mt-2 max-h-72 list-none overflow-y-auto p-0">
        {commands.map((c, i) => (
          <li
            key={c.id}
            id={`${id}-opt-${c.id}`}
            role="option"
            aria-selected={i === at}
            onClick={() => run(c)}
            onMouseMove={() => setActive(i)}
            className={`cursor-pointer rounded px-2 py-1.5 text-sm ${i === at ? "bg-accent-soft" : ""}`}
          >
            {c.label}
          </li>
        ))}
      </ul>
      <p role="status" aria-live="polite" className="m-0 mt-1 text-xs text-fg-muted">
        {commands.length === 0 ? "No matching commands" : `${commands.length} ${commands.length === 1 ? "command" : "commands"}`}
      </p>
    </>
  );
}

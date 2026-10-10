import { useRef, useState, type KeyboardEvent } from "react";
import { CircleAlert, TriangleAlert } from "lucide-react";
import { problemsSummary, type Problem } from "./diagnostics";

interface Props {
  problems: readonly Problem[];
  /** Called with the 1-based position when an item is activated. */
  onJump: (line: number, column: number) => void;
  /** The last check could not reach the server. */
  failed?: boolean;
  checked?: boolean;
}

/** Non-modal list of diagnostics below the editor; arrow keys move between items, Enter/click jumps. */
export function ProblemsList({ problems, onJump, failed = false, checked = true }: Props) {
  const [active, setActive] = useState(0);
  const items = useRef<(HTMLButtonElement | null)[]>([]);
  const current = Math.min(active, Math.max(problems.length - 1, 0));

  function focusItem(i: number): void {
    const n = Math.max(0, Math.min(problems.length - 1, i));
    setActive(n);
    items.current[n]?.focus();
  }

  function onKeyDown(e: KeyboardEvent<HTMLUListElement>): void {
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        focusItem(current + 1);
        break;
      case "ArrowUp":
        e.preventDefault();
        focusItem(current - 1);
        break;
      case "Home":
        e.preventDefault();
        focusItem(0);
        break;
      case "End":
        e.preventDefault();
        focusItem(problems.length - 1);
        break;
    }
  }

  return (
    <section aria-label="Problems" className="border-t border-border bg-surface text-sm">
      <p role="status" className="m-0 px-3 py-1.5 text-fg-muted">
        {checked ? problemsSummary(problems) : "Checking…"}
        {failed && " (the last check could not reach the server)"}
      </p>
      {problems.length > 0 && (
        <ul aria-label="Problem list" className="m-0 max-h-40 list-none overflow-y-auto p-0" onKeyDown={onKeyDown}>
          {problems.map((p, i) => (
            <li key={`${p.line}:${p.column}:${i}`} className="m-0">
              <button
                type="button"
                ref={(el) => {
                  items.current[i] = el;
                }}
                tabIndex={i === current ? 0 : -1}
                onFocus={() => setActive(i)}
                onClick={() => onJump(p.line, p.column)}
                className="flex w-full cursor-pointer items-start gap-2 px-3 py-1 text-left hover:bg-surface-2"
              >
                {p.severity === "error" ? (
                  <CircleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0 text-danger" />
                ) : (
                  <TriangleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0 text-warning" />
                )}
                <span className="font-semibold">{p.severity === "error" ? "Error" : "Warning"}</span>
                <span className="text-fg-muted">
                  line {p.line}, column {p.column}
                </span>
                <span className="min-w-0 flex-1">{p.message}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

import { useEffect, useId, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronUp, Copy } from "lucide-react";
import { btn, input } from "@/components/styles";
import { findMatches, isTruncated, segments, stepMatch } from "./logSearch";

/** Monospace, wrapping log with copy and in-log search. */
export function LogViewer({ log, label = "Run log" }: { log: string; label?: string }) {
  const searchId = useId();
  const [query, setQuery] = useState("");
  const [current, setCurrent] = useState(0);
  const [copied, setCopied] = useState<"" | "copied" | "failed">("");
  const currentRef = useRef<HTMLElement>(null);

  const matches = useMemo(() => findMatches(log, query), [log, query]);
  const parts = useMemo(() => segments(log, matches), [log, matches]);
  const index = matches.length === 0 ? 0 : Math.min(current, matches.length - 1);

  useEffect(() => {
    currentRef.current?.scrollIntoView?.({ block: "nearest" });
  }, [index, matches]);

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(log);
      setCopied("copied");
    } catch {
      setCopied("failed");
    }
  }

  const status = query === "" ? "" : matches.length === 0 ? "No matches" : `${index + 1} of ${matches.length}`;

  return (
    <section aria-label={label} className="flex min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-2 pb-2">
        <label htmlFor={searchId} className="text-sm font-medium">
          Search log
        </label>
        <input
          id={searchId}
          type="search"
          className={`${input} !w-48`}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setCurrent(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              setCurrent(stepMatch(index, matches.length, e.shiftKey ? -1 : 1));
            }
          }}
        />
        <button
          type="button"
          className={btn}
          aria-label="Previous match"
          disabled={matches.length === 0}
          onClick={() => setCurrent(stepMatch(index, matches.length, -1))}
        >
          <ChevronUp size={14} aria-hidden="true" />
        </button>
        <button
          type="button"
          className={btn}
          aria-label="Next match"
          disabled={matches.length === 0}
          onClick={() => setCurrent(stepMatch(index, matches.length, 1))}
        >
          <ChevronDown size={14} aria-hidden="true" />
        </button>
        <span role="status" className="text-sm text-fg-muted">
          {status}
        </span>
        <button type="button" className={`${btn} ml-auto`} onClick={() => void copy()}>
          <Copy size={14} aria-hidden="true" />
          Copy log
        </button>
        <span role="status" className="text-sm text-fg-muted">
          {copied === "copied" ? "Copied" : copied === "failed" ? "Copy failed" : ""}
        </span>
      </div>
      {isTruncated(log) && (
        <p className="m-0 pb-1 text-sm text-warning">Only the most recent part of this run’s log is kept (the last 256 KiB).</p>
      )}
      <pre
        tabIndex={0}
        role="region"
        aria-label={`${label} text`}
        className="m-0 max-h-80 min-h-16 overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-surface-2 p-2 font-mono text-xs"
      >
        {log === ""
          ? "The run wrote nothing to the log."
          : parts.map((p, i) =>
              p.match === undefined ? (
                p.text
              ) : (
                <mark
                  key={i}
                  ref={p.match === index ? currentRef : undefined}
                  className={p.match === index ? "bg-accent text-accent-fg" : "bg-accent-soft text-fg underline"}
                >
                  {p.text}
                </mark>
              ),
            )}
      </pre>
    </section>
  );
}

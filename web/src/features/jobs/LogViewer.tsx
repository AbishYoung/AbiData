import { useEffect, useId, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronUp, Copy } from "lucide-react";
import { Tip } from "@/components/Tip";
import { btn, btnGhost, input } from "@/components/styles";
import { findMatches, isTruncated, stepMatch } from "./logSearch";
import { localizeLog, pieces, stampTitle, utcOffsetLabel } from "./logTime";

/**
 * Monospace, wrapping log with copy and in-log search. The server logs times in UTC; they are shown
 * in the viewer's local time like the run history, with the UTC instant on hover. Copying yields UTC ISO times.
 */
export function LogViewer({ log, label = "Run log", startedAt }: { log: string; label?: string; startedAt?: string }) {
  const searchId = useId();
  const [query, setQuery] = useState("");
  const [current, setCurrent] = useState(0);
  const [copied, setCopied] = useState<"" | "copied" | "failed">("");
  const currentRef = useRef<HTMLElement>(null);

  const local = useMemo(() => localizeLog(log, startedAt), [log, startedAt]);
  const matches = useMemo(() => findMatches(local.text, query), [local.text, query]);
  const parts = useMemo(() => pieces(local.text, matches, local.stamps), [local.text, matches, local.stamps]);
  const zone = local.stamps[0] ? utcOffsetLabel(Date.parse(local.stamps[0].iso)) : "";
  const index = matches.length === 0 ? 0 : Math.min(current, matches.length - 1);

  useEffect(() => {
    currentRef.current?.scrollIntoView?.({ block: "nearest" });
  }, [index, matches]);

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(local.utcText);
      setCopied("copied");
    } catch {
      setCopied("failed");
    }
  }

  const status = query === "" ? "" : matches.length === 0 ? "No matches" : `${index + 1} of ${matches.length}`;

  return (
    <section aria-label={label} className="flex min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-1.5 pb-2">
        <label htmlFor={searchId} className="text-xs font-semibold uppercase tracking-wider text-fg-subtle">
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
        <Tip label="Previous match" shortcut="Shift+Enter">
          <button
            type="button"
            className={btnGhost}
            aria-label="Previous match"
            disabled={matches.length === 0}
            onClick={() => setCurrent(stepMatch(index, matches.length, -1))}
          >
            <ChevronUp size={14} aria-hidden="true" />
          </button>
        </Tip>
        <Tip label="Next match" shortcut="Enter">
          <button
            type="button"
            className={btnGhost}
            aria-label="Next match"
            disabled={matches.length === 0}
            onClick={() => setCurrent(stepMatch(index, matches.length, 1))}
          >
            <ChevronDown size={14} aria-hidden="true" />
          </button>
        </Tip>
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
      {zone !== "" && (
        <p className="m-0 pb-1 text-xs text-fg-muted">
          Times are in your local time ({zone}). Hover a time to see it in UTC. Copy log uses UTC.
        </p>
      )}
      <pre
        tabIndex={0}
        role="region"
        aria-label={`${label} text`}
        className="m-0 max-h-80 min-h-16 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-border bg-surface-2 p-3 font-mono text-xs leading-5 shadow-xs"
      >
        {log === ""
          ? "The run wrote nothing to the log."
          : parts.map((p, i) => {
              const content =
                p.iso !== undefined ? (
                  <time dateTime={p.iso} title={stampTitle(p.iso)}>
                    {p.text}
                  </time>
                ) : (
                  p.text
                );
              return p.match === undefined ? (
                <span key={i}>{content}</span>
              ) : (
                <mark
                  key={i}
                  ref={p.match === index ? currentRef : undefined}
                  className={p.match === index ? "bg-accent text-accent-fg" : "bg-accent-soft text-fg underline"}
                >
                  {content}
                </mark>
              );
            })}
      </pre>
    </section>
  );
}

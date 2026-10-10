import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDown, Copy, Pause, Play, Trash2 } from "lucide-react";
import { alertBox, btn, input } from "@/components/styles";
import { adminKeys, listWorkspaces } from "./adminApi";
import {
  MAX_LINES,
  NO_FILTER,
  appendLog,
  applyReset,
  clearBuffer,
  dismissGap,
  emptyBuffer,
  filterLines,
  formatLine,
  subsystemsOf,
  type LogBuffer,
  type LogFilter,
  type LogLine,
} from "./logBuffer";

type Conn = "connecting" | "live" | "reconnecting" | "closed";
const CONN_TEXT: Record<Conn, string> = {
  connecting: "Connecting to the log stream…",
  live: "Live",
  reconnecting: "Reconnecting…",
  closed: "The log stream is closed (you may not have access).",
};

const LEVEL_TONE: Record<LogLine["level"], string> = { debug: "text-fg-muted", info: "text-fg", warn: "text-warning", error: "text-danger" };

function clock(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.toLocaleTimeString(undefined, { hour12: false })}.${String(d.getMilliseconds()).padStart(3, "0")}`;
}

const Row = memo(function Row({ line, onCopy }: { line: LogLine; onCopy: (l: LogLine) => void }) {
  const extra = Object.entries(line.attrs).filter(([k]) => k !== "subsystem" && k !== "workspace_id");
  return (
    <div className="flex items-start gap-2 border-b border-border px-2 py-0.5 font-mono text-xs" data-level={line.level}>
      <time dateTime={line.time} className="shrink-0 text-fg-muted">
        {clock(line.time)}
      </time>
      <span className={`w-12 shrink-0 font-semibold uppercase ${LEVEL_TONE[line.level]}`}>{line.level}</span>
      {line.subsystem && <span className="shrink-0 text-fg-muted">[{line.subsystem}]</span>}
      <span className="min-w-0 flex-1 break-words">
        {line.msg}
        {extra.length > 0 && (
          <span className="text-fg-muted">
            {" "}
            {extra.map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`).join(" ")}
          </span>
        )}
      </span>
      <button type="button" className="shrink-0 rounded border border-border px-1 py-0.5 text-fg hover:bg-surface-2" onClick={() => onCopy(line)} aria-label={`Copy line: ${line.msg.slice(0, 60)}`}>
        <Copy size={12} aria-hidden="true" />
      </button>
    </div>
  );
});

export function LogsTab() {
  const [buf, setBuf] = useState<LogBuffer>(emptyBuffer);
  const [conn, setConn] = useState<Conn>("connecting");
  const [filter, setFilter] = useState<LogFilter>(NO_FILTER);
  const [frozen, setFrozen] = useState<{ lines: readonly LogLine[]; nextKey: number } | null>(null);
  const [copied, setCopied] = useState("");
  const [stuck, setStuck] = useState(true);
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  const workspaces = useQuery({ queryKey: adminKeys.workspaces, queryFn: listWorkspaces });

  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const es = new EventSource("/api/v1/admin/logs/stream");
    es.onopen = () => setConn("live");
    es.onerror = () => setConn(es.readyState === EventSource.CLOSED ? "closed" : "reconnecting");
    const onLog = (e: MessageEvent) => {
      setConn("live");
      setBuf((b) => appendLog(b, String(e.data)));
    };
    const onReset = () => setBuf((b) => applyReset(b));
    const onHello = () => setConn("live");
    es.addEventListener("log", onLog);
    es.addEventListener("reset", onReset);
    es.addEventListener("hello", onHello);
    return () => {
      es.removeEventListener("log", onLog);
      es.removeEventListener("reset", onReset);
      es.removeEventListener("hello", onHello);
      es.close();
    };
  }, []);

  const source = frozen ? frozen.lines : buf.lines;
  const shown = useMemo(() => filterLines(source, filter), [source, filter]);
  const subsystems = useMemo(() => subsystemsOf(buf.lines), [buf.lines]);
  const missed = frozen ? buf.nextKey - frozen.nextKey : 0;
  const lastKey = shown.length > 0 ? (shown[shown.length - 1]?.key ?? 0) : 0;

  // Keep the newest line in view unless the user scrolled up.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [lastKey, shown.length]);

  const onScroll = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    stick.current = atBottom;
    setStuck(atBottom);
  }, []);

  function jumpToLatest() {
    const el = scroller.current;
    stick.current = true;
    setStuck(true);
    if (el) el.scrollTop = el.scrollHeight;
  }

  const copy = useCallback((l: LogLine) => {
    const text = formatLine(l);
    const done = () => setCopied(`Copied: ${l.msg.slice(0, 60)}`);
    if (typeof navigator !== "undefined" && navigator.clipboard) {
      navigator.clipboard.writeText(text).then(done, () => setCopied("Copy failed. Select the text and copy it manually."));
    } else {
      setCopied("Copy is not available in this browser.");
    }
  }, []);

  const paused = frozen !== null;
  const setF = (patch: Partial<LogFilter>) => setFilter((f) => ({ ...f, ...patch }));
  const statusText = paused ? `Paused. ${missed} new ${missed === 1 ? "line" : "lines"} waiting.` : CONN_TEXT[conn];

  return (
    <section aria-label="Live logs" className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="flex flex-wrap items-end gap-3">
        <div>
          <label htmlFor="log-level" className="mb-1 block text-sm font-medium">
            Minimum level
          </label>
          <select id="log-level" className={input} value={filter.level} onChange={(e) => setF({ level: e.target.value as LogFilter["level"] })}>
            <option value="all">All levels</option>
            <option value="info">Info and above</option>
            <option value="warn">Warnings and errors</option>
            <option value="error">Errors only</option>
          </select>
        </div>
        <div>
          <label htmlFor="log-subsystem" className="mb-1 block text-sm font-medium">
            Subsystem
          </label>
          <select id="log-subsystem" className={input} value={filter.subsystem} onChange={(e) => setF({ subsystem: e.target.value })}>
            <option value="">All subsystems</option>
            {filter.subsystem && !subsystems.includes(filter.subsystem) && <option value={filter.subsystem}>{filter.subsystem}</option>}
            {subsystems.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label htmlFor="log-workspace" className="mb-1 block text-sm font-medium">
            Workspace
          </label>
          <select id="log-workspace" className={input} value={filter.workspace} onChange={(e) => setF({ workspace: e.target.value })}>
            <option value="">All workspaces</option>
            {(workspaces.data ?? []).map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </div>
        <button
          type="button"
          className={btn}
          aria-pressed={paused}
          onClick={() => setFrozen(paused ? null : { lines: buf.lines, nextKey: buf.nextKey })}
        >
          {paused ? <Play size={14} aria-hidden="true" /> : <Pause size={14} aria-hidden="true" />}
          {paused ? "Resume" : "Pause"}
        </button>
        <button type="button" className={btn} onClick={() => setBuf(clearBuffer)}>
          <Trash2 size={14} aria-hidden="true" />
          Clear
        </button>
        {!stuck && !paused && (
          <button type="button" className={btn} onClick={jumpToLatest}>
            <ArrowDown size={14} aria-hidden="true" />
            Jump to latest
          </button>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <span role="status" aria-live="polite" data-testid="log-status">
          {statusText}
        </span>
        <span className="text-fg-muted">
          Showing {shown.length.toLocaleString("en-US")} of {source.length.toLocaleString("en-US")} lines (the newest {MAX_LINES.toLocaleString("en-US")} are kept).
        </span>
        <span role="status" aria-live="polite" className="text-fg-muted">
          {copied}
        </span>
      </div>
      {buf.gap && (
        <div role="alert" className={`${alertBox} flex items-center gap-3`}>
          <span>The connection was interrupted for longer than the server remembers, so some lines are missing.</span>
          <button type="button" className={btn} onClick={() => setBuf(dismissGap)}>
            Dismiss
          </button>
        </div>
      )}
      <div
        ref={scroller}
        role="log"
        aria-label="Server log"
        aria-relevant="additions"
        tabIndex={0}
        onScroll={onScroll}
        className="min-h-64 flex-1 overflow-y-auto rounded-md border border-border bg-surface"
        style={{ maxHeight: "60vh" }}
      >
        {shown.length === 0 && <p className="m-0 p-3 text-sm text-fg-muted">No log lines match yet. New lines appear here as the server writes them.</p>}
        {shown.map((l) => (
          <Row key={l.key} line={l} onCopy={copy} />
        ))}
      </div>
    </section>
  );
}

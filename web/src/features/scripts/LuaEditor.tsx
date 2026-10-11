import { lazy, Suspense, useCallback, useId, useRef, useState } from "react";
import { useDiagnostics } from "./diagnostics";
import type { EditorHandle } from "./MonacoEditor";
import { ProblemsList } from "./ProblemsList";
import type { ScriptKind } from "./luaApi";

// Monaco (editor, Lua grammar, worker) lives in its own chunk, fetched on first use.
const MonacoEditor = lazy(() => import("./MonacoEditor"));

export interface LuaEditorProps {
  value: string;
  onChange: (value: string) => void;
  kind: ScriptKind;
  ws: string;
  /** Accessible name of the editor. */
  label: string;
  /** CSS height of the editing surface (default 24rem). */
  height?: string;
}

/** Character offset of a 1-based line/column in text (clamped to the text). */
export function offsetOf(text: string, line: number, column: number): number {
  const lines = text.split("\n");
  let off = 0;
  for (let i = 0; i < Math.min(line - 1, lines.length); i++) off += (lines[i]?.length ?? 0) + 1;
  const lineLen = lines[Math.min(line, lines.length) - 1]?.length ?? 0;
  return Math.min(text.length, off + Math.min(Math.max(column - 1, 0), lineLen));
}

/**
 * Lua editor with live server diagnostics, completions and hover from the Lua API manifest,
 * and a plain-text alternative for people who prefer a native text area.
 */
export function LuaEditor({ value, onChange, kind, ws, label, height = "24rem" }: LuaEditorProps) {
  const uid = useId().replace(/[^A-Za-z0-9]/g, "");
  const toggleId = `${uid}-plain`;
  const hintId = `${uid}-hint`;
  const [plain, setPlain] = useState(false);
  const handle = useRef<EditorHandle | null>(null);
  const textarea = useRef<HTMLTextAreaElement | null>(null);
  const diagnostics = useDiagnostics(ws, kind, value);

  const setHandle = useCallback((h: EditorHandle | null) => {
    handle.current = h;
  }, []);

  function jump(line: number, column: number): void {
    if (plain) {
      const ta = textarea.current;
      if (!ta) return;
      const off = offsetOf(value, line, column);
      ta.focus();
      ta.setSelectionRange(off, off);
    } else {
      handle.current?.reveal(line, column);
    }
  }

  return (
    <div className="flex min-h-0 flex-col" data-testid="lua-editor">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border bg-surface-2/50 px-3 py-1.5 text-sm">
        <label htmlFor={toggleId} className="inline-flex cursor-pointer items-center gap-2">
          <input id={toggleId} type="checkbox" checked={plain} onChange={(e) => setPlain(e.target.checked)} />
          Plain text editor
        </label>
        <span id={hintId} className="text-fg-muted">
          {plain ? "Native text area." : "Tab indents; Ctrl+M switches Tab to move focus."}
        </span>
      </div>
      {plain ? (
        <textarea
          ref={textarea}
          aria-label={label}
          aria-describedby={hintId}
          spellCheck={false}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          style={{ height }}
          className="w-full resize-y border-0 bg-surface p-3 font-mono text-sm text-fg"
        />
      ) : (
        <Suspense fallback={<div role="status" className="space-y-2 p-4"><span className="sr-only">Loading the code editor…</span>{[80, 60, 70, 40].map((w) => <div key={w} aria-hidden="true" className="h-3 animate-pulse rounded bg-surface-3" style={{ width: `${w}%` }} />)}</div>}>
          <MonacoEditor
            value={value}
            onChange={onChange}
            kind={kind}
            label={label}
            height={height}
            path={`inmemory://abidata/${uid}.lua`}
            problems={diagnostics.problems}
            onReady={setHandle}
          />
        </Suspense>
      )}
      <ProblemsList problems={diagnostics.problems} failed={diagnostics.failed} checked={diagnostics.checked} onJump={jump} />
    </div>
  );
}

import { useEffect, useRef, useState } from "react";
import { checkScript, type Marker } from "./scriptsApi";
import type { ScriptKind } from "./luaApi";

export const CHECK_DEBOUNCE_MS = 500;

export interface Problem {
  severity: "error" | "warning";
  message: string;
  /** 1-based. */
  line: number;
  column: number;
  endColumn: number;
}

export interface EditorMarker {
  severity: "error" | "warning";
  message: string;
  startLineNumber: number;
  startColumn: number;
  endLineNumber: number;
  endColumn: number;
}

/** Server markers use Monaco semantics (1-based); only degenerate ranges are widened. */
export function toProblems(markers: readonly Marker[]): Problem[] {
  return markers.map((m) => {
    const line = Math.max(1, m.line);
    const column = Math.max(1, m.column);
    return { severity: m.severity, message: m.message, line, column, endColumn: Math.max(m.end_column, column + 1) };
  });
}

export function toEditorMarkers(problems: readonly Problem[]): EditorMarker[] {
  return problems.map((p) => ({
    severity: p.severity,
    message: p.message,
    startLineNumber: p.line,
    startColumn: p.column,
    endLineNumber: p.line,
    endColumn: p.endColumn,
  }));
}

export function problemsSummary(problems: readonly Problem[]): string {
  if (problems.length === 0) return "No problems";
  return problems.length === 1 ? "1 problem" : `${problems.length} problems`;
}

export interface Debounced<A extends unknown[]> {
  call: (...args: A) => void;
  cancel: () => void;
}

/** Trailing-edge debounce; each call restarts the timer. */
export function debounce<A extends unknown[]>(fn: (...args: A) => void, ms: number): Debounced<A> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return {
    call: (...args) => {
      if (timer !== undefined) clearTimeout(timer);
      timer = setTimeout(() => {
        timer = undefined;
        fn(...args);
      }, ms);
    },
    cancel: () => {
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
    },
  };
}

export interface Diagnostics {
  problems: Problem[];
  /** True once a check has completed for some version of the text. */
  checked: boolean;
  /** The last check could not reach the server. */
  failed: boolean;
}

/**
 * Checks the source on the server 500 ms after the last edit. Never blocks typing:
 * results of superseded requests are discarded and failures only set `failed`.
 */
export function useDiagnostics(ws: string, kind: ScriptKind, source: string, enabled = true): Diagnostics {
  const [state, setState] = useState<Diagnostics>({ problems: [], checked: false, failed: false });
  const latest = useRef(0);

  useEffect(() => {
    if (!enabled) return;
    const id = ++latest.current;
    const controller = new AbortController();
    const run = debounce(() => {
      checkScript(ws, kind, source, controller.signal).then(
        (markers) => {
          if (latest.current === id) setState({ problems: toProblems(markers), checked: true, failed: false });
        },
        () => {
          if (latest.current === id && !controller.signal.aborted) setState((s) => ({ ...s, failed: true }));
        },
      );
    }, CHECK_DEBOUNCE_MS);
    run.call();
    return () => {
      run.cancel();
      controller.abort();
    };
  }, [ws, kind, source, enabled]);

  return state;
}

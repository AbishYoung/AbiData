import { useCallback, useEffect, useRef, useState } from "react";
import Editor from "@monaco-editor/react";
import { flushSync } from "react-dom";
import { applyTheme, currentThemeName, monaco, type ThemeName } from "@/features/scripts/monacoSetup";
import type { ParsedSqlError } from "./sqlErrorParser";

export interface MonacoSqlEditorHandle {
  focus: () => void;
  revealPosition: (lineNumber: number, column: number, endLineNumber?: number, endColumn?: number) => void;
}

export interface MonacoSqlEditorProps {
  value: string;
  onChange: (value: string) => void;
  onRun?: () => void;
  error?: ParsedSqlError | null;
  path?: string;
  height?: string;
  readOnly?: boolean;
  onReady?: (handle: MonacoSqlEditorHandle | null) => void;
}

const MARKER_OWNER = "sql-validator";

export default function MonacoSqlEditor({
  value,
  onChange,
  onRun,
  error,
  path = "query-console.sql",
  height = "100%",
  readOnly = false,
  onReady,
}: MonacoSqlEditorProps) {
  const [editor, setEditor] = useState<monaco.editor.IStandaloneCodeEditor | null>(null);
  const [theme, setTheme] = useState<ThemeName>(currentThemeName);
  const onReadyRef = useRef(onReady);
  const onRunRef = useRef(onRun);

  useEffect(() => {
    onReadyRef.current = onReady;
  }, [onReady]);

  useEffect(() => {
    onRunRef.current = onRun;
  }, [onRun]);

  // Sync theme with system / document data-theme
  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(applyTheme()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const media =
      typeof window !== "undefined" && typeof window.matchMedia === "function"
        ? window.matchMedia("(prefers-color-scheme: dark)")
        : null;
    const onMedia = () => setTheme(applyTheme());
    media?.addEventListener("change", onMedia);
    return () => {
      observer.disconnect();
      media?.removeEventListener("change", onMedia);
    };
  }, []);

  // Update error markers in Monaco
  useEffect(() => {
    const model = editor?.getModel();
    if (!model) return;

    if (!error) {
      monaco.editor.setModelMarkers(model, MARKER_OWNER, []);
      return;
    }

    monaco.editor.setModelMarkers(model, MARKER_OWNER, [
      {
        severity: monaco.MarkerSeverity.Error,
        message: error.message,
        startLineNumber: error.lineNumber,
        startColumn: error.column,
        endLineNumber: error.endLineNumber,
        endColumn: error.endColumn,
      },
    ]);
  }, [editor, error]);

  // Expose focus and revealPosition handle
  useEffect(() => {
    if (!editor) return;
    onReadyRef.current?.({
      focus: () => editor.focus(),
      revealPosition: (line: number, col: number, endLine?: number, endCol?: number) => {
        if (endLine !== undefined && endCol !== undefined) {
          editor.setSelection({
            startLineNumber: line,
            startColumn: col,
            endLineNumber: endLine,
            endColumn: endCol,
          });
        } else {
          editor.setPosition({ lineNumber: line, column: col });
        }
        editor.revealPositionInCenterIfOutsideViewport({ lineNumber: line, column: col });
        editor.focus();
      },
    });
    return () => onReadyRef.current?.(null);
  }, [editor]);

  const handleMount = useCallback((ed: monaco.editor.IStandaloneCodeEditor) => {
    setEditor(ed);
    ed.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => {
      onRunRef.current?.();
    });
  }, []);

  return (
    <div
      role="region"
      aria-label="SQL query editor"
      className="relative flex min-h-0 flex-1 w-full flex-col overflow-hidden rounded-lg border border-border-strong/70 bg-surface shadow-xs"
      data-testid="monaco-sql-editor"
    >
      <Editor
        height={height}
        language="sql"
        path={path}
        value={value}
        theme={theme}
        beforeMount={() => {
          applyTheme();
        }}
        onChange={(v) => {
          // Clear error markers when the query is edited
          const model = editor?.getModel();
          if (model) {
            monaco.editor.setModelMarkers(model, MARKER_OWNER, []);
          }
          flushSync(() => onChange(v ?? ""));
        }}
        onMount={handleMount}
        loading={
          <div role="status" className="space-y-2 p-4">
            <span className="sr-only">Loading SQL editor…</span>
            {[80, 60, 70, 40].map((w) => (
              <div key={w} aria-hidden="true" className="h-3 animate-pulse rounded bg-surface-3" style={{ width: `${w}%` }} />
            ))}
          </div>
        }
        options={{
          ariaLabel: "SQL query editor",
          automaticLayout: true,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          fontSize: 13,
          lineHeight: 20,
          fontFamily: "JetBrains Mono, Menlo, Monaco, Consolas, monospace",
          lineNumbers: "on",
          tabSize: 2,
          insertSpaces: true,
          wordBasedSuggestions: "off",
          quickSuggestions: { other: true, comments: false, strings: false },
          renderWhitespace: "none",
          accessibilitySupport: "auto",
          fixedOverflowWidgets: true,
          readOnly,
        }}
      />
    </div>
  );
}

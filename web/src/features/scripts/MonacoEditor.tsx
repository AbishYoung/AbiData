import { useEffect, useRef, useState } from "react";
import Editor from "@monaco-editor/react";
import { flushSync } from "react-dom";
import { applyTheme, currentThemeName, monaco, registerModelKind, type ThemeName } from "./monacoSetup";
import type { Problem } from "./diagnostics";
import type { ScriptKind } from "./luaApi";

export interface EditorHandle {
  focus: () => void;
  reveal: (line: number, column: number) => void;
}

interface Props {
  value: string;
  onChange: (value: string) => void;
  kind: ScriptKind;
  label: string;
  height: string;
  /** Unique model path per open editor. */
  path: string;
  problems: readonly Problem[];
  onReady: (handle: EditorHandle | null) => void;
}

const MARKER_OWNER = "abidata-check";

function severity(s: Problem["severity"]): monaco.MarkerSeverity {
  return s === "error" ? monaco.MarkerSeverity.Error : monaco.MarkerSeverity.Warning;
}

/** The Monaco Lua editor. Imported lazily by LuaEditor so Monaco stays in its own chunk. */
export default function MonacoEditor({ value, onChange, kind, label, height, path, problems, onReady }: Props) {
  const [editor, setEditor] = useState<monaco.editor.IStandaloneCodeEditor | null>(null);
  const [theme, setTheme] = useState<ThemeName>(currentThemeName);
  const onReadyRef = useRef(onReady);
  useEffect(() => {
    onReadyRef.current = onReady;
  }, [onReady]);

  // Follow the app theme (data-theme attribute or the OS preference).
  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(applyTheme()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onMedia = () => setTheme(applyTheme());
    media.addEventListener("change", onMedia);
    return () => {
      observer.disconnect();
      media.removeEventListener("change", onMedia);
    };
  }, []);

  // Providers filter by script kind through the model's URI.
  useEffect(() => {
    const model = editor?.getModel();
    if (!model) return;
    return registerModelKind(model.uri, kind);
  }, [editor, kind]);

  useEffect(() => {
    const model = editor?.getModel();
    if (!model) return;
    monaco.editor.setModelMarkers(
      model,
      MARKER_OWNER,
      problems.map((p) => ({
        severity: severity(p.severity),
        message: p.message,
        startLineNumber: p.line,
        startColumn: p.column,
        endLineNumber: p.line,
        endColumn: p.endColumn,
      })),
    );
  }, [editor, problems]);

  useEffect(() => {
    if (!editor) return;
    onReadyRef.current({
      focus: () => editor.focus(),
      reveal: (line, column) => {
        editor.setPosition({ lineNumber: line, column });
        editor.revealLineInCenterIfOutsideViewport(line);
        editor.focus();
      },
    });
    return () => onReadyRef.current(null);
  }, [editor]);

  return (
    <Editor
      height={height}
      language="lua"
      path={path}
      value={value}
      theme={theme}
      beforeMount={() => {
        applyTheme();
      }}
      // Render synchronously so the controlled value never lags behind fast typing
      // (a stale value prop would make the wrapper overwrite the newest keystrokes).
      onChange={(v) => flushSync(() => onChange(v ?? ""))}
      onMount={(ed) => setEditor(ed)}
      loading={<div role="status" className="space-y-2 p-4"><span className="sr-only">Loading the code editor…</span>{[80, 60, 70, 40].map((w) => <div key={w} aria-hidden="true" className="h-3 animate-shimmer rounded bg-surface-3" style={{ width: `${w}%` }} />)}</div>}
      options={{
        ariaLabel: label,
        automaticLayout: true,
        minimap: { enabled: false },
        scrollBeyondLastLine: false,
        fontSize: 14,
        tabSize: 2,
        insertSpaces: true,
        wordBasedSuggestions: "off",
        quickSuggestions: { other: true, comments: false, strings: false },
        renderWhitespace: "none",
        accessibilitySupport: "auto",
        fixedOverflowWidgets: true,
      }}
    />
  );
}

/* eslint-disable no-restricted-syntax -- documented exception (DESIGN.md): Monaco takes hex colours; these are fallbacks for when the tokens cannot be read. */
// Loaded only from the lazily imported MonacoEditor chunk. Monaco is bundled locally (no CDN: the
// CSP is script-src 'self'; worker-src 'self' blob:); only the editor worker and the Lua grammar ship.
import * as monaco from "monaco-editor/editor/editor.api";
import "monaco-editor/editor/browser/coreCommands";
import "monaco-editor/editor/browser/widget/codeEditor/codeEditorWidget";
import "monaco-editor/editor/contrib/bracketMatching/browser/bracketMatching";
import "monaco-editor/editor/contrib/clipboard/browser/clipboard";
import "monaco-editor/editor/contrib/comment/browser/comment";
import "monaco-editor/editor/contrib/contextmenu/browser/contextmenu";
import "monaco-editor/editor/contrib/cursorUndo/browser/cursorUndo";
import "monaco-editor/editor/contrib/find/browser/findController";
import "monaco-editor/editor/contrib/folding/browser/folding";
import "monaco-editor/editor/contrib/gotoError/browser/gotoError";
import "monaco-editor/editor/contrib/hover/browser/hoverContribution";
import "monaco-editor/editor/contrib/indentation/browser/indentation";
import "monaco-editor/editor/contrib/linesOperations/browser/linesOperations";
import "monaco-editor/editor/contrib/multicursor/browser/multicursor";
import "monaco-editor/editor/contrib/parameterHints/browser/parameterHints";
import "monaco-editor/editor/contrib/smartSelect/browser/smartSelect";
import "monaco-editor/editor/contrib/snippet/browser/snippetController2";
import "monaco-editor/editor/contrib/suggest/browser/suggestController";
import "monaco-editor/editor/contrib/toggleTabFocusMode/browser/toggleTabFocusMode";
import "monaco-editor/editor/contrib/wordHighlighter/browser/wordHighlighter";
import "monaco-editor/editor/contrib/wordOperations/browser/wordOperations";
import "monaco-editor/languages/definitions/lua/register";
import "monaco-editor/languages/definitions/sql/register";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import { loader } from "@monaco-editor/react";
import { openDocs } from "@/features/docs/docsStore";
import { parseColor, toHex } from "@/lib/color";
import {
  hoverFor,
  manifest,
  signatureFor,
  suggestionsFor,
  type ScriptKind,
  type SuggestKind,
} from "./luaApi";

const OPEN_DOCS = "abidata.openDocs";

/** A hover link that opens the docs panel at `target` (a trusted command link). */
function docsLink(target: string): string {
  return `[Open in the docs](command:${OPEN_DOCS}?${encodeURIComponent(JSON.stringify([target]))})`;
}

monaco.editor.registerCommand(OPEN_DOCS, (_accessor, target: unknown) => {
  if (typeof target === "string") openDocs(target);
});

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };
loader.config({ monaco });

/** Script kind per model URI, so the shared providers can filter by context. */
const modelKinds = new Map<string, ScriptKind>();

export function registerModelKind(uri: monaco.Uri, kind: ScriptKind): () => void {
  const key = uri.toString();
  modelKinds.set(key, kind);
  return () => {
    modelKinds.delete(key);
  };
}

function itemKind(kind: SuggestKind): monaco.languages.CompletionItemKind {
  const k = monaco.languages.CompletionItemKind;
  switch (kind) {
    case "module":
      return k.Module;
    case "function":
      return k.Function;
    case "value":
      return k.Field;
    case "keyword":
      return k.Keyword;
    case "global":
      return k.Variable;
    case "library":
      return k.Class;
  }
}

function linePrefix(model: monaco.editor.ITextModel, position: monaco.Position): string {
  return model.getValueInRange({
    startLineNumber: position.lineNumber,
    startColumn: 1,
    endLineNumber: position.lineNumber,
    endColumn: position.column,
  });
}

let providersRegistered = false;

function registerProviders(): void {
  if (providersRegistered) return;
  providersRegistered = true;
  const lang = monaco.languages;

  lang.registerCompletionItemProvider("lua", {
    triggerCharacters: ["."],
    provideCompletionItems(model, position) {
      const kind = modelKinds.get(model.uri.toString());
      if (!kind) return { suggestions: [] };
      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };
      const suggestions = suggestionsFor(manifest, kind, linePrefix(model, position)).map((s, i) => ({
          label: s.label,
          kind: itemKind(s.kind),
          insertText: s.insertText,
          detail: s.detail,
          documentation: s.documentation ? { value: s.documentation } : undefined,
          sortText: String(i).padStart(4, "0"),
          range,
        }));
      return { suggestions };
    },
  });

  lang.registerHoverProvider("lua", {
    provideHover(model, position) {
      const kind = modelKinds.get(model.uri.toString());
      if (!kind) return null;
      const info = hoverFor(manifest, kind, model.getLineContent(position.lineNumber), position.column);
      if (!info) return null;
      return {
        range: new monaco.Range(position.lineNumber, info.startColumn, position.lineNumber, info.endColumn),
        contents: [
          { value: info.markdown },
          { value: docsLink(info.docsTarget), isTrusted: true },
        ],
      };
    },
  });

  lang.registerSignatureHelpProvider("lua", {
    signatureHelpTriggerCharacters: ["(", ","],
    provideSignatureHelp(model, position) {
      const kind = modelKinds.get(model.uri.toString());
      if (!kind) return null;
      const sig = signatureFor(manifest, kind, linePrefix(model, position));
      if (!sig) return null;
      return {
        value: {
          signatures: [
            {
              label: sig.label,
              documentation: { value: sig.documentation },
              parameters: sig.parameters.map((p) => ({ label: p.label, documentation: { value: p.documentation } })),
              activeParameter: sig.activeParameter,
            },
          ],
          activeSignature: 0,
          activeParameter: sig.activeParameter,
        },
        dispose: () => undefined,
      };
    },
  });
}

registerProviders();

export type ThemeName = "abidata-light" | "abidata-dark";

/** A token as #rrggbb (Monaco takes only hex; the tokens are oklch). */
function cssVar(name: string, fallback: string): string {
  const c = parseColor(getComputedStyle(document.documentElement).getPropertyValue(name).trim());
  return c ? toHex(c) : fallback;
}

/** The theme the app is currently showing: data-theme override, else the OS preference. */
export function currentThemeName(): ThemeName {
  const forced = document.documentElement.getAttribute("data-theme");
  if (forced === "dark") return "abidata-dark";
  if (forced === "light") return "abidata-light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "abidata-dark" : "abidata-light";
}

/** (Re)defines both themes from the CSS variables' current colors and applies the active one. */
export function applyTheme(): ThemeName {
  const name = currentThemeName();
  const dark = name === "abidata-dark";
  const strip = (hex: string) => hex.replace("#", "");
  const fg = cssVar("--fg", dark ? "#e2e8f0" : "#0f172a");
  const muted = cssVar("--fg-muted", dark ? "#94a3b8" : "#475569");
  const accent = cssVar("--accent", dark ? "#7ea2ff" : "#1f4fd1");
  monaco.editor.defineTheme(name, {
    base: dark ? "vs-dark" : "vs",
    inherit: true,
    rules: [
      { token: "comment", foreground: strip(muted), fontStyle: "italic" },
      { token: "keyword", foreground: strip(accent) },
      { token: "string", foreground: strip(cssVar("--success", dark ? "#4ade80" : "#166534")) },
      { token: "number", foreground: strip(cssVar("--warning", dark ? "#fbbf24" : "#92400e")) },
    ],
    colors: {
      "editor.background": cssVar("--surface", dark ? "#111827" : "#ffffff"),
      "editor.foreground": fg,
      "editorLineNumber.foreground": muted,
      "editorLineNumber.activeForeground": fg,
      "editorCursor.foreground": accent,
      "editor.lineHighlightBackground": cssVar("--surface-2", dark ? "#1e293b" : "#f1f5f9"),
      "focusBorder": cssVar("--focus", accent),
    },
  });
  monaco.editor.setTheme(name);
  return name;
}

export { monaco };

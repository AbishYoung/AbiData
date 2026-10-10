import raw from "@/generated/lua-api.json";
import { luaAnchor } from "@/features/docs/docsLinks";

export type ScriptKind = "module" | "job" | "form_module" | "report_module";
export type LuaContext = "ui_event" | "report" | "job";

export interface ManifestParam {
  name: string;
  type: string;
  doc: string;
  optional?: boolean;
}

export interface ManifestFunction {
  name: string;
  signature: string;
  params?: ManifestParam[] | null;
  returns?: string | null;
  errors?: string[] | null;
  doc: string;
  example?: string | null;
  contexts?: string[] | null;
}

export interface ManifestValue {
  name: string;
  type: string;
  doc: string;
}

export interface ManifestModule {
  name: string;
  doc: string;
  contexts: string[];
  functions?: ManifestFunction[] | null;
  values?: ManifestValue[] | null;
  global?: boolean | null;
}

export const manifest: readonly ManifestModule[] = raw;

/** Lua contexts in which a script of this kind may run (module: any of them). */
export function contextsForKind(kind: ScriptKind): readonly LuaContext[] {
  switch (kind) {
    case "module":
      return ["ui_event", "report", "job"];
    case "job":
      return ["job"];
    case "form_module":
      return ["ui_event"];
    case "report_module":
      return ["report"];
  }
}

function available(contexts: readonly string[] | null | undefined, kind: ScriptKind): boolean {
  const allowed: readonly string[] = contextsForKind(kind);
  return (contexts ?? []).some((c) => allowed.includes(c));
}

export function functionContexts(mod: ManifestModule, fn: ManifestFunction): string[] {
  return fn.contexts && fn.contexts.length > 0 ? fn.contexts : mod.contexts;
}

export function modulesFor(api: readonly ManifestModule[], kind: ScriptKind): ManifestModule[] {
  return api.filter((m) => available(m.contexts, kind));
}

export function functionsFor(api: readonly ManifestModule[], kind: ScriptKind, moduleName: string): ManifestFunction[] {
  const mod = api.find((m) => m.name === moduleName);
  if (!mod) return [];
  return (mod.functions ?? []).filter((f) => available(functionContexts(mod, f), kind));
}

export function valuesFor(api: readonly ManifestModule[], kind: ScriptKind, moduleName: string): ManifestValue[] {
  const mod = api.find((m) => m.name === moduleName);
  if (!mod || !available(mod.contexts, kind)) return [];
  return mod.values ?? [];
}

export const KEYWORDS: readonly string[] = [
  "and", "break", "do", "else", "elseif", "end", "false", "for", "function", "if", "in",
  "local", "nil", "not", "or", "repeat", "return", "then", "true", "until", "while",
];

/** Globals that the sandbox keeps (BRIEF lua_runtime). */
export const GLOBALS: readonly string[] = [
  "assert", "error", "ipairs", "next", "pairs", "pcall", "select", "tonumber", "tostring", "type",
  "unpack", "xpcall", "setmetatable", "getmetatable", "rawequal", "rawget", "print", "require",
];

export const LIBRARIES: Readonly<Record<string, readonly string[]>> = {
  string: ["byte", "char", "find", "format", "gmatch", "gsub", "len", "lower", "match", "rep", "reverse", "sub", "upper"],
  table: ["concat", "insert", "maxn", "remove", "sort"],
  math: [
    "abs", "acos", "asin", "atan", "atan2", "ceil", "cos", "cosh", "deg", "exp", "floor", "fmod", "frexp", "huge",
    "ldexp", "log", "log10", "max", "min", "modf", "pi", "pow", "rad", "random", "randomseed", "sin", "sinh",
    "sqrt", "tan", "tanh",
  ],
};

export type SuggestKind = "module" | "function" | "value" | "keyword" | "global" | "library";

export interface Suggestion {
  label: string;
  kind: SuggestKind;
  insertText: string;
  detail: string;
  /** Markdown. */
  documentation: string;
}

const CONTEXT_LABEL: Readonly<Record<string, string>> = {
  ui_event: "form events",
  report: "reports",
  job: "jobs",
};

function contextList(ctx: readonly string[]): string {
  return ctx.map((c) => CONTEXT_LABEL[c] ?? c).join(", ");
}

function fence(code: string): string {
  return "```lua\n" + code + "\n```";
}

export function functionMarkdown(mod: ManifestModule, fn: ManifestFunction): string {
  const parts: string[] = [fence(fn.signature), fn.doc];
  const params = fn.params ?? [];
  if (params.length > 0) {
    parts.push(
      "**Parameters**\n\n" +
        params.map((p) => `- \`${p.name}\` (${p.type}${p.optional ? ", optional" : ""}): ${p.doc}`).join("\n"),
    );
  }
  if (fn.returns) parts.push(`**Returns** ${fn.returns}`);
  if (fn.errors && fn.errors.length > 0) parts.push(`**Errors** ${fn.errors.map((e) => `\`${e}\``).join(", ")}`);
  parts.push(`**Available in** ${contextList(functionContexts(mod, fn))}`);
  if (fn.example) parts.push("**Example**\n\n" + fence(fn.example));
  return parts.join("\n\n");
}

export function valueMarkdown(mod: ManifestModule, v: ManifestValue): string {
  return [fence(`${mod.name}.${v.name}: ${v.type}`), v.doc].join("\n\n");
}

export function moduleMarkdown(mod: ManifestModule): string {
  const members = [...(mod.functions ?? []).map((f) => f.name), ...(mod.values ?? []).map((v) => v.name)];
  const parts = [`**${mod.name}**`, mod.doc];
  if (members.length > 0) parts.push(members.map((m) => `\`${mod.name}.${m}\``).join(", "));
  parts.push(`**Available in** ${contextList(mod.contexts)}`);
  return parts.join("\n\n");
}

const MEMBER_AT_END = /([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z0-9_]*)$/;

/** Suggestions for the text before the cursor on the current line. */
export function suggestionsFor(api: readonly ManifestModule[], kind: ScriptKind, linePrefix: string): Suggestion[] {
  const member = MEMBER_AT_END.exec(linePrefix);
  if (member) {
    const owner = member[1] ?? "";
    const lib = LIBRARIES[owner];
    if (lib) {
      return lib.map((name) => ({
        label: name,
        kind: "function",
        insertText: name,
        detail: `${owner}.${name}`,
        documentation: fence(`${owner}.${name}`),
      }));
    }
    const mod = api.find((m) => m.name === owner);
    if (!mod) return [];
    const fns = functionsFor(api, kind, owner).map<Suggestion>((f) => ({
      label: f.name,
      kind: "function",
      insertText: f.name,
      detail: f.signature,
      documentation: functionMarkdown(mod, f),
    }));
    const vals = valuesFor(api, kind, owner).map<Suggestion>((v) => ({
      label: v.name,
      kind: "value",
      insertText: v.name,
      detail: `${v.name}: ${v.type}`,
      documentation: valueMarkdown(mod, v),
    }));
    return [...fns, ...vals];
  }
  // Right after a dot or colon whose owner is not a plain identifier, nothing sensible to offer.
  if (/[.:]\s*$/.test(linePrefix)) return [];
  return [
    ...modulesFor(api, kind).map<Suggestion>((m) => ({
      label: m.name,
      kind: "module",
      insertText: m.name,
      detail: "module",
      documentation: moduleMarkdown(m),
    })),
    ...Object.keys(LIBRARIES).map<Suggestion>((name) => ({
      label: name,
      kind: "library",
      insertText: name,
      detail: "Lua library",
      documentation: fence(name),
    })),
    ...GLOBALS.map<Suggestion>((name) => ({ label: name, kind: "global", insertText: name, detail: "Lua global", documentation: "" })),
    ...KEYWORDS.map<Suggestion>((name) => ({ label: name, kind: "keyword", insertText: name, detail: "keyword", documentation: "" })),
  ];
}

export interface HoverInfo {
  markdown: string;
  /** Docs page and section for this symbol, e.g. "reference/lua-api#db-query". */
  docsTarget: string;
  /** 1-based, end exclusive. */
  startColumn: number;
  endColumn: number;
}

/** The docs target of a Lua API section: a function ("db.query") or a module ("module-db"). */
export function luaDocsTarget(name: string): string {
  return `reference/lua-api#${luaAnchor(name)}`;
}

const CHAIN = /[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?/g;

/** Hover content for the identifier (or module.member) under a 1-based column. */
export function hoverFor(api: readonly ManifestModule[], kind: ScriptKind, lineText: string, column: number): HoverInfo | null {
  const offset = column - 1;
  for (const m of lineText.matchAll(CHAIN)) {
    const start = m.index;
    const end = start + m[0].length;
    if (offset < start || offset > end) continue;
    const [owner = "", member] = m[0].split(".");
    const mod = api.find((x) => x.name === owner);
    if (!mod) return null;
    const ownerEnd = start + owner.length;
    if (member === undefined || offset <= ownerEnd) {
      return {
        markdown: moduleMarkdown(mod),
        docsTarget: luaDocsTarget(`module-${mod.name}`),
        startColumn: start + 1,
        endColumn: ownerEnd + 1,
      };
    }
    const fn = (mod.functions ?? []).find((f) => f.name === member);
    const val = (mod.values ?? []).find((v) => v.name === member);
    const body = fn ? functionMarkdown(mod, fn) : val ? valueMarkdown(mod, val) : null;
    if (body === null) return null;
    const ctx = fn ? functionContexts(mod, fn) : mod.contexts;
    const note = available(ctx, kind)
      ? ""
      : `\n\n*Not available in this script type; it runs only in ${contextList(ctx)}.*`;
    return {
      markdown: body + note,
      docsTarget: luaDocsTarget(fn ? `${mod.name}.${member}` : `module-${mod.name}`),
      startColumn: ownerEnd + 2,
      endColumn: end + 1,
    };
  }
  return null;
}

export interface SignatureInfo {
  label: string;
  documentation: string;
  parameters: { label: string; documentation: string }[];
  activeParameter: number;
}

/** Signature help for the innermost unclosed call of a host function before the cursor. */
export function signatureFor(api: readonly ManifestModule[], kind: ScriptKind, linePrefix: string): SignatureInfo | null {
  let depth = 0;
  let commas = 0;
  let open = -1;
  let quote = "";
  for (let i = linePrefix.length - 1; i >= 0; i--) {
    const ch = linePrefix.charAt(i);
    if (quote) {
      if (ch === quote && linePrefix.charAt(i - 1) !== "\\") quote = "";
      continue;
    }
    if (ch === '"' || ch === "'") quote = ch;
    else if (ch === ")" || ch === "}" || ch === "]") depth++;
    else if (ch === "{" || ch === "[") {
      // An unclosed table constructor inside the call: its commas are not argument separators.
      if (depth === 0) commas = 0;
      else depth--;
    } else if (ch === "(") {
      if (depth === 0) {
        open = i;
        break;
      }
      depth--;
    } else if (ch === "," && depth === 0) commas++;
  }
  if (open < 0) return null;
  const callee = /([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\s*$/.exec(linePrefix.slice(0, open));
  if (!callee) return null;
  const owner = callee[1] ?? "";
  const mod = api.find((m) => m.name === owner);
  const fn = functionsFor(api, kind, owner).find((f) => f.name === callee[2]);
  if (!mod || !fn) return null;
  const params = fn.params ?? [];
  return {
    label: fn.signature,
    documentation: functionMarkdown(mod, fn),
    parameters: params.map((p) => ({ label: p.name, documentation: `${p.type}${p.optional ? ", optional" : ""}: ${p.doc}` })),
    activeParameter: Math.min(commas, Math.max(params.length - 1, 0)),
  };
}

import type { Column, Table } from "@/lib/schema";
import type { SchedulePreset } from "@/features/jobs/schedule";

/**
 * Example scripts offered under "Start from" when creating a script or a job. A template can be
 * bound to a table: its source then names that table and its columns. Scripts refer to both by
 * physical name (the SQL name), taken from the schema as is, never derived from display names.
 */
export type TemplateKind = "job" | "module";

export interface BoundColumn {
  /** Physical (SQL) name, used in records.* values and in SQL. */
  name: string;
  /** Display name, for comments and log messages. */
  label: string;
  type: Column["field_type"];
}

export interface BoundTable {
  name: string;
  label: string;
  /** Visible, writable-or-readable user columns in table order (no system id/_version). */
  columns: BoundColumn[];
}

/** Job settings a job template suggests; the user can change any of them before saving. */
export interface JobSuggestion {
  preset: SchedulePreset;
  /** "HH:MM" for daily/weekly presets. */
  time?: string;
  args: Record<string, unknown>;
}

export interface ScriptTemplate {
  id: string;
  title: string;
  /** One sentence shown under the picker. */
  description: string;
  kind: TemplateKind;
  /** "required": the source needs a table; "optional": it works without one; absent: never uses one. */
  table?: "required" | "optional";
  /** Tables this template can be bound to (e.g. ones with a date column); all tables when absent. */
  fits?: (t: BoundTable) => boolean;
  /** Suggested script name; for modules it must match MODULE_NAME. */
  name: (t: BoundTable | null) => string;
  /** The Lua source. Called with null only when `table` is not "required". */
  source: (t: BoundTable | null) => string;
  /** Job templates only. */
  job?: JobSuggestion;
}

/** The table as a template sees it: physical names, user columns in order. */
export function bindTable(t: Table): BoundTable {
  return {
    name: t.physical_name,
    label: t.display_name,
    columns: [...(t.columns ?? [])]
      .filter((c) => !c.is_hidden)
      .sort((a, b) => a.ordinal - b.ordinal)
      .map((c) => ({ name: c.physical_name, label: c.display_name, type: c.field_type })),
  };
}

/** A Lua string literal for any text. */
export function luaString(s: string): string {
  return `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n").replace(/\r/g, "\\r")}"`;
}

/** A double-quoted SQL identifier. */
export function sqlIdent(s: string): string {
  return `"${s.replace(/"/g, '""')}"`;
}

/** A Lua table key: bare when it is a plain identifier, else ["..."]. */
export function luaKey(s: string): string {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(s) && !LUA_KEYWORDS.has(s) ? s : `[${luaString(s)}]`;
}

const LUA_KEYWORDS = new Set([
  "and", "break", "do", "else", "elseif", "end", "false", "for", "function", "goto", "if", "in",
  "local", "nil", "not", "or", "repeat", "return", "then", "true", "until", "while",
]);

/** Collapses text to one line so it is safe inside a Lua `--` comment. */
function oneLine(s: string): string {
  return s.replace(/\s+/g, " ").trim();
}

/** Joins lines into a source file: no trailing whitespace, ends with a newline. */
function src(lines: string[]): string {
  return lines.map((l) => l.replace(/\s+$/, "")).join("\n") + "\n";
}

/** `item.name` for plain identifiers, `item["odd name"]` otherwise. */
function member(obj: string, name: string): string {
  const k = luaKey(name);
  return k.startsWith("[") ? `${obj}${k}` : `${obj}.${k}`;
}

/** A Lua list of strings: {"a", "b"}. */
function luaList(items: string[]): string {
  return `{${items.map(luaString).join(", ")}}`;
}

/** A name safe for artifacts.save: letters, digits, space, dot, underscore, dash; at most 128 chars. */
function artifactName(s: string, ext: string): string {
  const base = s.replace(/[^A-Za-z0-9 ._-]/g, "_").slice(0, 100) || "export";
  return base + ext;
}

/** A module name derived from a table's physical name; always matches MODULE_NAME. */
export function moduleNameFor(physical: string): string {
  let n = physical.toLowerCase().replace(/[^a-z0-9_]/g, "_").slice(0, 90);
  if (!/^[a-z]/.test(n)) n = "t" + n;
  return n + "_data";
}

function clip(s: string, max = 200): string {
  return s.length > max ? s.slice(0, max) : s;
}

function need(t: BoundTable | null): BoundTable {
  if (!t) throw new Error("this template needs a table");
  return t;
}

function isDateType(type: string): boolean {
  return type === "date" || type === "datetime";
}

const countRows: ScriptTemplate = {
  id: "job.count-rows",
  title: "Count rows and log the total",
  description: "Counts the rows of a table and writes the total to the run log.",
  kind: "job",
  table: "required",
  name: (t) => clip(`Count ${need(t).label} rows`),
  source: (bt) => {
    const t = need(bt);
    return src([
      `-- Counts the rows in ${oneLine(t.label)} and logs the total.`,
      "-- Change the SQL to count something narrower, for example by adding a WHERE clause.",
      `local total = db.query_value(${luaString(`SELECT count(*) FROM ${sqlIdent(t.name)}`)})`,
      `log.info(${luaString(`Rows in ${oneLine(t.label)}`)}, {table = ${luaString(t.name)}, total = total})`,
    ]);
  },
  job: { preset: "daily", time: "07:00", args: {} },
};

const exportCsv: ScriptTemplate = {
  id: "job.export-csv",
  title: "Export a table to CSV",
  description: "Writes the table's rows to a CSV file you can download from the run's history.",
  kind: "job",
  table: "required",
  name: (t) => clip(`Export ${need(t).label} to CSV`),
  source: (bt) => {
    const t = need(bt);
    const cols = t.columns.length > 0 ? t.columns.map((c) => c.name) : ["id"];
    const select = `SELECT ${cols.map(sqlIdent).join(", ")} FROM ${sqlIdent(t.name)} ORDER BY "id" LIMIT :limit`;
    return src([
      `-- Exports ${oneLine(t.label)} to a CSV file, available from the run's history.`,
      '-- Change the columns in the SQL and in "columns", or set "limit" in the job arguments (at most 10000 rows).',
      "local limit = job.args.limit or 10000",
      `local rows = db.query(${luaString(select)}, {limit = limit})`,
      `local text = csv.encode(rows, {columns = ${luaList(cols)}})`,
      `artifacts.save(${luaString(artifactName(t.name, ".csv"))}, text, {mime = "text/csv"})`,
      `log.info("Exported rows", {table = ${luaString(t.name)}, rows = #rows})`,
    ]);
  },
  job: { preset: "manual", args: { limit: 10000 } },
};

const importJson: ScriptTemplate = {
  id: "job.import-json",
  title: "Import JSON from a web address",
  description: "Downloads a JSON array from a URL and adds each item as a row (or logs it).",
  kind: "job",
  table: "optional",
  name: (t) => clip(t ? `Import JSON into ${t.label}` : "Import JSON"),
  source: (t) => {
    const out = [
      "-- Downloads a JSON array from job.args.url and processes each item.",
      "-- The host must be on the platform's egress allowlist; ask an administrator if the request is denied.",
      "local url = job.args.url",
      "if not url or url == \"\" then",
      "  error(\"Set the \\\"url\\\" argument in the job's settings\")",
      "end",
      "",
      "local res = fetch(url)",
      "if not res.ok then",
      "  error(\"Download failed with status \" .. res.status)",
      "end",
      "local items = res:json()",
      "if type(items) ~= \"table\" then",
      "  error(\"Expected a JSON array at \" .. url)",
      "end",
      "",
      "local imported = 0",
      "for _, item in ipairs(items) do",
    ];
    if (t) {
      out.push(`  -- Adjust the right-hand sides to match the JSON field names.`);
      out.push(`  records.insert(${luaString(t.name)}, {`);
      for (const c of t.columns) {
        if (c.type === "autonumber" || c.type === "calculated") continue;
        const note = c.type === "lookup" ? " -- id of the related row" : "";
        out.push(`    ${luaKey(c.name)} = ${member("item", c.name)},${note}`);
      }
      out.push("  })");
    } else {
      out.push("  -- To store items, pick a table and insert them, for example:");
      out.push('  -- records.insert("Customers", {Name = item.name, City = item.city})');
      out.push("  log.info(\"Item\", {item = json.encode(item)})");
    }
    out.push("  imported = imported + 1", "end", "", 'log.info("Import finished", {url = url, imported = imported})');
    return src(out);
  },
  job: { preset: "manual", args: { url: "https://example.com/data.json" } },
};

const cleanOldRows: ScriptTemplate = {
  id: "job.clean-old-rows",
  title: "Delete rows older than a number of days",
  description: "Finds rows whose date is older than a cutoff and deletes them (a dry run by default).",
  kind: "job",
  table: "required",
  fits: (t) => t.columns.some((c) => isDateType(c.type)),
  name: (t) => clip(`Clean old ${need(t).label} rows`),
  source: (bt) => {
    const t = need(bt);
    const col = t.columns.find((c) => isDateType(c.type));
    if (!col) throw new Error("this template needs a table with a date column");
    const now = col.type === "date" ? "datetime.today()" : "datetime.now()";
    const sql = `SELECT "id" FROM ${sqlIdent(t.name)} WHERE ${sqlIdent(col.name)} < :cutoff`;
    return src([
      `-- Deletes rows of ${oneLine(t.label)} whose ${oneLine(col.label)} is more than "days" days ago.`,
      '-- It only logs what it would delete until you set "dry_run" to false in the job arguments.',
      "local days = job.args.days or 90",
      "local dry_run = job.args.dry_run ~= false",
      `local cutoff = datetime.add(${now}, {days = -days})`,
      "",
      "-- A query returns at most 10000 rows, so a large backlog is cleared over several runs.",
      `local rows = db.query(${luaString(sql)}, {cutoff = cutoff})`,
      `log.info("Old rows found", {table = ${luaString(t.name)}, cutoff = cutoff, count = #rows, dry_run = dry_run})`,
      "",
      "if dry_run then",
      '  log.info("Dry run: nothing was deleted")',
      "else",
      "  for _, row in ipairs(rows) do",
      `    records.delete(${luaString(t.name)}, row.id)`,
      "  end",
      '  log.info("Deleted rows", {count = #rows})',
      "end",
    ]);
  },
  job: { preset: "daily", time: "03:00", args: { days: 90, dry_run: true } },
};

const blankJob: ScriptTemplate = {
  id: "job.blank",
  title: "Blank job with arguments",
  description: "A minimal job that reads its arguments and writes to the run log.",
  kind: "job",
  name: () => "My job",
  source: () =>
    src([
      "-- A job succeeds when the script reaches its end and fails when it calls error().",
      "-- Set values in the job's arguments and read them from job.args.",
      'local who = job.args.who or "world"',
      'log.info("Hello, " .. who, {run = job.run_id})',
      "",
      '-- To produce a file for download: artifacts.save("notes.txt", "text", {mime = "text/plain"})',
    ]),
  job: { preset: "manual", args: {} },
};

const helpers: ScriptTemplate = {
  id: "module.helpers",
  title: "Formatting helpers",
  description: "Small functions for formatting and cleaning values, ready to call from other scripts.",
  kind: "module",
  name: () => "helpers",
  source: () =>
    src([
      "-- Small helpers for other scripts. Use them as helpers.money(x), helpers.trim(s) and so on",
      "-- after loading the module with require(\"helpers\") (the module's name).",
      "local M = {}",
      "",
      "local function missing(v)",
      "  return v == nil or db.is_null(v)",
      "end",
      "",
      '-- Removes spaces from both ends of s; nil and NULL become "".',
      "function M.trim(s)",
      "  if missing(s) then return \"\" end",
      "  return (tostring(s):gsub(\"^%s+\", \"\"):gsub(\"%s+$\", \"\"))",
      "end",
      "",
      "-- True when s is nil, NULL, empty or only spaces.",
      "function M.is_blank(s)",
      "  return M.trim(s) == \"\"",
      "end",
      "",
      "-- Returns v, or fallback when v is nil, NULL or blank.",
      "function M.default(v, fallback)",
      "  if missing(v) or M.is_blank(v) then return fallback end",
      "  return v",
      "end",
      "",
      '-- Formats a number with two decimals and thousands separators: 1234.5 -> "1,234.50".',
      '-- Returns "" when x is not a number.',
      "function M.money(x)",
      "  if missing(x) then return \"\" end",
      "  local n = tonumber(tostring(x))",
      "  if n == nil then return \"\" end",
      "  local int, frac = string.format(\"%.2f\", n):match(\"^(-?%d+)(%.%d+)$\")",
      "  int = int:reverse():gsub(\"(%d%d%d)\", \"%1,\"):reverse():gsub(\"^(-?),\", \"%1\")",
      "  return int .. frac",
      "end",
      "",
      "return M",
    ]),
};

const tableAccess: ScriptTemplate = {
  id: "module.table-access",
  title: "Read and write one table",
  description: "Functions to get, find, insert, update and delete rows of one table.",
  kind: "module",
  table: "required",
  name: (t) => moduleNameFor(need(t).name),
  source: (bt) => {
    const t = need(bt);
    const known = ["id", ...t.columns.map((c) => c.name)];
    const out = [
      `-- Access to ${oneLine(t.label)}. Load it with require(${luaString(moduleNameFor(t.name))}).`,
      "-- Columns (physical name - display name - type):",
      "--   id - the row id",
      ...t.columns.map((c) => `--   ${oneLine(c.name)} - ${oneLine(c.label)} - ${c.type}`),
      "local M = {}",
      "",
      `local TABLE = ${luaString(t.name)}`,
      `local TABLE_SQL = ${luaString(sqlIdent(t.name))}`,
      "",
      "-- Only these columns may be used by find_by, so a column name is never taken on trust.",
      "local COLUMNS = {",
      ...known.map((n) => `  [${luaString(n)}] = true,`),
      "}",
      "",
      "local function quote(name)",
      "  return '\"' .. name:gsub('\"', '\"\"') .. '\"'",
      "end",
      "",
      "-- The row with this id, or nil.",
      "function M.get(id)",
      "  return records.get(TABLE, id)",
      "end",
      "",
      "-- The first row whose column equals value, or nil. Unknown columns raise an error.",
      "function M.find_by(column, value)",
      "  if not COLUMNS[column] then",
      "    error(\"unknown column: \" .. tostring(column))",
      "  end",
      "  return db.query_one(\"SELECT * FROM \" .. TABLE_SQL .. \" WHERE \" .. quote(column) .. \" = :value LIMIT 1\", {value = value})",
      "end",
      "",
      "-- The number of rows.",
      "function M.count()",
      "  return db.count(TABLE)",
      "end",
      "",
      "-- Adds a row from a table of column values; returns its id.",
      "function M.insert(values)",
      "  return records.insert(TABLE, values)",
      "end",
      "",
      "-- Changes the given columns. Pass the row's _version to refuse the change if the row has been edited since.",
      "function M.update(id, values, version)",
      "  return records.update(TABLE, id, values, {version = version})",
      "end",
      "",
      "-- Deletes a row (optionally only if it still has this version).",
      "function M.delete(id, version)",
      "  records.delete(TABLE, id, {version = version})",
      "end",
      "",
      "return M",
    ];
    return src(out);
  },
};

export const TEMPLATES: readonly ScriptTemplate[] = [
  countRows,
  exportCsv,
  importJson,
  cleanOldRows,
  blankJob,
  helpers,
  tableAccess,
];

export function templatesFor(kind: TemplateKind): ScriptTemplate[] {
  return TEMPLATES.filter((t) => t.kind === kind);
}

export function findTemplate(id: string): ScriptTemplate | undefined {
  return TEMPLATES.find((t) => t.id === id);
}

/** The tables a template can be bound to. */
export function tablesFor(t: ScriptTemplate, tables: BoundTable[]): BoundTable[] {
  return t.fits ? tables.filter(t.fits) : tables;
}

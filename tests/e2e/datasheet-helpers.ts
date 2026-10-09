import { expect, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { ADMIN_EMAIL, signIn } from "./helpers";

export interface Fixture {
  ws: string;
  everythingId: string;
  peopleId: string;
  /** Column IDs of the Everything table by display name. */
  cols: Record<string, string>;
  /** Column ID of People."Full name". */
  fullNameId: string;
  /** Row IDs by Name. */
  rows: Record<string, number>;
  people: Record<string, number>;
}

export interface ApiRow {
  id: number;
  _version: number;
  fields: Record<string, unknown>;
  display?: Record<string, string>;
}

interface ApiResult {
  status: number;
  body: unknown;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Calls the JSON API as the signed-in user (CSRF token and Origin like the SPA). */
export async function callApi(page: Page, method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<ApiResult> {
  const origin = new URL(page.url()).origin;
  const meRes = await page.request.get(`${origin}/api/v1/auth/me`);
  const me: unknown = await meRes.json();
  const csrf = isRecord(me) && typeof me.csrf_token === "string" ? me.csrf_token : "";
  const res = await page.request.fetch(`${origin}${path}`, {
    method,
    headers: { "X-CSRF-Token": csrf, Origin: origin, "Content-Type": "application/json", ...headers },
    data: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  return { status: res.status(), body: text ? (JSON.parse(text) as unknown) : null };
}

async function mustCall(page: Page, method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<unknown> {
  const res = await callApi(page, method, path, body, headers);
  expect(res.status, `${method} ${path}: ${JSON.stringify(res.body)}`).toBeLessThan(300);
  return res.body;
}

function asRecord(v: unknown): Record<string, unknown> {
  if (!isRecord(v)) throw new Error(`expected an object, got ${JSON.stringify(v)}`);
  return v;
}

function asArray(v: unknown): unknown[] {
  if (!Array.isArray(v)) throw new Error(`expected an array, got ${JSON.stringify(v)}`);
  return v;
}

function str(v: unknown): string {
  if (typeof v !== "string") throw new Error(`expected a string, got ${JSON.stringify(v)}`);
  return v;
}

function tableByName(schema: unknown, name: string): Record<string, unknown> {
  const t = asArray(asRecord(schema).tables).map(asRecord).find((x) => x.display_name === name);
  if (!t) throw new Error(`table ${name} not found`);
  return t;
}

function columnIds(table: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const c of asArray(table.columns).map(asRecord)) out[str(c.display_name)] = str(c.id);
  return out;
}

/** Creates a workspace with a People table and an Everything table holding one column of every field type. */
export async function createFixture(page: Page): Promise<Fixture> {
  await signIn(page);
  const ws = str(asRecord(await mustCall(page, "POST", "/api/v1/admin/workspaces", { name: "Datasheet", owner_email: ADMIN_EMAIL })).id);
  const base = `/api/v1/w/${ws}`;

  const peopleApply = asRecord(
    await mustCall(page, "POST", `${base}/schema/apply`, {
      ops: [{ op: "create_table", table: { display_name: "People", columns: [{ display_name: "Full name", field_type: "short_text", is_required: true }] } }],
    }),
  );
  const people = tableByName(peopleApply.schema, "People");
  const peopleId = str(people.id);
  const fullNameId = columnIds(people)["Full name"] ?? "";

  const everythingApply = asRecord(
    await mustCall(page, "POST", `${base}/schema/apply`, {
      ops: [
        {
          op: "create_table",
          table: {
            display_name: "Everything",
            columns: [
              { display_name: "Name", field_type: "short_text", is_required: true, type_options: { max_length: 40 } },
              { display_name: "Notes", field_type: "long_text" },
              { display_name: "Quantity", field_type: "integer" },
              { display_name: "Weight", field_type: "number" },
              { display_name: "Price", field_type: "currency" },
              { display_name: "Active", field_type: "yes_no" },
              { display_name: "Born", field_type: "date" },
              { display_name: "Seen", field_type: "datetime" },
              { display_name: "Website", field_type: "hyperlink" },
              { display_name: "Owner", field_type: "lookup", lookup: { parent_table_id: peopleId, display_column_id: fullNameId } },
              { display_name: "Doubled", field_type: "calculated", type_options: { expression: '"Quantity" * 2', result_type: "integer" } },
            ],
          },
        },
      ],
    }),
  );
  const everything = tableByName(everythingApply.schema, "Everything");
  const everythingId = str(everything.id);
  const cols = columnIds(everything);

  const peopleIds: Record<string, number> = {};
  for (const n of ["Alice Anders", "Bob Brown"]) {
    const r = asRecord(await mustCall(page, "POST", `${base}/tables/${peopleId}/rows`, { fields: { [fullNameId]: n } }));
    peopleIds[n] = Number(r.id);
  }
  const rows: Record<string, number> = {};
  const seeds: [string, number][] = [
    ["Alpha", 5],
    ["Bravo", 7],
    ["Charlie", 9],
    ["Zulu", 1],
  ];
  for (const [name, qty] of seeds) {
    const r = asRecord(
      await mustCall(page, "POST", `${base}/tables/${everythingId}/rows`, {
        fields: { [cols.Name ?? ""]: name, [cols.Quantity ?? ""]: qty },
      }),
    );
    rows[name] = Number(r.id);
  }
  return { ws, everythingId, peopleId, cols, fullNameId, rows, people: peopleIds };
}

export async function getRow(page: Page, f: Fixture, tableId: string, id: number): Promise<ApiRow> {
  const body = asRecord(await mustCall(page, "GET", `/api/v1/w/${f.ws}/tables/${tableId}/rows/${id}`));
  return { id: Number(body.id), _version: Number(body._version), fields: asRecord(body.fields), display: isRecord(body.display) ? (body.display as Record<string, string>) : undefined };
}

export async function patchRow(page: Page, f: Fixture, id: number, version: number, fields: Record<string, unknown>): Promise<ApiRow> {
  const body = asRecord(
    await mustCall(page, "PATCH", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows/${id}`, { fields }, { "If-Match": `"${version}"` }),
  );
  return { id: Number(body.id), _version: Number(body._version), fields: asRecord(body.fields) };
}

export async function insertRow(page: Page, f: Fixture, fields: Record<string, unknown>): Promise<ApiRow> {
  const body = asRecord(await mustCall(page, "POST", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows`, { fields }));
  return { id: Number(body.id), _version: Number(body._version), fields: asRecord(body.fields) };
}

/** Signs in and opens a table's datasheet through the navigation pane. */
export async function openTable(page: Page, f: Fixture, tableName: string): Promise<Locator> {
  await signIn(page);
  await page.goto(`/w/${f.ws}`);
  return switchTable(page, tableName);
}

/** Opens a table's datasheet from the navigation pane of the already open workspace. */
export async function switchTable(page: Page, tableName: string): Promise<Locator> {
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await nav.getByRole("button", { name: tableName, exact: true }).click();
  const grid = page.getByRole("grid", { name: `${tableName} datasheet` });
  await expect(grid).toBeVisible();
  return grid;
}

/** The nth (1-based, aria-rowindex) row of the grid. */
export function rowAt(grid: Locator, n: number): Locator {
  return grid.locator(`[role="row"][aria-rowindex="${n}"]`);
}

export async function colIndex(grid: Locator, column: string): Promise<string> {
  const header = grid.getByRole("columnheader").filter({ has: grid.page().getByRole("button", { name: column, exact: true }) });
  const idx = await header.getAttribute("aria-colindex");
  if (!idx) throw new Error(`column ${column} not found`);
  return idx;
}

/** The cell of a row (aria-rowindex) in a named column. */
export async function cellAt(grid: Locator, n: number, column: string): Promise<Locator> {
  return rowAt(grid, n).locator(`[role="gridcell"][aria-colindex="${await colIndex(grid, column)}"]`);
}

/** The aria-rowindex of the row whose cell in `column` has the given text. */
export async function rowIndexOf(grid: Locator, name: string, column = "Name"): Promise<number> {
  const count = Number(await grid.getAttribute("aria-rowcount"));
  const nameIdx = await colIndex(grid, column);
  for (let n = 1; n <= count + 1; n++) {
    const cell = rowAt(grid, n).locator(`[role="gridcell"][aria-colindex="${nameIdx}"]`);
    if ((await cell.count()) > 0 && (await cell.innerText()).trim() === name) return n;
  }
  throw new Error(`no row named ${name}`);
}

/** Runs axe (wcag2a, wcag2aa) on the whole page with the datasheet open (including any dialogs). */
export async function expectDatasheetAccessible(page: Page): Promise<void> {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
}

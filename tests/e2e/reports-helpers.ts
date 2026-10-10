import { expect, type Page } from "@playwright/test";
import { apiPost } from "./queries-helpers";
import { workspaceId } from "./tabledesign-helpers";

export interface ApiTable {
  id: string;
  display_name: string;
  columns: { id: string; display_name: string }[];
}

export type PaperName = "letter" | "a4";

async function createTable(page: Page, name: string, columns: { display_name: string; field_type: string }[]): Promise<ApiTable> {
  const ws = workspaceId(page);
  const out = await apiPost<{ schema: { tables: ApiTable[] } }>(page, `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table: { display_name: name, columns } }],
  });
  const t = out.schema.tables.find((x) => x.display_name === name);
  if (!t) throw new Error(`table ${name} was not created`);
  return t;
}

export function colId(t: ApiTable, name: string): string {
  const c = t.columns.find((x) => x.display_name === name);
  if (!c) throw new Error(`no column ${name} in ${t.display_name}`);
  return c.id;
}

async function insertAll(page: Page, t: ApiTable, rows: Record<string, unknown>[]): Promise<void> {
  const ws = workspaceId(page);
  for (let i = 0; i < rows.length; i += 500) {
    const ops = rows.slice(i, i + 500).map((fields) => ({ op: "insert", fields }));
    await apiPost(page, `/api/v1/w/${ws}/tables/${t.id}/rows/batch`, { ops });
  }
}

export const REGIONS = ["North", "South", "East"];
export const ROWS_PER_REGION = 30;
export const LONG_NOTE =
  "This is a deliberately long note that must wrap over several lines inside its text box so the detail band grows and the paginator has to use the measured height of the band.";

/** Sales(Region, Rep, Amount, Note): 3 regions x 30 rows of 10.00; every 10th note is long. */
export async function createSales(page: Page): Promise<ApiTable> {
  const t = await createTable(page, "Sales", [
    { display_name: "Region", field_type: "short_text" },
    { display_name: "Rep", field_type: "short_text" },
    { display_name: "Amount", field_type: "currency" },
    { display_name: "Note", field_type: "short_text" },
  ]);
  const rows: Record<string, unknown>[] = [];
  REGIONS.forEach((region, r) => {
    for (let i = 1; i <= ROWS_PER_REGION; i++) {
      const n = r * ROWS_PER_REGION + i;
      rows.push({
        [colId(t, "Region")]: region,
        [colId(t, "Rep")]: `Rep${String(n).padStart(3, "0")}`,
        [colId(t, "Amount")]: "10.00",
        [colId(t, "Note")]: n % 10 === 0 ? LONG_NOTE : `note ${n}`,
      });
    }
  });
  await insertAll(page, t, rows);
  return t;
}

/** Big(Name, Qty) with n rows. */
export async function createBig(page: Page, n: number): Promise<ApiTable> {
  const t = await createTable(page, "Big", [
    { display_name: "Name", field_type: "short_text" },
    { display_name: "Qty", field_type: "integer" },
  ]);
  const rows: Record<string, unknown>[] = [];
  for (let i = 1; i <= n; i++) rows.push({ [colId(t, "Name")]: `Item${String(i).padStart(5, "0")}`, [colId(t, "Qty")]: i });
  await insertAll(page, t, rows);
  return t;
}

const ctl = (id: string, type: string, x: number, w: number, extra: Record<string, unknown> = {}) => ({
  id,
  name: id,
  type,
  x,
  y: 0,
  w,
  h: 20,
  ...extra,
});

/** Mirrors the golden test layout: report header, page header, group header/footer, grow detail, report footer. */
export function salesReportBody(t: ApiTable, name: string, paper: PaperName) {
  const bind = (c: string) => ({ column_id: colId(t, c) });
  const sum = (scope: string) => ({ aggregate: "sum", column_id: colId(t, "Amount"), scope });
  return {
    name,
    source_kind: "table",
    source_id: t.id,
    page_setup: { paper, orientation: "portrait", margin_top: 48, margin_bottom: 48, margin_left: 48, margin_right: 48 },
    layout: {
      width: 700,
      bands: {
        report_header: { height: 30, controls: [ctl("title", "label", 0, 300, { caption: "Sales by region", h: 28, style: { bold: "true", font_size: "18" } })] },
        page_header: {
          height: 24,
          controls: [
            ctl("ph1", "label", 0, 40, { caption: "Page" }),
            ctl("pn", "page_number", 44, 30),
            ctl("ph2", "label", 80, 20, { caption: "of" }),
            ctl("pc", "page_count", 104, 30),
          ],
        },
        detail: {
          height: 20,
          can_grow: true,
          controls: [
            ctl("rep", "textbox", 0, 100, { bind: bind("Rep") }),
            ctl("amt", "textbox", 110, 80, { bind: bind("Amount"), style: { align: "right" } }),
            ctl("note", "textbox", 210, 300, { bind: bind("Note") }),
          ],
        },
        page_footer: { height: 24, controls: [ctl("pf", "label", 0, 300, { caption: "Confidential sales footer" })] },
        report_footer: {
          height: 24,
          controls: [ctl("glab", "label", 0, 100, { caption: "Grand total" }), ctl("grand", "textbox", 110, 80, { aggregate: sum("report"), style: { align: "right" } })],
        },
      },
      groups: [
        {
          column_id: colId(t, "Region"),
          sort: "asc",
          header: { height: 24, keep_together: true, controls: [ctl("gh", "textbox", 0, 200, { bind: bind("Region"), style: { bold: "true" } })] },
          footer: {
            height: 24,
            controls: [ctl("glabel", "label", 0, 100, { caption: "Region total" }), ctl("gsum", "textbox", 110, 80, { aggregate: sum("group"), style: { align: "right" } })],
          },
        },
      ],
      sorts: [{ column_id: colId(t, "Rep") }],
    },
  };
}

export function bigReportBody(t: ApiTable) {
  return {
    name: "Big report",
    source_kind: "table",
    source_id: t.id,
    page_setup: { paper: "letter", orientation: "portrait", margin_top: 48, margin_bottom: 48, margin_left: 48, margin_right: 48 },
    layout: {
      width: 700,
      bands: {
        report_header: { height: 0, controls: [] },
        page_header: { height: 24, controls: [ctl("pn", "page_number", 0, 30), ctl("pc", "page_count", 40, 40)] },
        detail: {
          height: 16,
          controls: [
            ctl("nm", "textbox", 0, 120, { bind: { column_id: colId(t, "Name") }, h: 16 }),
            ctl("q", "textbox", 130, 60, { bind: { column_id: colId(t, "Qty") }, h: 16 }),
          ],
        },
        page_footer: { height: 0, controls: [] },
        report_footer: { height: 0, controls: [] },
      },
      groups: [],
      sorts: [],
    },
  };
}

export async function createReportViaApi(page: Page, body: unknown): Promise<string> {
  const r = await apiPost<{ id: string }>(page, `/api/v1/w/${workspaceId(page)}/reports`, body);
  return r.id;
}

/** Reads "Page n of N" from the viewer toolbar. */
export async function pageOf(page: Page): Promise<{ current: number; total: number }> {
  const text = (await page.getByTestId("report-page-of").innerText()).trim();
  const m = /^Page (\d+) of (\d+)$/.exec(text);
  if (!m?.[1] || !m[2]) throw new Error(`unexpected page indicator: ${text}`);
  return { current: Number(m[1]), total: Number(m[2]) };
}

export async function openReport(page: Page, name: string): Promise<void> {
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await nav.getByRole("button", { name, exact: true }).click();
  await expect(page.getByTestId("report-page-of")).toBeVisible();
}

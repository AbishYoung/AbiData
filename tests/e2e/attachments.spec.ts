// File attachments (ADR 0011): the field type in the designer, the attachment manager in the
// datasheet, a form Attachments control, a report Image control, the phone experience and axe in
// both colour schemes. The use case is a field crew attaching phone photos to an invasive-species
// sighting, which coordinators then view in forms and reports.
import { deflateSync } from "node:zlib";
import { expect, test, type Locator, type Page } from "@playwright/test";
import { callApi, cellAt, getRow, type Fixture } from "./datasheet-helpers";
import { createReportViaApi, openReport } from "./reports-helpers";
import { expectNoAxeViolations, signIn } from "./helpers";
import { apiPost, signInAs } from "./queries-helpers";
import { openForm } from "./forms-runtime-helpers";
import {
  addField,
  applyFromDialog,
  createWorkspace,
  designView,
  expectDesignViewAccessible,
  fetchSchema,
  nav,
  previewChanges,
  propertiesPanel,
  startNewTable,
  workspaceId,
  type SchemaTable,
} from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });
test.use({ actionTimeout: 20_000 });

// ---- a tiny PNG made in the test ---------------------------------------------------------------

const CRC_TABLE = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
function crc32(buf: Buffer): number {
  let c = 0xffffffff;
  for (const b of buf) c = (CRC_TABLE[(c ^ b) & 0xff] ?? 0) ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}
function chunk(type: string, data: Buffer): Buffer {
  const head = Buffer.alloc(4);
  head.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([head, body, crc]);
}
/** A valid size x size PNG of one colour. */
function png(r: number, g: number, b: number, size = 8): Buffer {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 2; // colour type: RGB
  const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: size }, () => [r, g, b]).flat())]);
  const raw = Buffer.concat(Array.from({ length: size }, () => row));
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

const RED = png(220, 30, 30);
const GREEN = png(30, 160, 60);
const BLUE = png(30, 60, 200);
const file = (name: string, buffer: Buffer) => ({ name, mimeType: "image/png", buffer });

const WS = `Field Photos ${Date.now()}`;
let sightings: SchemaTable;
let speciesId = "";
let photosId = "";
let rowId = 0;
let wsId = "";

/** Every test has a fresh browser page: sign in and open the workspace. */
async function enter(page: Page): Promise<void> {
  await signIn(page);
  await page.goto(`/w/${wsId}`);
  await expect(nav(page).getByRole("button", { name: "Sightings", exact: true })).toBeVisible();
}

async function fx(page: Page): Promise<Fixture> {
  // Only `ws` and the table are used by the datasheet helpers below.
  return { ws: wsId, everythingId: sightings.id } as Fixture;
}

async function openSightings(page: Page): Promise<Locator> {
  await nav(page).getByRole("button", { name: "Sightings", exact: true }).click();
  const grid = page.getByRole("grid", { name: "Sightings datasheet" });
  await expect(grid).toBeVisible();
  return grid;
}

async function openPhotosDialog(page: Page, grid: Locator): Promise<Locator> {
  const cell = await cellAt(grid, 1, "Photos");
  await cell.click();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Photos" });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function storedPhotos(page: Page): Promise<string[]> {
  const row = await getRow(page, await fx(page), sightings.id, rowId);
  const v = row.fields[photosId];
  return Array.isArray(v) ? (v as string[]) : [];
}

async function fileNames(page: Page, id: string): Promise<string> {
  const res = await page.request.get(`/api/v1/w/${wsId}/files/${id}`);
  expect(res.ok()).toBeTruthy();
  return ((await res.json()) as { name: string }).name;
}

const tileNames = (dialog: Locator) => dialog.getByTestId("attachment-tile").locator("p.truncate");

test("designer: Attachments type with a tooltip and a files-per-record option", async ({ page }) => {
  await createWorkspace(page, WS);
  wsId = workspaceId(page);
  await startNewTable(page, "Sightings");
  await addField(page, 2, "Species", "Short text");
  await addField(page, 3, "Photos", "Attachments");

  // The data type picker explains the type; the property is a number from 1 to 100 (default 20).
  await designView(page).getByRole("row", { name: "Field 3", exact: true }).getByLabel("Data type").hover();
  await expect(page.getByRole("tooltip")).toContainText("photos and documents kept with the record");
  const files = propertiesPanel(page).getByLabel("Files per record");
  await expect(files).toHaveValue("20");
  await files.fill("3");
  // No default value and no index for a list of files.
  await expect(propertiesPanel(page).getByLabel("Default value")).toHaveCount(0);
  await expect(propertiesPanel(page).getByLabel("Indexed")).toHaveCount(0);

  await files.fill("0");
  await designView(page).getByRole("button", { name: "Preview changes" }).click();
  await expect(designView(page).getByText("Files per record must be a whole number from 1 to 100.")).toBeVisible();
  await files.fill("3");

  await expectDesignViewAccessible(page);
  const dialog = await previewChanges(page);
  await applyFromDialog(dialog);

  sightings = (await fetchSchema(page)).find((t) => t.display_name === "Sightings") as SchemaTable;
  const photos = sightings.columns.find((c) => c.display_name === "Photos");
  expect(photos?.field_type).toBe("attachments");
  expect(photos?.type_options).toMatchObject({ max_files: 3 });
  photosId = photos?.id ?? "";
  speciesId = sightings.columns.find((c) => c.display_name === "Species")?.id ?? "";

  // A saved attachments field can no longer change type.
  await expect(designView(page).getByRole("row", { name: "Field 3", exact: true }).getByLabel("Data type")).toBeDisabled();

  // History on, so the record's changes can be read back.
  const me = await callApi(page, "POST", `/api/v1/w/${workspaceId(page)}/schema/apply`, {
    ops: [{ op: "set_table_props", table_id: sightings.id, props: { track_history: true } }],
  });
  expect(me.status).toBe(200);
  const created = await callApi(page, "POST", `/api/v1/w/${workspaceId(page)}/tables/${sightings.id}/rows`, { fields: { [speciesId]: "Lantana camara" } });
  expect(created.status).toBe(201);
  rowId = Number((created.body as { id: number }).id);
});

test("datasheet: upload, thumbnails, reorder, viewer, download, remove and the limit", async ({ page }) => {
  test.setTimeout(120_000);
  const requests: string[] = [];
  page.on("request", (r) => requests.push(r.url()));
  await enter(page);
  const grid = await openSightings(page);

  // Enter opens the manager in a dialog; Browse and drag and drop both feed the same input path.
  let dialog = await openPhotosDialog(page, grid);
  await expect(dialog.getByTestId("attachment-count")).toHaveText("0 of 3 files");
  await expect(dialog.getByRole("button", { name: "Browse" })).toBeVisible();
  // Not a phone: no Take photo.
  await expect(dialog.getByTestId("take-photo-input")).toHaveCount(0);

  await dialog.getByTestId("attachment-input").setInputFiles([file("alpha.png", RED), file("bravo.png", GREEN)]);
  await expect(dialog.getByTestId("attachment-tile")).toHaveCount(2);
  await expect(tileNames(dialog)).toHaveText(["alpha.png", "bravo.png"]);
  await expect(dialog.getByTestId("attachment-count")).toHaveText("2 of 3 files");
  await expect(dialog.getByTestId("upload-row")).toHaveCount(0);
  // The thumbnails are real pictures.
  const imgs = dialog.getByTestId("attachment-tile").locator("img");
  await expect(imgs).toHaveCount(2);
  await expect.poll(() => imgs.first().evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true);

  // The limit is 3: of two more files only one is taken.
  await dialog.getByTestId("attachment-input").setInputFiles([file("charlie.png", BLUE), file("delta.png", png(200, 200, 0))]);
  await expect(dialog.getByTestId("attachment-tile")).toHaveCount(3);
  await expect(dialog.getByRole("status").filter({ hasText: "holds at most 3 files" })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Browse" })).toBeDisabled();
  await expect(dialog.getByText("Limit reached: 3 files.")).toBeVisible();

  // Reorder with the keyboard-accessible buttons: alpha, bravo, charlie -> bravo, alpha, charlie.
  const earlier = dialog.getByRole("button", { name: "Move bravo.png earlier" });
  await earlier.focus();
  await page.keyboard.press("Enter");
  await expect(tileNames(dialog)).toHaveText(["bravo.png", "alpha.png", "charlie.png"]);
  await expect(dialog.getByRole("button", { name: "Move bravo.png earlier" })).toBeDisabled();
  await dialog.getByRole("button", { name: "Move charlie.png later" }).isDisabled();
  await expect(dialog.getByRole("button", { name: "Move charlie.png later" })).toBeDisabled();

  // ... and by dragging charlie onto bravo.
  await dialog.getByTestId("attachment-tile").nth(2).dragTo(dialog.getByTestId("attachment-tile").nth(0));
  await expect(tileNames(dialog)).toHaveText(["charlie.png", "bravo.png", "alpha.png"]);

  // Done keeps the changes in the record; Shift+Enter saves it (the normal datasheet path).
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(dialog).toBeHidden();
  const cell = await cellAt(grid, 1, "Photos");
  await expect(cell.getByTestId("attachment-cell")).toContainText("3");
  await expect(cell.locator("img")).toHaveCount(3);
  await page.keyboard.press("Shift+Enter");
  await expect.poll(async () => (await storedPhotos(page)).length).toBe(3);
  const ids = await storedPhotos(page);
  expect(await Promise.all(ids.map((id) => fileNames(page, id)))).toEqual(["charlie.png", "bravo.png", "alpha.png"]);

  // Reopen with F2: the viewer pages through the images with the arrow keys.
  await cell.click();
  await page.keyboard.press("F2");
  dialog = page.getByRole("dialog", { name: "Photos" });
  await expect(tileNames(dialog)).toHaveText(["charlie.png", "bravo.png", "alpha.png"]);
  await dialog.getByRole("button", { name: "View charlie.png" }).click();
  const viewer = page.getByTestId("image-viewer");
  await expect(viewer).toBeVisible();
  await expect(viewer.getByTestId("viewer-position")).toHaveText("1 of 3");
  await expect(viewer.getByTestId("viewer-image")).toHaveAttribute("alt", "charlie.png");
  await expect(viewer.getByRole("button", { name: "Previous image" })).toBeDisabled();
  await page.keyboard.press("ArrowRight");
  await expect(viewer.getByTestId("viewer-position")).toHaveText("2 of 3");
  await expect(viewer.getByTestId("viewer-image")).toHaveAttribute("alt", "bravo.png");
  await viewer.getByRole("button", { name: "Next image" }).click();
  await expect(viewer.getByTestId("viewer-image")).toHaveAttribute("alt", "alpha.png");
  await expect(viewer.getByRole("button", { name: "Next image" })).toBeDisabled();
  await page.keyboard.press("ArrowLeft");
  await expect(viewer.getByTestId("viewer-position")).toHaveText("2 of 3");
  await expect.poll(() => viewer.getByTestId("viewer-image").evaluate((i: HTMLImageElement) => i.naturalWidth)).toBeGreaterThan(0);
  await page.keyboard.press("Escape");
  await expect(viewer).toBeHidden();
  await expect(dialog).toBeVisible();

  // Download returns the bytes that were uploaded.
  const [download] = await Promise.all([page.waitForEvent("download"), dialog.getByRole("link", { name: "Download alpha.png" }).click()]);
  expect(download.suggestedFilename()).toBe("alpha.png");
  const path = await download.path();
  const { readFile } = await import("node:fs/promises");
  expect((await readFile(path)).equals(RED)).toBe(true);

  // Remove one file, finish and save.
  await dialog.getByRole("button", { name: "Remove bravo.png" }).click();
  await expect(dialog.getByTestId("attachment-tile")).toHaveCount(2);
  await dialog.getByRole("button", { name: "Done" }).click();
  await page.keyboard.press("Shift+Enter");
  await expect.poll(async () => (await storedPhotos(page)).length).toBe(2);
  expect(await Promise.all((await storedPhotos(page)).map((id) => fileNames(page, id)))).toEqual(["charlie.png", "alpha.png"]);

  // Cancel throws the changes away.
  dialog = await openPhotosDialog(page, grid);
  await dialog.getByRole("button", { name: "Remove alpha.png" }).click();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  expect((await storedPhotos(page)).length).toBe(2);

  // File details were looked up in batches (GET /files?ids=), never one request per file.
  const perFile = requests.filter((u) => /\/files\/[0-9A-Z]{26}$/.test(new URL(u).pathname));
  const batched = requests.filter((u) => new URL(u).pathname.endsWith("/files") && new URL(u).searchParams.has("ids"));
  expect(perFile).toEqual([]);
  expect(batched.length).toBeGreaterThan(0);
  expect(batched.length).toBeLessThanOrEqual(8);
});

test("datasheet: a refused upload shows its reason, and history lists file names", async ({ page }) => {
  await enter(page);
  const grid = await openSightings(page);
  const ws = wsId;

  // A quota of one byte of file storage makes every upload fail with QUOTA_EXCEEDED.
  const list = await callApi(page, "GET", "/api/v1/admin/workspaces");
  const workspace = (list.body as { items: { id: string; name: string }[] }).items.find((w) => w.name === WS);
  expect(workspace).toBeDefined();
  const patch = await callApi(page, "PATCH", `/api/v1/admin/workspaces/${workspace?.id}`, { quota: { max_file_bytes: 1 } });
  expect(patch.status).toBeLessThan(300);
  // Make room first: drop a file so the field has a free slot.
  let dialog = await openPhotosDialog(page, grid);
  await dialog.getByRole("button", { name: "Remove alpha.png" }).click();
  await dialog.getByTestId("attachment-input").setInputFiles(file("echo.png", png(10, 10, 10, 16)));
  const row = dialog.getByTestId("upload-row");
  await expect(row.getByRole("alert")).toContainText("echo.png");
  await expect(row.getByRole("alert")).toContainText(/quota|storage|limit/i);
  await expect(dialog.getByTestId("attachment-tile")).toHaveCount(1);
  await row.getByRole("button", { name: /Dismiss the error/ }).click();
  await expect(row).toHaveCount(0);
  await callApi(page, "PATCH", `/api/v1/admin/workspaces/${workspace?.id}`, { quota: { max_file_bytes: 0 } });

  // The same upload works again once the quota is lifted; then save.
  await dialog.getByTestId("attachment-input").setInputFiles(file("echo.png", png(10, 10, 10, 16)));
  await expect(dialog.getByTestId("attachment-tile")).toHaveCount(2);
  await dialog.getByRole("button", { name: "Done" }).click();
  await page.keyboard.press("Shift+Enter");
  await expect.poll(async () => (await storedPhotos(page)).length).toBe(2);

  // Row history names the files instead of printing [object Object] or raw ids.
  await page.getByRole("button", { name: "History", exact: true }).click();
  const panel = page.getByRole("dialog", { name: /History of record/ });
  await expect(panel).toBeVisible();
  await expect(panel.getByTestId("history-change").first()).toContainText("echo.png");
  await expect(panel).not.toContainText("[object Object]");
  await expect(panel).not.toContainText(/[0-9A-HJKMNP-TV-Z]{26}/);
  await panel.getByRole("button", { name: "Close history" }).click();
  expect(ws).toBeTruthy();
});

test("a viewer sees the files but cannot change them", async ({ page }) => {
  const ws = wsId;
  await enter(page);
  const second = `viewer-${Date.now()}@example.com`;
  await apiPost(page, "/api/v1/admin/users", { email: second, display_name: "Vera Viewer", password: "viewer-password-1" });
  await apiPost(page, `/api/v1/w/${ws}/members`, { email: second, role: "viewer" });
  await page.context().clearCookies();
  await signInAs(page, second, "viewer-password-1");
  await page.goto(`/w/${ws}`);
  const grid = await openSightings(page);
  const cell = await cellAt(grid, 1, "Photos");
  await cell.click();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Photos" });
  await expect(dialog.getByTestId("attachment-tile")).toHaveCount(2);
  await expect(dialog.getByRole("button", { name: "Browse" })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: /^Remove / })).toHaveCount(0);
  await expect(dialog.getByRole("link", { name: /^Download / }).first()).toBeVisible();
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(dialog).toBeHidden();
});

test("form: an Attachments control shows the gallery and edits it when the form allows edits", async ({ page }) => {
  await enter(page);
  const ws = wsId;
  const empty = { height: 0, controls: [] };
  const box = { w: 0, h: 24, tab_index: 0 };
  const body = (name: string, allowEdits: boolean) => ({
    name,
    source_kind: "table",
    source_id: sightings.id,
    default_view: "single",
    module_source: "",
    allow_edits: allowEdits,
    allow_additions: false,
    allow_deletions: false,
    layout: {
      width: 600,
      sections: {
        header: empty,
        footer: empty,
        detail: {
          height: 280,
          controls: [
            { ...box, id: "f1", name: "SpeciesLabel", type: "label", x: 0, y: 0, w: 90, caption: "Species" },
            { ...box, id: "f2", name: "Species", type: "textbox", x: 100, y: 0, w: 240, tab_index: 1, bind: { column_id: speciesId } },
            { ...box, id: "f3", name: "PhotosLabel", type: "label", x: 0, y: 40, w: 90, caption: "Photos" },
            { ...box, id: "f4", name: "Photos", type: "attachments", x: 100, y: 40, w: 460, h: 220, tab_index: 2, bind: { column_id: photosId } },
          ],
        },
      },
    },
  });
  const editable = await apiPost<{ id: string; name: string }>(page, `/api/v1/w/${ws}/forms`, body("Sighting form", true));
  const readOnly = await apiPost<{ id: string; name: string }>(page, `/api/v1/w/${ws}/forms`, body("Sighting viewer", false));
  // The server refuses an Attachments control that is bound to a field of another type.
  const bad = body("Bad form", true);
  bad.layout.sections.detail.controls[3] = { ...bad.layout.sections.detail.controls[3], bind: { column_id: speciesId } } as never;
  const refused = await callApi(page, "POST", `/api/v1/w/${ws}/forms`, bad);
  expect(refused.status).toBe(400);

  await page.reload();
  await openForm(page, ws, editable);
  const form = page.getByRole("group", { name: "Sighting form", exact: true });
  const gallery = form.getByRole("region", { name: "Photos attachments" });
  await expect(gallery.getByTestId("attachment-tile")).toHaveCount(2);
  await expect(gallery.getByTestId("attachment-count")).toHaveText("2 of 3 files");

  await gallery.getByTestId("attachment-input").setInputFiles(file("foxtrot.png", png(120, 0, 120)));
  await expect(gallery.getByTestId("attachment-tile")).toHaveCount(3);
  await expect(form.getByText("Unsaved changes")).toBeVisible();
  await gallery.getByRole("button", { name: "Move foxtrot.png earlier" }).click();
  await form.getByRole("toolbar").getByRole("button", { name: "Save", exact: true }).click();
  await expect(form.getByText("Unsaved changes")).toBeHidden();
  const ids = await storedPhotos(page);
  expect(await Promise.all(ids.map((id) => fileNames(page, id)))).toEqual(["charlie.png", "foxtrot.png", "echo.png"]);

  // The form that does not allow edits shows the gallery without any way to change it.
  await page.getByRole("tab", { name: "Sighting form" }).waitFor();
  await openForm(page, ws, readOnly);
  const view = page.getByRole("group", { name: "Sighting viewer", exact: true }).getByRole("region", { name: "Photos attachments" });
  await expect(view.getByTestId("attachment-tile")).toHaveCount(3);
  await expect(view.getByRole("button", { name: "Browse" })).toHaveCount(0);
  await expect(view.getByRole("button", { name: /^Remove / })).toHaveCount(0);

  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await expectNoAxeViolations(page);
  }
  await page.emulateMedia({ colorScheme: "light" });
});

test("form designer: the Attachments control is in the toolbox and binds an Attachments field", async ({ page }) => {
  const ws = wsId;
  await enter(page);
  await nav(page).getByRole("button", { name: "New form" }).click();
  const setup = page.getByRole("form", { name: "New form setup" });
  await setup.getByLabel("Form name").fill("Designed sighting form");
  await setup.getByLabel("Record source type").selectOption("table");
  await setup.getByLabel("Record source", { exact: true }).selectOption({ label: "Sightings" });
  await setup.getByRole("button", { name: "Start designing" }).click();
  const root = page.getByTestId("form-designer");

  // Adding the Attachments field from the field list makes an Attachments control bound to it.
  await root.getByRole("region", { name: "Field list" }).getByRole("button", { name: "Add Photos to form" }).click();
  await expect(page.getByTestId("form-canvas").getByRole("button", { name: /^Attachments Photos, x \d+ y \d+$/ })).toBeVisible();

  // The toolbox offers the control too; its property sheet only binds Attachments fields.
  await root.getByRole("region", { name: "Toolbox" }).getByRole("button", { name: "Attachments", exact: true }).click();
  const props = page.getByRole("form", { name: /^Properties of Attachments/ });
  await expect(props).toBeVisible();
  expect(await props.getByLabel("Bound field").locator("option").allInnerTexts()).toEqual(["Choose a field", "Photos"]);
  await expect(props.getByLabel("Control source")).toHaveCount(0);
  await props.getByLabel("Bound field").selectOption({ label: "Photos" });
  await page.keyboard.press("ControlOrMeta+S");
  await expect.poll(async () => {
    const res = await page.request.get(`/api/v1/w/${ws}/forms`);
    const forms = ((await res.json()) as { items: { name: string; layout: { sections: { detail: { controls: { type: string; bind?: { column_id: string } }[] } } } }[] }).items;
    const f = forms.find((x) => x.name === "Designed sighting form");
    return f?.layout.sections.detail.controls.filter((c) => c.type === "attachments" && c.bind?.column_id === photosId).length;
  }).toBe(2);
});

test("report: an Image control prints the first image, or a gallery of up to N", async ({ page }) => {
  const ws = wsId;
  await enter(page);
  const ctl = (id: string, type: string, x: number, w: number, h: number, extra: Record<string, unknown> = {}) => ({ id, name: id, type, x, y: 0, w, h, ...extra });
  const report = (name: string, max?: number) => ({
    name,
    source_kind: "table",
    source_id: sightings.id,
    page_setup: { paper: "letter", orientation: "portrait", margin_top: 48, margin_bottom: 48, margin_left: 48, margin_right: 48 },
    layout: {
      width: 700,
      bands: {
        report_header: { height: 0, controls: [] },
        page_header: { height: 0, controls: [] },
        detail: {
          height: 140,
          controls: [
            ctl("sp", "textbox", 0, 200, 20, { bind: { column_id: speciesId } }),
            ctl("photo", "image", 220, 300, 130, { bind: { column_id: photosId }, ...(max ? { max_images: max } : {}) }),
          ],
        },
        page_footer: { height: 0, controls: [] },
        report_footer: { height: 0, controls: [] },
      },
      groups: [],
      sorts: [],
    },
  });
  await createReportViaApi(page, report("Photo sheet"));
  await createReportViaApi(page, report("Photo gallery", 2));
  // The server refuses an image bound to a field that is not an attachments field.
  const bad = report("Bad image");
  bad.layout.bands.detail.controls[1] = ctl("photo", "image", 220, 300, 130, { bind: { column_id: speciesId } });
  expect((await callApi(page, "POST", `/api/v1/w/${ws}/reports`, bad)).status).toBe(400);
  await page.reload();

  await openReport(page, "Photo sheet");
  let images = page.getByTestId("report-image").locator("img");
  await expect(images).toHaveCount(1);
  await expect.poll(() => images.first().evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true);
  expect(await images.first().getAttribute("src")).toMatch(new RegExp(`/api/v1/w/${ws}/files/[0-9A-Z]{26}/content$`));

  await openReport(page, "Photo gallery");
  images = page.getByTestId("report-image").locator("img");
  await expect(images).toHaveCount(2);
  await expect.poll(() => images.nth(1).evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true);

  // Print media: the pictures are in the printed pages.
  await page.emulateMedia({ media: "print" });
  await expect(page.getByTestId("report-print-root").locator("img").first()).toBeVisible();
  await page.emulateMedia({ media: "screen" });
});

test("report designer: the Image control binds an Attachments field and sets the number of images", async ({ page }) => {
  const ws = wsId;
  await enter(page);
  await nav(page).getByRole("button", { name: "New report" }).click();
  const setup = page.getByRole("form", { name: "New report setup" });
  await setup.getByLabel("Report name").fill("Designed photo report");
  await setup.getByLabel("Record source type").selectOption("table");
  await setup.getByLabel("Record source", { exact: true }).selectOption({ label: "Sightings" });
  await setup.getByRole("button", { name: "Start designing" }).click();
  const root = page.getByTestId("report-designer");

  // The Attachments field is added as an Image control.
  await root.getByRole("region", { name: "Field list" }).getByRole("button", { name: "Add Photos to band" }).click();
  await expect(page.getByTestId("report-canvas").getByRole("button", { name: /^Image Photos, x \d+ y \d+$/ })).toBeVisible();
  const props = page.getByRole("form", { name: /^Properties of Photos/ });
  await expect(props.getByLabel("Attachments field")).toHaveValue(/.+/);
  expect(await props.getByLabel("Attachments field").locator("option").allInnerTexts()).toEqual(["Choose a field", "Photos"]);
  await props.getByLabel("Images shown").fill("3");
  await page.keyboard.press("ControlOrMeta+S");
  await expect.poll(async () => {
    const res = await page.request.get(`/api/v1/w/${ws}/reports`);
    const reports = ((await res.json()) as { items: { name: string; layout: { bands: { detail: { controls: { type: string; max_images?: number }[] } } } }[] }).items;
    return reports.find((r) => r.name === "Designed photo report")?.layout.bands.detail.controls.find((c) => c.type === "image")?.max_images;
  }).toBe(3);
});

test("phone: Take photo uses the camera input and the record sheet shows the gallery", async ({ page }) => {
  const ws = wsId;
  await page.setViewportSize({ width: 390, height: 844 });
  await signIn(page);
  await page.goto(`/w/${ws}`);
  await page.getByRole("button", { name: "Open navigation" }).click();
  await nav(page).getByRole("button", { name: "Sightings", exact: true }).click();
  await page.getByRole("list", { name: "Sightings records" }).getByRole("button").first().click();

  const sheet = page.getByRole("dialog");
  const gallery = sheet.getByRole("region", { name: "Photos attachments" });
  await expect(gallery).toBeVisible();
  await expect(gallery.getByTestId("attachment-tile")).toHaveCount(3);
  // The field is full (3 of 3): remove one so a photo can be taken.
  await gallery.getByRole("button", { name: /^Remove / }).first().click();
  const take = gallery.getByRole("button", { name: "Take photo" });
  await expect(take).toBeVisible();
  const input = gallery.getByTestId("take-photo-input");
  await expect(input).toHaveAttribute("capture", "environment");
  await expect(input).toHaveAttribute("accept", "image/*");
  await expect(input).toHaveAttribute("type", "file");

  await input.setInputFiles(file("camera.png", png(5, 90, 200, 12)));
  await expect(gallery.getByTestId("attachment-tile")).toHaveCount(3);
  await expect(gallery.locator("p.truncate", { hasText: "camera.png" })).toBeVisible();
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await expectNoAxeViolations(page);
  }
  await page.emulateMedia({ colorScheme: "light" });
  await sheet.getByRole("button", { name: "Save", exact: true }).click();
  await expect(sheet).toBeHidden();
  const ids = await storedPhotos(page);
  expect(ids).toHaveLength(3);
  expect(await Promise.all(ids.map((id) => fileNames(page, id)))).toContain("camera.png");
  await page.setViewportSize({ width: 1280, height: 720 });
});

test("axe: the attachments dialog, the designer and the viewer are clean in light and dark", async ({ page }) => {
  const ws = wsId;
  await enter(page);
  const grid = await openSightings(page);
  const dialog = await openPhotosDialog(page, grid);
  await expect(dialog.getByTestId("attachment-tile").first()).toBeVisible();
  // A scan with a running upload would be flaky; the idle manager (dropzone, tiles, notice) is what is scanned.
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await expectNoAxeViolations(page);
  }
  await dialog.getByRole("button", { name: /^View / }).first().click();
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await expectNoAxeViolations(page);
  }
  await page.getByTestId("image-viewer").getByRole("button", { name: "Close viewer" }).click();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  await page.emulateMedia({ colorScheme: "light" });
});

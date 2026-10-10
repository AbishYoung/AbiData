import { expect, test, type Page } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD, expectNoAxeViolations } from "./helpers";
import { createWorkspace, fetchSchema, nav } from "./tabledesign-helpers";
import { EDITOR_EMAIL, EDITOR_PASSWORD, addEditorMember, signInAs } from "./queries-helpers";
import { createSales } from "./reports-helpers";
import {
  REPORT_NAME,
  WORKSPACE,
  addTool,
  canvasControl,
  controlPos,
  designerRoot,
  listReportsApi,
  openPanel,
  properties,
  readAllPages,
  selectBand,
} from "./reports-designer-helpers";

test.describe.configure({ mode: "serial" });

async function openWs(page: Page, email = ADMIN_EMAIL, password = ADMIN_PASSWORD): Promise<void> {
  await signInAs(page, email, password);
  await page.getByRole("link", { name: new RegExp(WORKSPACE) }).click();
  await expect(page).toHaveURL(/\/w\//);
}

test("builder designs a grouped report, saves it with Mod+S, reopens it and runs it", async ({ page }) => {
  test.setTimeout(120_000);
  await createWorkspace(page, WORKSPACE);
  await createSales(page);
  await page.reload();

  // Setup step from the Reports group's plus button.
  await nav(page).getByRole("button", { name: "New report" }).click();
  const setup = page.getByRole("form", { name: "New report setup" });
  await setup.getByLabel("Report name").fill(REPORT_NAME);
  await setup.getByLabel("Record source type").selectOption("table");
  await setup.getByLabel("Record source", { exact: true }).selectOption({ label: "Sales" });
  await setup.getByLabel("Paper").selectOption("letter");
  await setup.getByLabel("Orientation").selectOption("portrait");
  await setup.getByLabel("Margins (px)").fill("48");
  await setup.getByRole("button", { name: "Start designing" }).click();
  const root = designerRoot(page);
  await expect(root.getByRole("region", { name: "Field list" }).getByRole("button", { name: "Add Amount to band" })).toBeVisible();
  await expect(root.getByTestId("width-guide-label")).toHaveText("Page content width 720 px");

  // Bands before any group: the fixed five.
  const bandNames = () => root.getByRole("group", { name: / band$/ }).evaluateAll((els) => els.map((e) => e.getAttribute("aria-label")));
  expect(await bandNames()).toEqual(["Report header band", "Page header band", "Detail band", "Page footer band", "Report footer band"]);

  // Group on Region: its header and footer bands appear around the detail.
  const groups = root.getByRole("region", { name: "Groups and sorting" });
  await groups.getByRole("button", { name: "Add group" }).click();
  await groups.getByLabel("Group 1 field").selectOption({ label: "Region" });
  expect(await bandNames()).toEqual([
    "Report header band",
    "Page header band",
    "Group header 1 band",
    "Detail band",
    "Group footer 1 band",
    "Page footer band",
    "Report footer band",
  ]);

  // Group header: the region name.
  await selectBand(page, "Group header 1");
  await root.getByRole("region", { name: "Field list" }).getByRole("button", { name: "Add Region to band" }).click();
  await expect(canvasControl(page, "Textbox", "Region")).toBeVisible();

  // Group footer: a label and a sum aggregate scoped to the group.
  await selectBand(page, "Group footer 1");
  await addTool(page, "Label");
  await properties(page).getByLabel("Name", { exact: true }).fill("TotalLabel");
  await properties(page).getByLabel("Caption").fill("Region total");
  await addTool(page, "Textbox");
  await properties(page).getByLabel("Name", { exact: true }).fill("RegionTotal");
  await properties(page).getByLabel("Control source").selectOption("aggregate");
  await properties(page).getByLabel("Function").selectOption("sum");
  await properties(page).getByLabel("Aggregate field").selectOption({ label: "Amount" });
  await expect(properties(page).getByLabel("Scope")).toHaveValue("group");
  await properties(page).getByLabel("Format").selectOption("currency");
  await expect(canvasControl(page, "Textbox", "RegionTotal")).toBeVisible();

  // Detail: fields add a bound textbox here and a caption label in the page header.
  await selectBand(page, "Detail");
  const fields = root.getByRole("region", { name: "Field list" });
  await fields.getByRole("button", { name: "Add Rep to band" }).click();
  await fields.getByRole("button", { name: "Add Amount to band" }).click();
  await expect(canvasControl(page, "Textbox", "Rep")).toBeVisible();
  await expect(canvasControl(page, "Textbox", "Amount")).toBeVisible();
  await expect(page.getByTestId("band-page_header").getByRole("button", { name: /^Label Rep_Label, x 0 y 0$/ })).toBeVisible();
  await expect(page.getByTestId("band-detail").getByRole("button", { name: /^Textbox Rep, x 0 y 0$/ })).toBeVisible();

  // A calculated textbox in the detail band.
  await addTool(page, "Textbox");
  await properties(page).getByLabel("Name", { exact: true }).fill("Doubled");
  await properties(page).getByLabel("Control source").selectOption("calc");
  await properties(page).getByLabel("Lua expression").fill("Amount * 2");
  await expect(canvasControl(page, "Textbox", "Doubled")).toBeVisible();

  // Band properties: the detail band can grow.
  await selectBand(page, "Detail");
  await page.getByRole("form", { name: "Properties of Detail band" }).getByLabel("Can grow").check();
  await expect(root.getByRole("button", { name: "Select Detail band" })).toContainText("can grow");

  // A page number in the page footer.
  await selectBand(page, "Page footer");
  await addTool(page, "Page number");
  await expect(canvasControl(page, "Page number", "PageNumber")).toBeVisible();

  // Keyboard only: choose a band, add a control with Enter, nudge it with the arrow keys.
  await root.getByRole("button", { name: "Select Report header band" }).focus();
  await page.keyboard.press("Enter");
  await expect(root.getByTestId("toolbox-target")).toHaveText("Adds to: Report header");
  await root.getByRole("region", { name: "Toolbox" }).getByRole("button", { name: "Label", exact: true }).focus();
  await page.keyboard.press("Enter");
  const title = canvasControl(page, "Label", "Label");
  await expect(title).toBeFocused();
  const p0 = await controlPos(title);
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowDown");
  await expect(canvasControl(page, "Label", "Label")).toHaveAttribute("aria-label", `Label Label, x ${p0.x + 16} y ${p0.y + 8}`);
  await properties(page).getByLabel("Caption").fill("Sales by region");

  // Axe on the layout (control selected, then band and groups panels) and the other panels.
  await expectNoAxeViolations(page);
  await canvasControl(page, "Textbox", "RegionTotal").click();
  await expectNoAxeViolations(page);
  await selectBand(page, "Detail");
  await expectNoAxeViolations(page);
  await openPanel(page, "Module");
  await expect(root.getByRole("textbox", { name: "Lua source of report module" })).toBeAttached();
  await expectNoAxeViolations(page);
  await openPanel(page, "Preview");
  await expect(root.getByRole("button", { name: "Save to preview" })).toBeVisible();
  await expectNoAxeViolations(page);
  await openPanel(page, "Layout");

  // Save with Mod+S.
  await expect(root.getByText("Unsaved changes")).toBeVisible();
  await canvasControl(page, "Textbox", "Rep").focus();
  await page.keyboard.press("ControlOrMeta+S");
  await expect(root.getByText("Saved", { exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: REPORT_NAME })).toBeVisible();
  await expect(page.getByRole("tab", { name: REPORT_NAME })).not.toContainText("unsaved");
  await expect(nav(page).getByRole("button", { name: REPORT_NAME, exact: true })).toBeVisible();

  const schema = await fetchSchema(page);
  const sales = schema.find((t) => t.display_name === "Sales");
  const col = (n: string) => sales?.columns.find((c) => c.display_name === n)?.id;
  const saved = (await listReportsApi(page)).find((r) => r.name === REPORT_NAME);
  expect(saved?.source_id).toBe(sales?.id);
  expect(saved?.page_setup).toMatchObject({ paper: "letter", orientation: "portrait", margin_left: 48, margin_right: 48 });
  expect(saved?.layout.width).toBe(720);
  expect(saved?.layout.groups).toHaveLength(1);
  expect(saved?.layout.groups?.[0]?.column_id).toBe(col("Region"));
  const total = saved?.layout.groups?.[0]?.footer.controls?.find((c) => c.name === "RegionTotal");
  expect(total?.aggregate).toEqual({ aggregate: "sum", column_id: col("Amount"), scope: "group" });
  expect(total?.format).toBe("currency");
  expect(saved?.layout.groups?.[0]?.header.controls?.map((c) => c.bind?.column_id)).toEqual([col("Region")]);
  expect(saved?.layout.bands.detail.can_grow).toBe(true);
  expect(saved?.layout.bands.detail.controls?.find((c) => c.name === "Doubled")?.calc?.lua_expr).toBe("Amount * 2");
  expect(saved?.layout.bands.page_footer.controls?.map((c) => c.type)).toEqual(["page_number"]);
  expect(saved?.layout.bands.page_header.controls?.map((c) => c.caption).sort()).toEqual(["Amount", "Rep"]);
  expect(saved?.layout.bands.report_header.controls?.[0]?.caption).toBe("Sales by region");

  // Preview now runs the saved report through the viewer.
  await openPanel(page, "Preview");
  await expect(page.getByTestId("report-page-of")).toBeVisible();
  await openPanel(page, "Layout");

  // Close and reopen in Design: the layout persisted.
  await page.getByRole("button", { name: `Close ${REPORT_NAME}` }).click();
  await expect(designerRoot(page)).toHaveCount(0);
  await nav(page).getByRole("button", { name: `More actions for ${REPORT_NAME}` }).click();
  await page.getByRole("menuitem", { name: "Design" }).click();
  await expect(canvasControl(page, "Textbox", "RegionTotal")).toBeVisible();
  await expect(canvasControl(page, "Textbox", "Doubled")).toBeVisible();
  await expect(canvasControl(page, "Page number", "PageNumber")).toBeVisible();
  expect(await bandNames()).toContain("Group footer 1 band");
  await expect(root.getByRole("button", { name: "Select Detail band" })).toContainText("can grow");
  await canvasControl(page, "Textbox", "RegionTotal").click();
  await expect(properties(page).getByLabel("Control source")).toHaveValue("aggregate");
  await expect(properties(page).getByLabel("Aggregate field")).toHaveValue(col("Amount") ?? "");
  await expect(root.getByText("Saved", { exact: true })).toBeVisible();

  // Report view: grouped output with the group totals.
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Report" }).click();
  await expect(page.getByTestId("report-page-of")).toBeVisible();
  const text = (await readAllPages(page)).join("\n");
  for (const region of ["North", "South", "East"]) expect(text).toContain(region);
  expect(text.match(/Region total/g)).toHaveLength(3);
  expect(text.match(/300\.00/g)).toHaveLength(3);
  expect(text).not.toContain("#Error");

  await addEditorMember(page);
});

test("a page change moves the width guide and flags controls past it", async ({ page }) => {
  await openWs(page);
  await nav(page).getByRole("button", { name: `More actions for ${REPORT_NAME}` }).click();
  await page.getByRole("menuitem", { name: "Design" }).click();
  const root = designerRoot(page);
  await expect(root.getByTestId("width-guide-label")).toHaveText("Page content width 720 px");

  // Wider left margins narrow the printable area.
  const props = root.getByRole("form", { name: "Report properties" });
  await props.getByLabel("Left margin").fill("200");
  await expect(root.getByTestId("width-guide-label")).toHaveText("Page content width 568 px");
  await props.getByLabel("Left margin").fill("300");
  await props.getByLabel("Right margin").fill("300");
  await expect(root.getByTestId("width-guide-label")).toHaveText("Page content width 216 px");
  const warnings = root.getByRole("region", { name: "Layout warnings" });
  await expect(warnings).toContainText("extends past the page content width (216 px)");
  await expectNoAxeViolations(page);

  // Not saved: leave the tab with the original page setup.
  await props.getByLabel("Left margin").fill("48");
  await props.getByLabel("Right margin").fill("48");
  await expect(root.getByTestId("width-guide-label")).toHaveText("Page content width 720 px");
  await expect(root.getByText("Saved", { exact: true })).toBeVisible();
});

test("an editor sees the report without design actions", async ({ page }) => {
  await openWs(page, EDITOR_EMAIL, EDITOR_PASSWORD);
  await expect(nav(page).getByRole("button", { name: "New report" })).toHaveCount(0);
  await nav(page).getByRole("button", { name: `More actions for ${REPORT_NAME}` }).click();
  await expect(page.getByRole("menuitem")).toHaveText(["Open"]);
  await page.keyboard.press("Escape");
  await nav(page).getByRole("button", { name: REPORT_NAME, exact: true }).click();
  await expect(page.getByTestId("report-page-of")).toBeVisible();
  await expect(page.getByRole("group", { name: "View" })).toHaveCount(0);
  await expect(designerRoot(page)).toHaveCount(0);
  await expectNoAxeViolations(page);
});

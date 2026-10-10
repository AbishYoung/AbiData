import { expect, test } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD, signOut } from "./helpers";
import { createWorkspace, expectDesignViewAccessible, nav, workspaceId } from "./tabledesign-helpers";
import { EDITOR_EMAIL, EDITOR_PASSWORD, addEditorMember, createSampleData, signInAs } from "./queries-helpers";
import {
  FORM_NAME,
  WORKSPACE,
  canvasControl,
  controlX,
  designerRoot,
  dragTo,
  listFormsApi,
  openPanel,
  properties,
} from "./forms-designer-helpers";

test.describe.configure({ mode: "serial" });

async function openWs(page: import("@playwright/test").Page, email = ADMIN_EMAIL, password = ADMIN_PASSWORD): Promise<void> {
  await signInAs(page, email, password);
  await page.getByRole("link", { name: new RegExp(WORKSPACE) }).click();
  await expect(page).toHaveURL(/\/w\//);
}

test("builder designs an order form, saves it with Mod+S and sees it persisted", async ({ page }) => {
  await createWorkspace(page, WORKSPACE);
  await createSampleData(page);
  await page.reload();
  await expect(nav(page).getByRole("button", { name: "Orders", exact: true })).toBeVisible();

  // Setup step.
  await nav(page).getByRole("button", { name: "New form" }).click();
  const setup = page.getByRole("form", { name: "New form setup" });
  await setup.getByLabel("Form name").fill(FORM_NAME);
  await setup.getByLabel("Record source type").selectOption("table");
  await setup.getByLabel("Record source", { exact: true }).selectOption({ label: "Orders" });
  await setup.getByRole("button", { name: "Start designing" }).click();
  const root = designerRoot(page);
  const fieldList = root.getByRole("region", { name: "Field list" });
  await expect(fieldList.getByRole("button", { name: "Add Qty to form" })).toBeVisible();

  // Drag a field onto the detail section with the mouse.
  await dragTo(page, fieldList.getByText(/^Qty/), page.getByTestId("section-detail"), { x: 40, y: 100 });
  await expect(canvasControl(page, "Textbox", "Qty")).toBeVisible();
  await expect(canvasControl(page, "Label", "Qty_Label")).toBeVisible();
  await page.waitForTimeout(300); // dnd-kit swallows clicks for a moment after a drag

  // Keyboard path: "Add to form" for a lookup column prefills a combobox.
  await fieldList.getByRole("button", { name: "Add Customer to form" }).focus();
  await expect(fieldList.getByRole("button", { name: "Add Customer to form" })).toBeFocused();
  await page.keyboard.press("Enter");
  const customer = canvasControl(page, "Combobox", "Customer");
  await expect(customer).toBeVisible();
  await expect(properties(page).getByLabel("Row source", { exact: true })).toHaveText(/Customers/);

  // A calculated textbox.
  await root.getByRole("region", { name: "Toolbox" }).getByRole("button", { name: "Textbox", exact: true }).click();
  await properties(page).getByLabel("Name", { exact: true }).fill("Doubled");
  await properties(page).getByLabel("Control source").selectOption("calc");
  await properties(page).getByLabel("Lua expression").fill("record.Qty * 2");
  const doubled = canvasControl(page, "Textbox", "Doubled");
  await expect(doubled).toBeVisible();

  // A button with a property set.
  await root.getByRole("region", { name: "Toolbox" }).getByRole("button", { name: "Button", exact: true }).click();
  await properties(page).getByLabel("Name", { exact: true }).fill("SaveBtn");
  await properties(page).getByLabel("Caption").fill("Save order");
  await expect(canvasControl(page, "Button", "SaveBtn")).toBeVisible();

  // Module handler, then wire it to the button's click event.
  await openPanel(page, "Module");
  await expect(root.getByRole("textbox", { name: "Lua source of form module" })).toBeAttached();
  await root.getByLabel("Plain text editor").check();
  await root.getByRole("textbox", { name: "Lua source of form module" }).fill("function onSave()\nend\n");
  await expect(root.getByText("Functions found: onSave.")).toBeVisible();
  await openPanel(page, "Layout");
  await canvasControl(page, "Button", "SaveBtn").click();
  await properties(page).getByLabel("On click").fill("onSave");
  await expect(properties(page).getByText("No function named")).toHaveCount(0);

  // Keyboard nudge and Shift+click multi-select with Align left.
  const button = canvasControl(page, "Button", "SaveBtn");
  const x0 = await controlX(button);
  await button.focus();
  await page.keyboard.press("ArrowRight");
  await expect(canvasControl(page, "Button", "SaveBtn")).toHaveAttribute("aria-label", new RegExp(`x ${x0 + 8} `));
  await canvasControl(page, "Textbox", "Doubled").click();
  await canvasControl(page, "Button", "SaveBtn").click({ modifiers: ["Shift"] });
  await expect(root.getByText("2 controls selected")).toBeVisible();
  await root.getByRole("button", { name: "Align left" }).click();
  expect(await controlX(canvasControl(page, "Textbox", "Doubled"))).toBe(await controlX(canvasControl(page, "Button", "SaveBtn")));

  // Axe on the layout, module and preview panels.
  await expectDesignViewAccessible(page);
  await openPanel(page, "Module");
  await expectDesignViewAccessible(page);
  await openPanel(page, "Preview");
  await expect(root.getByRole("textbox", { name: "Qty", exact: true })).toHaveValue("1");
  await expectDesignViewAccessible(page);
  await openPanel(page, "Layout");

  // Save with Mod+S.
  await expect(root.getByText("Unsaved changes")).toBeVisible();
  await canvasControl(page, "Textbox", "Qty").focus();
  await page.keyboard.press("ControlOrMeta+S");
  await expect(root.getByText("Saved", { exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: FORM_NAME })).toBeVisible();
  await expect(page.getByRole("tab", { name: FORM_NAME })).not.toContainText("unsaved");
  await expect(nav(page).getByRole("button", { name: FORM_NAME, exact: true })).toBeVisible();

  const forms = await listFormsApi(page);
  const saved = forms.find((f) => f.name === FORM_NAME);
  const controls = saved?.layout.sections.detail.controls ?? [];
  expect(controls.map((c) => c.name).sort()).toEqual(["Customer", "Customer_Label", "Doubled", "Qty", "Qty_Label", "SaveBtn"]);
  expect(controls.find((c) => c.name === "Doubled")?.calc?.lua_expr).toBe("record.Qty * 2");
  expect(controls.find((c) => c.name === "SaveBtn")?.events).toEqual({ click: "onSave" });
  expect(controls.find((c) => c.name === "Customer")?.lookup?.table_id).toBeTruthy();

  // Close and reopen in Design view: the layout persisted.
  await page.getByRole("button", { name: `Close ${FORM_NAME}` }).click();
  await expect(designerRoot(page)).toHaveCount(0);
  await nav(page).getByRole("button", { name: `More actions for ${FORM_NAME}` }).click();
  await page.getByRole("menuitem", { name: "Design view" }).click();
  await expect(canvasControl(page, "Textbox", "Qty")).toBeVisible();
  await expect(canvasControl(page, "Combobox", "Customer")).toBeVisible();
  await expect(canvasControl(page, "Textbox", "Doubled")).toBeVisible();
  await canvasControl(page, "Button", "SaveBtn").click();
  await expect(properties(page).getByLabel("On click")).toHaveValue("onSave");
  await openPanel(page, "Module");
  await root.getByLabel("Plain text editor").check();
  await expect(root.getByRole("textbox", { name: "Lua source of form module" })).toHaveValue(/function onSave\(\)/);
  await addEditorMember(page);
});

test("builder renames and an editor sees forms without design actions; then the builder deletes", async ({ page }) => {
  await openWs(page);

  // Rename from the context menu keeps the module.
  await nav(page).getByRole("button", { name: FORM_NAME, exact: true }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "Rename" }).click();
  const dialog = page.getByRole("dialog", { name: "Rename form" });
  await dialog.getByLabel("Form name").fill("Order entry v2");
  await dialog.getByRole("button", { name: "Rename" }).click();
  await expect(dialog).toBeHidden();
  await expect(nav(page).getByRole("button", { name: "Order entry v2", exact: true })).toBeVisible();
  const forms = await listFormsApi(page);
  expect(forms.map((f) => f.name)).toEqual(["Order entry v2"]);
  const res = await page.request.get(`/api/v1/w/${workspaceId(page)}/forms/${forms[0]?.id ?? ""}`);
  expect((await res.json()).module_source).toContain("function onSave()");
  await signOut(page);

  // Editor: sees the form, opens it, no design actions.
  await openWs(page, EDITOR_EMAIL, EDITOR_PASSWORD);
  await expect(nav(page).getByRole("button", { name: "New form" })).toHaveCount(0);
  await nav(page).getByRole("button", { name: "More actions for Order entry v2" }).click();
  await expect(page.getByRole("menuitem")).toHaveText(["Open"]);
  await page.keyboard.press("Escape");
  await nav(page).getByRole("button", { name: "Order entry v2", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Order entry v2" })).toBeVisible();
  await expect(page.getByRole("group", { name: "View" })).toHaveCount(0);
  await expect(designerRoot(page)).toHaveCount(0);
  await page.getByRole("button", { name: /Edith/ }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);


  // Builder deletes it from the menu.
  await openWs(page);
  await nav(page).getByRole("button", { name: "Order entry v2", exact: true }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete form" }).click();
  await expect(nav(page).getByRole("button", { name: "Order entry v2", exact: true })).toHaveCount(0);
  expect(await listFormsApi(page)).toEqual([]);
});

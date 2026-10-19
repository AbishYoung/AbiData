import { randomUUID } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { expectNoAxeViolations, settleAnimations, signIn } from "./helpers";
import { apiPost } from "./queries-helpers";

// The blog authoring UI (ADR 0017): every user has a blog workspace with its own editor.
test.describe.configure({ mode: "serial" });

const RUN = Date.now().toString(36);
const EMAIL = `blogger-${RUN}@example.com`;
const NAME = "Bea Blogger";
const PASSWORD = `pw-${randomUUID()}`;
const TITLE = `Hello from ${RUN}`;

async function signInAsBlogger(page: Page): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email").fill(EMAIL);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
}

/**
 * axe on the editor without the preview frame: the frame runs no scripts (by design), so axe
 * cannot be injected into it and would wait forever. The rendered post is checked on the public page.
 */
async function expectEditorAccessible(page: Page): Promise<void> {
  await settleAnimations(page);
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).exclude('iframe[title="Preview"]').analyze();
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
}

test("a user writes, previews and publishes a post", async ({ page }) => {
  await signIn(page);
  await apiPost(page, "/api/v1/admin/users", { email: EMAIL, display_name: NAME, password: PASSWORD });
  await page.getByRole("button", { name: /Admin/ }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await signInAsBlogger(page);

  // The account menu leads to the blog; a new blog is empty and off.
  await page.getByRole("button", { name: NAME }).click();
  await page.getByRole("menuitem", { name: "Blog" }).click();
  await expect(page).toHaveURL(/\/w\/[^/]+\/blog$/);
  await expect(page.getByText("No posts yet")).toBeVisible();
  await expect(page.getByText(/Your blog is off/)).toBeVisible();
  await expectNoAxeViolations(page);

  await page.getByRole("link", { name: "Write your first post" }).click();
  await expect(page.getByLabel(/^Title/)).toBeFocused();
  await page.getByLabel(/^Title/).fill(TITLE);
  await expect(page.getByText(new RegExp(`/b/[a-z0-9-]+/hello-from-${RUN}$`))).toBeVisible();
  await page.getByLabel("Body").fill("Some **bold** words.\n\n<script>window.bad = 1</script>");
  await expect(page.frameLocator('iframe[title="Preview"]').locator("strong")).toHaveText("bold");
  await expect(page.frameLocator('iframe[title="Preview"]').locator("script")).toHaveCount(0);

  await page.getByRole("button", { name: "Save draft" }).click();
  await expect(page).toHaveURL(/\/blog\/posts\/\d+$/);
  await expect(page.getByText("Draft", { exact: true })).toBeVisible();

  // Publishing while the blog is off asks first.
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const ask = page.getByRole("alertdialog", { name: "Your blog is off" });
  await ask.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByRole("button", { name: "Unpublish" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Unlist" })).toBeVisible();
  await expect(page.getByLabel("Publish date")).not.toHaveValue("");
  await expectEditorAccessible(page);

  await page.getByRole("navigation", { name: "Blog" }).getByRole("link", { name: "Posts" }).click();
  await expect(page.getByRole("link", { name: TITLE })).toBeVisible();
  await expect(page.getByText("Published", { exact: true }).first()).toBeVisible();
});

test("the blog is switched on in settings and opens as a workspace", async ({ page }) => {
  await signInAsBlogger(page);
  await page.goto("/blog");
  await page.getByRole("navigation", { name: "Blog" }).getByRole("link", { name: "Settings" }).click();
  // Public blogs are off on a new server until an administrator turns them on.
  await expect(page.getByText(/Public blogs are turned off on this server/)).toBeVisible();
  await page.getByRole("switch", { name: "Blog is published" }).click();
  await page.getByRole("alertdialog", { name: "Turn your blog on?" }).getByRole("button", { name: "Turn on" }).click();
  await expect(page.getByRole("switch", { name: "Blog is published" })).toBeChecked();
  await expectNoAxeViolations(page);

  await page.getByRole("button", { name: "More" }).click();
  await page.getByRole("menuitem", { name: "Open as workspace" }).click();
  await expect(page.getByText("This is your blog's workspace.")).toBeVisible();
  await page.getByRole("link", { name: "blog editor" }).click();
  await expect(page.getByRole("link", { name: TITLE })).toBeVisible();
});

async function setPublicBlogs(page: Page, enabled: boolean): Promise<void> {
  const me: { csrf_token: string } = await (await page.request.get("/api/v1/auth/me")).json();
  const res = await page.request.put("/api/v1/admin/settings/public-blogs", {
    data: { enabled },
    headers: { "X-CSRF-Token": me.csrf_token, Origin: new URL(page.url()).origin },
  });
  expect(res.ok(), await res.text()).toBeTruthy();
}

test("the published post is public once the server allows blogs", async ({ page, browser, baseURL }) => {
  await signInAsBlogger(page);
  const me: { user: { username: string } } = await (await page.request.get("/api/v1/auth/me")).json();
  const address = `/b/${me.user.username}/hello-from-${RUN}`;
  // A stranger sees nothing until an administrator turns public blogs on.
  const stranger = await browser.newContext({ baseURL: baseURL ?? "" });
  const visitor = await stranger.newPage();
  expect((await visitor.goto(address))?.status()).toBe(404);

  const adminContext = await browser.newContext({ baseURL: baseURL ?? "" });
  const admin = await adminContext.newPage();
  await signIn(admin);
  await setPublicBlogs(admin, true);
  try {
    const res = await visitor.goto(address);
    expect(res?.status()).toBe(200);
    expect(res?.headers()["content-security-policy"]).toContain("default-src 'none'");
    await expect(visitor.getByRole("heading", { level: 1, name: TITLE })).toBeVisible();
    await expect(visitor.locator("strong")).toHaveText("bold");
    await expect(visitor.locator("script")).toHaveCount(0);
    await expectNoAxeViolations(visitor);
    await visitor.getByRole("link", { name: /More from/ }).click();
    await expect(visitor.getByRole("link", { name: TITLE })).toBeVisible();
  } finally {
    await setPublicBlogs(admin, false);
    await adminContext.close();
    await stranger.close();
  }
});

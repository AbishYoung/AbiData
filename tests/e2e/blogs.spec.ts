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

  // The workspace list shows the user's blog with its badge; the account menu also leads to it.
  await expect(page.getByText("Blog", { exact: true })).toBeVisible();
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
  await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: "Posts", exact: true }).click();
  await expect(page.getByRole("grid", { name: "Posts datasheet" })).toContainText(TITLE);
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
    await expectNoAxeViolations(visitor);

    // Unlisting removes the post from the blog home while keeping its direct URL live with noindex.
    await page.goto("/blog");
    await page.getByRole("link", { name: TITLE }).click();
    await page.getByRole("button", { name: "Unlist" }).click();
    await expect(page.getByText("Unlisted", { exact: true })).toBeVisible();
    const homeAfterUnlist = await visitor.request.get(`/b/${me.user.username}`);
    expect(await homeAfterUnlist.text()).not.toContain(TITLE);
    const unlistedPost = await visitor.request.get(address);
    expect(unlistedPost.status()).toBe(200);
    expect(unlistedPost.headers()["x-robots-tag"]).toBe("noindex");
    await page.getByRole("button", { name: "Publish", exact: true }).click();
    await expect(page.getByRole("button", { name: "Unlist" })).toBeVisible();
  } finally {
    await setPublicBlogs(admin, false);
    await adminContext.close();
    await stranger.close();
  }
});

test("an administrator takes a post down and the owner sees why", async ({ page, browser, baseURL }) => {
  await signInAsBlogger(page);
  const me: { user: { username: string } } = await (await page.request.get("/api/v1/auth/me")).json();
  const username = me.user.username;

  const adminContext = await browser.newContext({ baseURL: baseURL ?? "" });
  const admin = await adminContext.newPage();
  await signIn(admin);
  await admin.goto("/admin?tab=blogs");
  await admin.getByLabel("Search usernames").fill(username);
  await admin.getByRole("button", { name: `Open @${username}` }).click();
  await admin.getByRole("button", { name: `Take down ${TITLE}` }).click();
  const dialog = admin.getByRole("dialog", { name: `Take down “${TITLE}”?` });
  await dialog.getByLabel("Reason").selectOption("spam");
  await dialog.getByLabel(/Note to the owner/).fill("Remove the advertising.");
  await dialog.getByRole("button", { name: "Take down" }).click();
  await expect(admin.getByRole("button", { name: `Restore ${TITLE}` })).toBeVisible();
  await expectNoAxeViolations(admin);

  // The owner sees the chip, the banner and no publish actions.
  await page.goto("/blog");
  await expect(page.getByRole("listitem").filter({ hasText: TITLE }).getByText("Taken down", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: TITLE }).click();
  const banner = page.getByRole("status", { name: /This post was taken down by an administrator/ });
  await expect(banner).toContainText("Remove the advertising.");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Unpublish" })).toHaveCount(0);

  // Restore brings the actions back.
  await admin.getByRole("button", { name: `Restore ${TITLE}` }).click();
  await expect(admin.getByRole("button", { name: `Take down ${TITLE}` })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("button", { name: "Unpublish" })).toBeVisible();
  await adminContext.close();
});

// A 1×1 PNG.
const PIXEL = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64");

test("on a phone in dark mode: a cover, the feed, and axe on every blog screen", async ({ page, browser, baseURL }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ colorScheme: "dark" });
  await signInAsBlogger(page);
  const me: { user: { username: string } } = await (await page.request.get("/api/v1/auth/me")).json();
  const username = me.user.username;

  await page.goto("/blog");
  await expect(page.getByRole("link", { name: TITLE })).toBeVisible();
  await expectNoAxeViolations(page);

  // The cover needs alt text, and is saved with the post.
  await page.getByRole("link", { name: TITLE }).click();
  await page.getByRole("group", { name: "Cover image" }).locator('input[type="file"]').setInputFiles({ name: "cover.png", mimeType: "image/png", buffer: PIXEL });
  await expect(page.getByRole("button", { name: "Remove cover" })).toBeVisible();
  await page.getByRole("button", { name: "Update" }).click();
  await expect(page.getByText("Describe the cover image for readers who cannot see it.")).toBeVisible();
  await page.getByLabel(/^Cover alt text/).fill("A single dark pixel");
  await page.getByRole("button", { name: "Update" }).click();
  await expect(page.getByText("Unsaved changes")).toHaveCount(0);
  await expectEditorAccessible(page);

  // On a phone the blog navigation moves into the More menu.
  await page.getByRole("button", { name: "More" }).click();
  await page.getByRole("menuitem", { name: "Settings" }).click();
  await expect(page.getByRole("switch", { name: "Blog is published" })).toBeVisible();
  await expectNoAxeViolations(page);

  const adminContext = await browser.newContext({ baseURL: baseURL ?? "" });
  const admin = await adminContext.newPage();
  await signIn(admin);
  await setPublicBlogs(admin, true);
  const stranger = await browser.newContext({ baseURL: baseURL ?? "", viewport: { width: 390, height: 844 }, colorScheme: "dark" });
  try {
    const visitor = await stranger.newPage();
    await visitor.goto(`/b/${username}/hello-from-${RUN}`);
    const cover = visitor.getByRole("img", { name: "A single dark pixel" });
    await expect(cover).toBeVisible();
    expect((await visitor.request.get((await cover.getAttribute("src")) ?? "")).status()).toBe(200);
    await expectNoAxeViolations(visitor);
    await visitor.goto(`/b/${username}`);
    await expectNoAxeViolations(visitor);
    const feed = await visitor.request.get(`/b/${username}/feed.xml`);
    expect(feed.headers()["content-type"]).toContain("application/atom+xml");
    expect(await feed.text()).toContain(TITLE);
  } finally {
    await setPublicBlogs(admin, false);
    await adminContext.close();
    await stranger.close();
  }
});

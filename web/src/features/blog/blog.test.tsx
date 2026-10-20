import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import { ConfirmHost } from "@/components/ui";
import { ApiError } from "@/lib/api";
import type { BlogInfo, BlogPost } from "./blogApi";

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, ...rest }: { children: ReactNode; to: string; params?: Record<string, string> }) => (
    <a href={Object.entries(params ?? {}).reduce((s, [k, v]) => s.replace(`$${k}`, v), to)} {...rest}>
      {children}
    </a>
  ),
  useNavigate: () => navigate,
  useBlocker: () => undefined,
}));

const api = vi.hoisted(() => ({
  listPosts: vi.fn(),
  getPost: vi.fn(),
  createPost: vi.fn(),
  updatePost: vi.fn(),
  setPostStatus: vi.fn(),
  deletePost: vi.fn(),
  previewPost: vi.fn(),
  checkSlug: vi.fn(),
}));
vi.mock("./blogApi", async (importOriginal) => ({ ...(await importOriginal<typeof import("./blogApi")>()), ...api }));

const uploadFile = vi.hoisted(() => vi.fn());
vi.mock("@/features/files/filesApi", async (importOriginal) => ({ ...(await importOriginal<typeof import("@/features/files/filesApi")>()), uploadFile }));

import { PostEditor } from "./PostEditor";
import { PostList } from "./PostList";

const info: BlogInfo = {
  id: "B1",
  workspace_id: "w1",
  username: "ann",
  title: "Ann writes",
  intro: "",
  published: false,
  public_blogs_enabled: true,
  public_url: "http://host/b/ann",
};

function post(over: Partial<BlogPost> = {}): BlogPost {
  return {
    id: 7,
    _version: 1,
    title: "Hello",
    slug: "hello",
    summary: "",
    body_md: "Hi",
    status: "draft",
    chip: "draft",
    public_url: "http://host/b/ann/hello",
    ...over,
  };
}

function wrap(node: ReactNode) {
  return render(
    <QueryClientProvider client={queryClient}>
      {node}
      <ConfirmHost />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  queryClient.clear();
  Object.values(api).forEach((f) => f.mockReset());
  navigate.mockReset();
  api.previewPost.mockResolvedValue("<p>preview</p>");
  api.checkSlug.mockImplementation((_ws: string, slug: string) => Promise.resolve({ slug, available: true }));
});
afterEach(cleanup);

describe("PostEditor", () => {
  it("creates a draft from a new post and moves to its address", async () => {
    const user = userEvent.setup();
    api.createPost.mockResolvedValue(post({ title: "First post", slug: "first-post" }));
    wrap(<PostEditor ws="w1" info={info} id={null} />);
    const title = screen.getByLabelText(/^Title/);
    expect(title).toHaveFocus();
    await user.type(title, "First post");
    expect(screen.getByText("http://host/b/ann/first-post")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(api.createPost).toHaveBeenCalledWith("w1", { title: "First post", summary: "", body_md: "" }));
    expect(navigate).toHaveBeenCalledWith({ to: "/w/$ws/blog/posts/$id", params: { ws: "w1", id: "7" }, replace: true });
  });

  it("requires a title", async () => {
    const user = userEvent.setup();
    wrap(<PostEditor ws="w1" info={info} id={null} />);
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    expect(await screen.findByText("Give the post a title.")).toBeInTheDocument();
    expect(api.createPost).not.toHaveBeenCalled();
  });

  it("asks before publishing while the blog is off, then saves and publishes", async () => {
    const user = userEvent.setup();
    api.getPost.mockResolvedValue(post());
    api.updatePost.mockResolvedValue(post({ _version: 2, summary: "s" }));
    api.setPostStatus.mockResolvedValue(post({ _version: 3, status: "published", chip: "published", published_at: "2026-10-19T10:00:00Z" }));
    wrap(<PostEditor ws="w1" info={info} id={7} />);
    await user.type(await screen.findByLabelText(/^Summary/), "s");
    await user.click(screen.getByRole("button", { name: "Publish" }));
    const ask = await screen.findByRole("alertdialog", { name: "Your blog is off" });
    await user.click(within(ask).getByRole("button", { name: "Publish" }));
    await waitFor(() => expect(api.setPostStatus).toHaveBeenCalledWith("w1", 7, 2, "published"));
    expect(api.updatePost).toHaveBeenCalledWith("w1", 7, 1, { summary: "s" });
    expect(await screen.findByRole("button", { name: "Unpublish" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Update" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Unlist" })).toBeInTheDocument();
  });

  it("offers reload or overwrite on a version conflict", async () => {
    const user = userEvent.setup();
    api.getPost.mockResolvedValue(post());
    api.updatePost.mockRejectedValue(new ApiError({ status: 409, code: "CONCURRENT_MODIFICATION", title: "Changed", message: "Changed elsewhere." }));
    wrap(<PostEditor ws="w1" info={info} id={7} />);
    await user.type(await screen.findByLabelText(/^Summary/), "x");
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    expect(await screen.findByRole("dialog", { name: "This post was changed elsewhere" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reload the post" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Keep my version" })).toBeInTheDocument();
  });

  it("shows a field error from the server next to the field", async () => {
    const user = userEvent.setup();
    api.getPost.mockResolvedValue(post());
    api.updatePost.mockRejectedValue(new ApiError({ status: 400, code: "VALIDATION_FAILED", title: "Bad", message: "That slug is reserved.", details: { field: "slug" } }));
    wrap(<PostEditor ws="w1" info={info} id={7} />);
    await user.type(await screen.findByLabelText(/^Summary/), "x");
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    expect(await screen.findByText("That slug is reserved.")).toBeInTheDocument();
  });

  it("shows a take-down and offers only Save", async () => {
    api.getPost.mockResolvedValue(
      post({ status: "published", chip: "taken_down", published_at: "2026-10-01T00:00:00Z", takedown: { reason_code: "spam", reason: "Spam", note: "Remove the ads.", at: "2026-10-02T00:00:00Z" } }),
    );
    wrap(<PostEditor ws="w1" info={info} id={7} />);
    const banner = await screen.findByRole("status", { name: /This post was taken down by an administrator/ });
    expect(within(banner).getByText("Remove the ads.")).toBeInTheDocument();
    expect(within(banner).getByText("Spam")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Publish" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Unlist" })).not.toBeInTheDocument();
  });

  it("uploads a cover and requires its alt text", async () => {
    const user = userEvent.setup();
    api.getPost.mockResolvedValue(post());
    api.updatePost.mockResolvedValue(post({ _version: 2, cover: "01J00000000000000000000000", cover_alt: "A boat" }));
    uploadFile.mockReturnValue({ promise: Promise.resolve({ id: "01J00000000000000000000000", content_type: "image/png" }), abort: vi.fn() });
    const { container } = wrap(<PostEditor ws="w1" info={info} id={7} />);
    await screen.findByRole("group", { name: "Cover image" });
    const input = container.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    await user.upload(input, new File(["png"], "boat.png", { type: "image/png" }));
    expect(await screen.findByRole("button", { name: "Remove cover" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    expect(await screen.findByText("Describe the cover image for readers who cannot see it.")).toBeInTheDocument();
    expect(api.updatePost).not.toHaveBeenCalled();
    await user.type(screen.getByLabelText(/^Cover alt text/), "A boat");
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(api.updatePost).toHaveBeenCalledWith("w1", 7, 1, { cover: "01J00000000000000000000000", cover_alt: "A boat" }));
  });

  it("removes a cover with its alt text", async () => {
    const user = userEvent.setup();
    api.getPost.mockResolvedValue(post({ cover: "01J00000000000000000000000", cover_alt: "A boat" }));
    api.updatePost.mockResolvedValue(post({ _version: 2 }));
    wrap(<PostEditor ws="w1" info={info} id={7} />);
    expect(await screen.findByLabelText(/^Cover alt text/)).toHaveValue("A boat");
    await user.click(screen.getByRole("button", { name: "Remove cover" }));
    expect(screen.queryByLabelText(/^Cover alt text/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(api.updatePost).toHaveBeenCalledWith("w1", 7, 1, { cover: "", cover_alt: "" }));
  });

  it("renders the preview in a sandbox without scripts", async () => {
    api.getPost.mockResolvedValue(post());
    wrap(<PostEditor ws="w1" info={info} id={7} />);
    const frame = await screen.findByTitle("Preview");
    expect(frame.getAttribute("sandbox")).toBe("allow-same-origin");
  });

  it("uploads an inline image into the body and formats with keyboard shortcuts", async () => {
    const user = userEvent.setup();
    api.getPost.mockResolvedValue(post({ body_md: "Hello world" }));
    uploadFile.mockReturnValue({ promise: Promise.resolve({ id: "01JINLINE00000000000000000", content_type: "image/png" }), abort: vi.fn() });
    wrap(<PostEditor ws="w1" info={info} id={7} />);
    const body = await screen.findByLabelText("Body");
    expect(screen.getByText(/2 words · ~1 min read/)).toBeInTheDocument();

    body.focus();
    (body as HTMLTextAreaElement).setSelectionRange(0, 5);
    await user.keyboard("{Control>}b{/Control}");
    expect(body).toHaveValue("**Hello** world");
    // The shortcut re-selects the bolded word; let that settle before moving the caret.
    await waitFor(() => expect((body as HTMLTextAreaElement).selectionStart).toBe(2));

    (body as HTMLTextAreaElement).setSelectionRange(15, 15);
    expect((body as HTMLTextAreaElement).selectionStart).toBe(15);
    expect((body as HTMLTextAreaElement).selectionEnd).toBe(15);
    const inlineInput = screen.getByTestId("inline-image-input");
    await user.upload(inlineInput, new File(["png"], "chart-q4.png", { type: "image/png" }));
    await waitFor(() => expect((body as HTMLTextAreaElement).value).toContain("![chart q4](attachment:01JINLINE00000000000000000)"));
  });
});

describe("PostList", () => {
  it("shows the empty state and the blog-off note", async () => {
    api.listPosts.mockResolvedValue({ items: [] });
    wrap(<PostList ws="w1" info={info} status="" q="" onSearch={() => undefined} />);
    expect(await screen.findByText("No posts yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Write your first post" })).toHaveAttribute("href", "/w/w1/blog/new");
    expect(screen.getByText(/Your blog is off/)).toBeInTheDocument();
  });

  it("lists posts with text chips", async () => {
    api.listPosts.mockResolvedValue({
      items: [
        { id: 1, title: "One", slug: "one", status: "published", chip: "published", published_at: "2026-10-01T00:00:00Z" },
        { id: 2, title: "Two", slug: "", status: "draft", chip: "needs_attention", problem: "The slug is not valid." },
      ],
      next_cursor: "50",
    });
    wrap(<PostList ws="w1" info={{ ...info, published: true }} status="" q="" onSearch={() => undefined} />);
    expect(await screen.findByRole("link", { name: "One" })).toHaveAttribute("href", "/w/w1/blog/posts/1");
    expect(screen.getByText("Needs attention", { selector: "span" })).toBeInTheDocument();
    expect(screen.getByText("The slug is not valid.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Load more" })).toBeInTheDocument();
    expect(screen.queryByText(/Your blog is off/)).not.toBeInTheDocument();
  });
});

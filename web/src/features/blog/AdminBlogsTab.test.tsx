import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import type { AdminBlog } from "./blogApi";

const api = vi.hoisted(() => ({
  adminGetPublicBlogs: vi.fn(),
  adminPutPublicBlogs: vi.fn(),
  adminListBlogs: vi.fn(),
  adminGetBlog: vi.fn(),
  adminTakeDown: vi.fn(),
  adminRestore: vi.fn(),
  adminResolveReports: vi.fn(),
}));
vi.mock("./blogApi", async (importOriginal) => ({ ...(await importOriginal<typeof import("./blogApi")>()), ...api }));

import { AdminBlogsTab } from "./AdminBlogsTab";

const blog: AdminBlog = {
  id: "B1",
  username: "ann",
  title: "Ann writes",
  workspace_id: "w1",
  published: true,
  public_url: "http://host/b/ann",
  active_takedowns: 0,
  open_reports: 1,
  intro: "",
  damaged: false,
  posts: [{ title: "Hello", slug: "hello", status: "published", url: "http://host/b/ann/hello", taken_down: false }],
  takedowns: [],
  reports: [{ id: "R1", post_slug: "hello", reason_code: "phishing", message: "Fake login.", created_at: "2026-10-01T00:00:00Z" }],
};

beforeEach(() => {
  queryClient.clear();
  Object.values(api).forEach((f) => f.mockReset());
  api.adminGetPublicBlogs.mockResolvedValue({ enabled: false, forced_off: false });
  api.adminListBlogs.mockResolvedValue([blog]);
  api.adminGetBlog.mockResolvedValue(blog);
});
afterEach(cleanup);

describe("AdminBlogsTab", () => {
  it("turns public blogs on", async () => {
    const user = userEvent.setup();
    api.adminPutPublicBlogs.mockResolvedValue({ enabled: true, forced_off: false });
    render(
      <QueryClientProvider client={queryClient}>
        <AdminBlogsTab />
      </QueryClientProvider>,
    );
    await user.click(await screen.findByRole("checkbox", { name: "Allow public blogs on this server" }));
    await waitFor(() => expect(api.adminPutPublicBlogs).toHaveBeenCalledWith(true));
    expect(await screen.findByText(/Public blogs are on/)).toBeInTheDocument();
  });

  it("takes a post down with a reason and a note", async () => {
    const user = userEvent.setup();
    const down = { ...blog, active_takedowns: 1, posts: [{ title: "Hello", slug: "hello", status: "published", url: "http://host/b/ann/hello", taken_down: true }] };
    api.adminTakeDown.mockImplementation(() => {
      api.adminGetBlog.mockResolvedValue(down);
      return Promise.resolve(down);
    });
    render(
      <QueryClientProvider client={queryClient}>
        <AdminBlogsTab />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("1 report")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Open @ann" }));
    expect(await screen.findByText("Fake login.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Take down Hello" }));
    const dialog = await screen.findByRole("dialog", { name: "Take down “Hello”?" });
    await user.selectOptions(within(dialog).getByLabelText("Reason"), "phishing");
    await user.type(within(dialog).getByLabelText(/Note to the owner/), "Contact abuse@example.com.");
    await user.click(within(dialog).getByRole("button", { name: "Take down" }));
    await waitFor(() => expect(api.adminTakeDown).toHaveBeenCalledWith("B1", { post_slug: "hello", reason_code: "phishing", note: "Contact abuse@example.com." }));
    expect(await screen.findByRole("button", { name: "Restore Hello" })).toBeInTheDocument();
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import type { Me } from "@/lib/session";

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, to, params, ...rest }: { children: ReactNode; to: string; params?: Record<string, string> }) => (
    <a href={Object.entries(params ?? {}).reduce((s, [k, v]) => s.replace(`$${k}`, v), to)} {...rest}>
      {children}
    </a>
  ),
  useNavigate: () => navigate,
}));

vi.mock("@/features/members/membersApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/members/membersApi")>();
  return {
    ...actual,
    listCandidates: vi.fn().mockResolvedValue([
      { user_id: "u_cand", email: "cand@example.com", username: "canduser", display_name: "Candidate User" },
    ]),
    addMember: vi.fn().mockResolvedValue({
      user_id: "u_cand",
      email: "cand@example.com",
      username: "canduser",
      display_name: "Candidate User",
      role: "editor",
    }),
  };
});

import { PickerPage } from "./PickerPage";

const sampleMe: Me = {
  csrf_token: "csrf123",
  user: {
    id: "u1",
    email: "me@example.com",
    username: "me",
    display_name: "Me User",
    is_platform_admin: false,
  },
  memberships: [
    {
      workspace_id: "w_owned",
      workspace_name: "Owned Space",
      role: "owner",
      status: "active",
      published_apps: [
        { id: "app1", name: "Solo Portal", path: "/a/solo-token", link: "http://host/a/solo-token" },
      ],
    },
    {
      workspace_id: "w_builder",
      workspace_name: "Builder Space",
      role: "builder",
      status: "active",
    },
    {
      workspace_id: "w_editor",
      workspace_name: "Editor Space",
      role: "editor",
      status: "active",
      published_apps: [
        { id: "app2", name: "Form Alpha", path: "/a/alpha-token", link: "http://host/a/alpha-token" },
        { id: "app3", name: "Report Beta", path: "/a/beta-token", link: "http://host/a/beta-token" },
      ],
    },
    {
      workspace_id: "w_viewer",
      workspace_name: "Viewer Space",
      role: "viewer",
      status: "active",
    },
  ],
  maps: { tile_url: "", attribution: "" },
};

function renderPicker(me: Me = sampleMe) {
  return render(
    <QueryClientProvider client={queryClient}>
      <PickerPage me={me} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  queryClient.clear();
});
afterEach(cleanup);

describe("PickerPage", () => {
  it("renders workspace cards with role badges and access level subtitles", () => {
    renderPicker();

    expect(screen.getByText("Owned Space")).toBeInTheDocument();
    expect(screen.getByText("Owner · Full control of members & settings")).toBeInTheDocument();

    expect(screen.getByText("Builder Space")).toBeInTheDocument();
    expect(screen.getByText("Added as Builder · Design tables, queries, forms & reports")).toBeInTheDocument();

    expect(screen.getByText("Editor Space")).toBeInTheDocument();
    expect(screen.getByText("Added as Editor · Add and edit data")).toBeInTheDocument();

    expect(screen.getByText("Viewer Space")).toBeInTheDocument();
    expect(screen.getByText("Added as Viewer · Read-only access")).toBeInTheDocument();
  });

  it("filters workspaces by All, Shared with you, and Owned by you", async () => {
    const user = userEvent.setup();
    renderPicker();

    // Verify filter pills show correct counts
    const allTab = screen.getByRole("tab", { name: /^All/ });
    const sharedTab = screen.getByRole("tab", { name: /^Shared with you/ });
    const ownedTab = screen.getByRole("tab", { name: /^Owned by you/ });

    expect(allTab).toHaveTextContent("4");
    expect(sharedTab).toHaveTextContent("3");
    expect(ownedTab).toHaveTextContent("1");

    // Click "Shared with you": only builder, editor, and viewer spaces are shown
    await user.click(sharedTab);
    expect(screen.queryByText("Owned Space")).not.toBeInTheDocument();
    expect(screen.getByText("Builder Space")).toBeInTheDocument();
    expect(screen.getByText("Editor Space")).toBeInTheDocument();
    expect(screen.getByText("Viewer Space")).toBeInTheDocument();

    // Click "Owned by you": only owned space is shown
    await user.click(ownedTab);
    expect(screen.getByText("Owned Space")).toBeInTheDocument();
    expect(screen.queryByText("Builder Space")).not.toBeInTheDocument();
    expect(screen.queryByText("Editor Space")).not.toBeInTheDocument();
    expect(screen.queryByText("Viewer Space")).not.toBeInTheDocument();

    // Click "All": all spaces restored
    await user.click(allTab);
    expect(screen.getByText("Owned Space")).toBeInTheDocument();
    expect(screen.getByText("Builder Space")).toBeInTheDocument();
  });

  it("renders a direct published app link when a workspace has 1 published app", () => {
    renderPicker();

    const appLink = screen.getByRole("link", { name: "Open published app Solo Portal" });
    expect(appLink).toHaveAttribute("href", "/a/solo-token");
  });

  it("renders a published apps dropdown when a workspace has multiple published apps", async () => {
    const user = userEvent.setup();
    renderPicker();

    const menuTrigger = screen.getByRole("button", { name: "Published apps for Editor Space" });
    expect(menuTrigger).toBeInTheDocument();

    await user.click(menuTrigger);

    const formAlphaLink = await screen.findByRole("menuitem", { name: "Form Alpha" });
    expect(formAlphaLink).toHaveAttribute("href", "/a/alpha-token");

    const reportBetaLink = screen.getByRole("menuitem", { name: "Report Beta" });
    expect(reportBetaLink).toHaveAttribute("href", "/a/beta-token");
  });

  it("renders Add member button only on owned workspace cards and opens AddDialog", async () => {
    const user = userEvent.setup();
    renderPicker();

    // Owner card has Add member button
    const addBtn = screen.getByRole("button", { name: "Add member to Owned Space" });
    expect(addBtn).toBeInTheDocument();

    // Non-owner card does not have Add member button
    expect(screen.queryByRole("button", { name: "Add member to Builder Space" })).not.toBeInTheDocument();

    // Clicking Add member opens dialog
    await user.click(addBtn);
    expect(await screen.findByRole("dialog", { name: "Add a member" })).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
  });
});

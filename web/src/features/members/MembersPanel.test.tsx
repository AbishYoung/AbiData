import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";

const candidates = [
  { user_id: "u1", email: "candidate1@example.com", username: "cand1", display_name: "Candidate One" },
  { user_id: "u2", email: "candidate2@example.com", display_name: "Candidate Two" },
];

const api = vi.hoisted(() => ({
  listCandidates: vi.fn(),
  addMember: vi.fn(),
}));

vi.mock("./membersApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./membersApi")>()),
  ...api,
}));

import { AddDialog } from "./MembersPanel";

beforeEach(() => {
  queryClient.clear();
  api.listCandidates.mockReset();
  api.addMember.mockReset();
  api.listCandidates.mockResolvedValue(candidates);
});
afterEach(cleanup);

describe("AddDialog", () => {
  it("renders candidate users and clicking one populates the email input", async () => {
    const user = userEvent.setup();
    const onAdded = vi.fn();
    const onClose = vi.fn();

    render(
      <QueryClientProvider client={queryClient}>
        <AddDialog ws="ws_test" onClose={onClose} onAdded={onAdded} />
      </QueryClientProvider>,
    );

    // Dialog title
    expect(screen.getByRole("dialog", { name: "Add a member" })).toBeInTheDocument();

    // Check candidate list is rendered
    expect(await screen.findByText(/Available platform users \(2\)/)).toBeInTheDocument();
    expect(screen.getByText("Candidate One")).toBeInTheDocument();
    expect(screen.getByText("@cand1")).toBeInTheDocument();

    const emailInput = screen.getByLabelText("Email");
    expect(emailInput).toHaveValue("");

    // Click candidate 1 -> fills email input
    const cand1Button = screen.getByRole("button", { name: /Select Candidate One/ });
    await user.click(cand1Button);

    expect(emailInput).toHaveValue("candidate1@example.com");

    // Role selector has role description
    expect(screen.getByText("Can add and edit data")).toBeInTheDocument();

    // Change role to Builder
    const roleSelect = screen.getByLabelText("Role");
    await user.selectOptions(roleSelect, "builder");
    expect(screen.getByText("Can design tables, queries, forms and reports")).toBeInTheDocument();

    // Submit form
    api.addMember.mockResolvedValue({
      user_id: "u1",
      email: "candidate1@example.com",
      username: "cand1",
      display_name: "Candidate One",
      role: "builder",
    });

    await user.click(screen.getByRole("button", { name: "Add member" }));

    await waitFor(() => {
      expect(api.addMember).toHaveBeenCalledWith("ws_test", {
        email: "candidate1@example.com",
        role: "builder",
      });
      expect(onAdded).toHaveBeenCalled();
    });
  });

  it("allows typing an email directly without selecting a candidate", async () => {
    const user = userEvent.setup();
    const onAdded = vi.fn();
    const onClose = vi.fn();

    render(
      <QueryClientProvider client={queryClient}>
        <AddDialog ws="ws_test" onClose={onClose} onAdded={onAdded} />
      </QueryClientProvider>,
    );

    const emailInput = screen.getByLabelText("Email");
    await user.type(emailInput, "manual@example.com");

    api.addMember.mockResolvedValue({
      user_id: "u_man",
      email: "manual@example.com",
      display_name: "Manual User",
      role: "editor",
    });

    await user.click(screen.getByRole("button", { name: "Add member" }));

    await waitFor(() => {
      expect(api.addMember).toHaveBeenCalledWith("ws_test", {
        email: "manual@example.com",
        role: "editor",
      });
      expect(onAdded).toHaveBeenCalled();
    });
  });
});

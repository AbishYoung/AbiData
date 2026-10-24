import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QuickAddRow } from "./QuickAddRow";
import { col } from "./testutil";

afterEach(cleanup);

const columns = [col("id", "autonumber", { display_name: "ID" }), col("name", "short_text", { display_name: "Name" }), col("qty", "integer", { display_name: "Qty" })];

function setup(onSubmit = vi.fn().mockResolvedValue(null), onClose = vi.fn()) {
  render(<QuickAddRow columns={columns} widths={[90, 180, 110]} totalWidth={456} top={32} onSubmit={onSubmit} onClose={onClose} />);
  return { onSubmit, onClose };
}

describe("QuickAddRow", () => {
  it("focuses the first editable field, saves with Enter and clears for the next record", async () => {
    const user = userEvent.setup();
    const { onSubmit } = setup();
    const name = screen.getByLabelText("Name, new record");
    expect(name).toHaveFocus();
    expect(screen.getByLabelText("ID, new record")).toBeDisabled();
    await user.type(name, "Widget");
    await user.type(screen.getByLabelText("Qty, new record"), "4{Enter}");
    expect(onSubmit).toHaveBeenCalledWith({ name: "Widget", qty: 4 });
    await waitFor(() => expect(screen.getByLabelText("Name, new record")).toHaveValue(""));
    expect(screen.getByLabelText("Name, new record")).toHaveFocus();
  });

  it("keeps the draft and shows the error when a value does not parse", async () => {
    const user = userEvent.setup();
    const { onSubmit } = setup();
    await user.type(screen.getByLabelText("Qty, new record"), "abc{Enter}");
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getAllByText("Enter a whole number without decimals.").length).toBeGreaterThan(0);
    expect(screen.getByLabelText("Qty, new record")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByLabelText("Qty, new record")).toHaveValue("abc");
  });

  it("shows server field errors and keeps the values", async () => {
    const user = userEvent.setup();
    setup(vi.fn().mockResolvedValue({ name: "Name is required." }));
    await user.type(screen.getByLabelText("Qty, new record"), "1{Enter}");
    await waitFor(() => expect(screen.getByLabelText("Name, new record")).toHaveAttribute("aria-invalid", "true"));
    expect(screen.getByLabelText("Qty, new record")).toHaveValue("1");
  });

  it("closes with Escape", async () => {
    const user = userEvent.setup();
    const { onClose } = setup();
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });
});

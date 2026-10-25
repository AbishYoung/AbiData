import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConditionalFormattingModal } from "./ConditionalFormattingModal";
import { col } from "./testutil";

afterEach(cleanup);

const columns = [col("status", "short_text", { display_name: "Status" }), col("due", "date", { display_name: "Due" })];

describe("ConditionalFormattingModal", () => {
  it("adds, edits and saves a rule", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    render(<ConditionalFormattingModal columns={columns} rules={[]} onSave={onSave} onClose={vi.fn()} />);
    expect(screen.getByText(/There are no rules yet/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Add rule" }));
    await user.type(screen.getByLabelText("Rule 1 value"), "Late");
    await user.selectOptions(screen.getByLabelText("Rule 1 applies to"), "row");
    await user.selectOptions(screen.getByLabelText("Rule 1 colour"), "warning");
    await user.click(screen.getByRole("button", { name: "Save rules" }));
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0]?.[0]).toMatchObject([{ columnId: "status", operator: "eq", value: "Late", target: "row", tone: "warning" }]);
  });

  it("offers the overdue condition for dates and hides the value box for it", async () => {
    const user = userEvent.setup();
    render(<ConditionalFormattingModal columns={columns} rules={[]} onSave={vi.fn()} onClose={vi.fn()} />);
    await user.click(screen.getByRole("button", { name: "Add rule" }));
    await user.selectOptions(screen.getByLabelText("Rule 1 field"), "due");
    await user.selectOptions(screen.getByLabelText("Rule 1 condition"), "is_overdue");
    expect(screen.queryByLabelText("Rule 1 value")).toBeNull();
  });

  it("reorders and removes rules without saving until asked", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    const rules = [
      { id: "a", columnId: "status", operator: "eq" as const, value: "A", target: "cell" as const, tone: "danger" as const },
      { id: "b", columnId: "status", operator: "eq" as const, value: "B", target: "cell" as const, tone: "success" as const },
    ];
    render(<ConditionalFormattingModal columns={columns} rules={rules} onSave={onSave} onClose={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Move rule 1 up" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Move rule 1 down" }));
    expect(screen.getByLabelText("Rule 1 value")).toHaveValue("B");
    await user.click(screen.getByRole("button", { name: "Remove rule 2" }));
    expect(onSave).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Save rules" }));
    expect(onSave.mock.calls[0]?.[0]).toMatchObject([{ id: "b" }]);
  });
});

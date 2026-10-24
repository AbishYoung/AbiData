import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PastePreviewDialog } from "./PastePreviewDialog";
import { col, row } from "./testutil";

afterEach(cleanup);

const columns = [col("name", "short_text", { display_name: "Name" }), col("qty", "integer", { display_name: "Qty" })];
const rows = [row(1, 2, { name: "Ann", qty: 1 }), row(2, 4, { name: "Bob", qty: 2 })];

function setup(matrix: string[][], over: Partial<Parameters<typeof PastePreviewDialog>[0]> = {}) {
  const onApply = vi.fn();
  const onCancel = vi.fn();
  render(
    <PastePreviewDialog
      columns={columns}
      matrix={matrix}
      startRow={0}
      startCol={0}
      total={2}
      rowAt={(i) => rows[i]}
      busy={false}
      error={null}
      onApply={onApply}
      onCancel={onCancel}
      {...over}
    />,
  );
  return { onApply, onCancel };
}

describe("PastePreviewDialog", () => {
  it("previews the conversion and applies an overwrite with the planned writes", async () => {
    const user = userEvent.setup();
    const { onApply } = setup([["Zed", "5"], ["Bob", "9"]]);
    expect(screen.getByRole("dialog", { name: "Paste cells" })).toBeVisible();
    expect(screen.getByText(/Row 1 to 2, Name to Qty/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: /Paste into 2 records/ }));
    expect(onApply).toHaveBeenCalledWith(
      [
        { op: "update", id: 1, version: 2, fields: { name: "Zed", qty: 5 } },
        { op: "update", id: 2, version: 4, fields: { qty: 9 } },
      ],
      "overwrite",
    );
  });

  it("blocks invalid values until they are skipped", async () => {
    const user = userEvent.setup();
    const { onApply } = setup([["Zed", "abc"]]);
    const paste = screen.getByRole("button", { name: /^Paste into/ });
    expect(paste).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent("Qty");
    await user.click(screen.getByRole("checkbox", { name: /Skip the cells/ }));
    expect(paste).toBeEnabled();
    await user.click(paste);
    expect(onApply).toHaveBeenCalledWith([{ op: "update", id: 1, version: 2, fields: { name: "Zed" } }], "overwrite");
  });

  it("can insert the rows as new records instead", async () => {
    const user = userEvent.setup();
    const { onApply } = setup([["New", "3"]]);
    await user.click(screen.getByRole("radio", { name: /Insert as new records/ }));
    await user.click(screen.getByRole("button", { name: /Paste as 1 new record/ }));
    expect(onApply).toHaveBeenCalledWith([{ op: "insert", fields: { name: "New", qty: 3 } }], "insert");
  });

  it("starts in insert mode below the last record and shows the server's refusal", () => {
    setup([["New", "3"]], { startRow: 2, error: "Name must be unique." });
    expect(screen.getByRole("radio", { name: /Overwrite existing cells/ })).toBeDisabled();
    expect(screen.getByRole("radio", { name: /Insert as new records/ })).toBeChecked();
    expect(screen.getByText("Name must be unique.")).toBeVisible();
  });
});

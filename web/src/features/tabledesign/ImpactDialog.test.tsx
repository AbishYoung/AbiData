import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ImpactReport } from "./draft";
import { ImpactDialog } from "./ImpactDialog";

afterEach(cleanup);

const empty: ImpactReport = {
  breaking: [],
  warnings: [],
  conversion_failures: [],
  rows_affected: 0,
  restore_point_required: false,
  schema_version: 3,
};

function show(report: ImpactReport | null, extra: Partial<Parameters<typeof ImpactDialog>[0]> = {}) {
  const onApply = vi.fn();
  const onCancel = vi.fn();
  render(
    <ImpactDialog title="Review design changes" summary="One change." report={report} busy={false} onApply={onApply} onCancel={onCancel} {...extra} />,
  );
  return { onApply, onCancel };
}

describe("ImpactDialog", () => {
  it("shows only Cancel while the preview is running", () => {
    show(null);
    expect(screen.getByRole("status")).toHaveTextContent("Checking");
    expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Apply" })).toBeNull();
  });

  it("lets a clean change be applied and notes the restore point", async () => {
    const { onApply } = show({
      ...empty,
      rows_affected: 12,
      restore_point_required: true,
      warnings: [{ kind: "data_loss", message: "Field “Notes” and its values will be deleted." }],
    });
    expect(screen.getByRole("heading", { name: /Warnings/ })).toBeInTheDocument();
    expect(screen.getByText(/Notes/)).toBeInTheDocument();
    expect(screen.getByText("A restore point will be taken first.")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Apply anyway" })).toBeNull();
    const apply = screen.getByRole("button", { name: "Apply" });
    expect(apply).toBeEnabled();
    await userEvent.setup().click(apply);
    expect(onApply).toHaveBeenCalledWith(false);
  });

  it("lists conversion failures with sample values and blocks plain Apply", () => {
    show({
      ...empty,
      breaking: [{ kind: "conversion_failure", message: "1 values in “Name” cannot be converted from short_text to integer." }],
      conversion_failures: [
        { column_id: "C1", column: "Name", from: "short_text", to: "integer", count: 1, samples: [{ row_id: 4, value: "abc" }] },
      ],
    });
    expect(screen.getByRole("heading", { name: /Breaking/ })).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Values that cannot be converted" });
    expect(within(table).getByText("Name")).toBeInTheDocument();
    expect(within(table).getByText("short_text → integer")).toBeInTheDocument();
    expect(within(table).getByText("“abc”")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  });

  it("requires the acknowledgement before Apply anyway sends force", async () => {
    const { onApply } = show({ ...empty, breaking: [{ kind: "query_invalid", message: "Query “Q1” uses a field that is removed." }] });
    const anyway = screen.getByRole("button", { name: "Apply anyway" });
    expect(anyway).toBeDisabled();
    const user = userEvent.setup();
    await user.click(screen.getByRole("checkbox", { name: "I understand this may break the listed objects" }));
    expect(anyway).toBeEnabled();
    await user.click(anyway);
    expect(onApply).toHaveBeenCalledWith(true);
  });

  it("never offers Apply anyway when existing rows violate the design", () => {
    show({ ...empty, breaking: [{ kind: "constraint_violation", message: "3 rows have no value for a required field." }] });
    expect(screen.queryByRole("button", { name: "Apply anyway" })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  });

  it("explains a concurrent modification and offers Reload design", async () => {
    const onReload = vi.fn();
    show(null, { concurrent: true, onReload });
    expect(screen.getByRole("alert")).toHaveTextContent("changed elsewhere");
    await userEvent.setup().click(screen.getByRole("button", { name: "Reload design" }));
    expect(onReload).toHaveBeenCalled();
  });

  it("shows server errors inline", () => {
    show(null, { error: "Change 1 (alter_column): Enter a name." });
    expect(screen.getByRole("alert")).toHaveTextContent("Change 1 (alter_column): Enter a name.");
  });
});

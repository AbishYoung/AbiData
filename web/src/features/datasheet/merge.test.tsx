import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mergeFields, mergedRequest } from "./merge";
import { MergeDialog } from "./MergeDialog";
import { col, row } from "./testutil";

const columns = [
  col("c_name", "short_text", { display_name: "Name" }),
  col("c_qty", "integer", { display_name: "Quantity" }),
  col("c_owner", "lookup", { display_name: "Owner" }),
];
const current = row(7, 9, { c_name: "Theirs", c_qty: 5, c_owner: 2 }, { c_owner: "Bea" });

describe("merge", () => {
  const fields = mergeFields(
    columns,
    { c_name: "Mine", c_qty: 5, c_owner: 1 },
    { c_owner: "Al" },
    current,
  );

  it("lists only changed fields whose values still differ", () => {
    expect(fields.map((f) => f.colId)).toEqual(["c_name", "c_owner"]);
    expect(fields[1]).toMatchObject({ mine: 1, mineDisplay: "Al", theirs: 2, theirsDisplay: "Bea" });
  });

  it("builds the merged payload from the choices with the current version", () => {
    expect(mergedRequest(fields, {}, current)).toEqual({ version: 9, fields: { c_name: "Mine", c_owner: 1 } });
    expect(mergedRequest(fields, { c_name: "theirs" }, current)).toEqual({ version: 9, fields: { c_owner: 1 } });
    expect(mergedRequest(fields, { c_name: "theirs", c_owner: "theirs" }, current)).toEqual({ version: 9, fields: {} });
  });
});

describe("MergeDialog", () => {
  const fields = mergeFields(columns, { c_name: "Mine", c_owner: 1 }, { c_owner: "Al" }, current);

  it("saves the user's radio choices", async () => {
    const onSave = vi.fn();
    render(<MergeDialog columns={columns} fields={fields} current={current} busy={false} onSave={onSave} onDiscard={vi.fn()} />);
    const nameGroup = screen.getByRole("group", { name: "Name" });
    expect(nameGroup).toHaveTextContent("Mine");
    expect(nameGroup).toHaveTextContent("Theirs");
    await userEvent.click(screen.getAllByRole("radio", { name: /Theirs/ })[0] as HTMLElement);
    await userEvent.click(screen.getByRole("button", { name: "Save merged" }));
    expect(onSave).toHaveBeenCalledWith({ version: 9, fields: { c_owner: 1 } });
  });

  it("discards on request", async () => {
    const onDiscard = vi.fn();
    render(<MergeDialog columns={columns} fields={fields} current={current} busy={false} onSave={vi.fn()} onDiscard={onDiscard} />);
    await userEvent.click(screen.getByRole("button", { name: "Discard mine" }));
    expect(onDiscard).toHaveBeenCalled();
  });
});

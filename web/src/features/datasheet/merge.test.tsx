import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { mergedRequest, threeWayMerge, type MergeInput } from "./merge";
import { MergeDialog } from "./MergeDialog";
import { col, row } from "./testutil";

const columns = [
  col("c_name", "short_text", { display_name: "Name" }),
  col("c_qty", "integer", { display_name: "Quantity" }),
  col("c_owner", "lookup", { display_name: "Owner" }),
  col("c_price", "currency", { display_name: "Price" }),
  col("c_due", "date", { display_name: "Due" }),
  col("c_note", "long_text", { display_name: "Note" }),
];

function input(partial: Partial<MergeInput>): MergeInput {
  return { changes: {}, display: {}, base: {}, baseDisplay: {}, ...partial };
}

describe("threeWayMerge", () => {
  it("sends my field when only other fields changed on the server", () => {
    const current = row(7, 9, { c_name: "Base", c_qty: 8 });
    const r = threeWayMerge(columns, input({ changes: { c_name: "Mine" }, base: { c_name: "Base" } }), current);
    expect(r.send).toEqual({ c_name: "Mine" });
    expect(r.conflicts).toEqual([]);
  });

  it("needs no decision when the server already holds my value", () => {
    const current = row(7, 9, { c_name: "Mine" });
    const r = threeWayMerge(columns, input({ changes: { c_name: "Mine" }, base: { c_name: "Base" } }), current);
    expect(r.send).toEqual({});
    expect(r.conflicts).toEqual([]);
  });

  it("is a conflict when both changed the same field differently", () => {
    const current = row(7, 9, { c_name: "Theirs" });
    const r = threeWayMerge(columns, input({ changes: { c_name: "Mine" }, base: { c_name: "Base" } }), current);
    expect(r.send).toEqual({});
    expect(r.conflicts).toEqual([{ colId: "c_name", base: "Base", baseDisplay: undefined, mine: "Mine", theirs: "Theirs", mineDisplay: undefined, theirsDisplay: undefined }]);
  });

  it("splits disjoint and overlapping fields", () => {
    const current = row(7, 9, { c_name: "Theirs", c_qty: 1, c_note: "n" });
    const r = threeWayMerge(
      columns,
      input({ changes: { c_name: "Mine", c_qty: 2 }, base: { c_name: "Base", c_qty: 1 } }),
      current,
    );
    expect(r.send).toEqual({ c_qty: 2 });
    expect(r.conflicts.map((c) => c.colId)).toEqual(["c_name"]);
  });

  it("ignores fields the user did not change (a reverted field is not in the changes)", () => {
    const current = row(7, 9, { c_name: "Theirs", c_qty: 99 });
    const r = threeWayMerge(columns, input({ changes: {}, base: {} }), current);
    expect(r).toEqual({ send: {}, sendDisplay: {}, conflicts: [] });
  });

  it("treats null, undefined and empty text as the same empty value", () => {
    const current = row(7, 9, { c_note: null });
    expect(threeWayMerge(columns, input({ changes: { c_note: "x" }, base: { c_note: "" } }), current).send).toEqual({ c_note: "x" });
    expect(threeWayMerge(columns, input({ changes: { c_note: null }, base: { c_note: "x" } }), row(7, 9, {})).conflicts).toEqual([]);
    // I cleared it, they edited it.
    const c = threeWayMerge(columns, input({ changes: { c_note: null }, base: { c_note: "x" } }), row(7, 9, { c_note: "y" }));
    expect(c.conflicts).toHaveLength(1);
    expect(c.conflicts[0]).toMatchObject({ mine: null, theirs: "y", base: "x" });
    // The record's field was empty before; both filled it in.
    expect(threeWayMerge(columns, input({ changes: { c_note: "a" }, base: { c_note: null } }), row(7, 9, { c_note: "b" })).conflicts).toHaveLength(1);
  });

  it("compares typed values by value, not by text", () => {
    // Currency "10.5000" equals "10.50"; integer 5 equals "5"; lookup 2 equals "2".
    const current = row(7, 9, { c_price: "10.5000", c_qty: "5", c_owner: 2, c_due: "2026-03-01" });
    const r = threeWayMerge(
      columns,
      input({
        changes: { c_price: "10.50", c_qty: 6, c_owner: 3, c_due: "2026-04-01" },
        base: { c_price: "10.5", c_qty: 5, c_owner: "2", c_due: "2026-03-01" },
        display: { c_owner: "Cy" },
      }),
      current,
    );
    expect(r.conflicts).toEqual([]);
    expect(r.send).toEqual({ c_qty: 6, c_owner: 3, c_due: "2026-04-01" });
    expect(r.sendDisplay).toEqual({ c_owner: "Cy" });
  });

  it("detects a typed conflict and carries lookup display text", () => {
    const current = row(7, 9, { c_owner: 4, c_due: "2026-05-05" }, { c_owner: "Di" });
    const r = threeWayMerge(
      columns,
      input({
        changes: { c_owner: 3, c_due: "2026-04-01" },
        display: { c_owner: "Cy" },
        base: { c_owner: 2, c_due: "2026-03-01" },
        baseDisplay: { c_owner: "Bo" },
      }),
      current,
    );
    expect(r.conflicts.map((c) => c.colId)).toEqual(["c_owner", "c_due"]);
    expect(r.conflicts[0]).toMatchObject({ base: 2, baseDisplay: "Bo", mine: 3, mineDisplay: "Cy", theirs: 4, theirsDisplay: "Di" });
  });

  it("treats a field with no recorded base as a conflict unless the values agree", () => {
    const r = threeWayMerge(columns, input({ changes: { c_name: "Mine" } }), row(7, 9, { c_name: "Theirs" }));
    expect(r.conflicts).toHaveLength(1);
    expect("base" in (r.conflicts[0] ?? {})).toBe(false);
  });
});

describe("mergedRequest", () => {
  const current = row(7, 9, { c_name: "Theirs", c_owner: 2 });
  const fields = threeWayMerge(
    columns,
    input({ changes: { c_name: "Mine", c_owner: 1 }, base: { c_name: "Base", c_owner: 0 }, display: { c_owner: "Al" } }),
    current,
  ).conflicts;

  it("builds the payload from the choices and the auto-merged fields with the current version", () => {
    expect(mergedRequest(fields, {}, current)).toEqual({ version: 9, fields: { c_name: "Mine", c_owner: 1 } });
    expect(mergedRequest(fields, { c_name: "theirs" }, current, { c_qty: 3 })).toEqual({ version: 9, fields: { c_qty: 3, c_owner: 1 } });
    expect(mergedRequest(fields, { c_name: "theirs", c_owner: "theirs" }, current)).toEqual({ version: 9, fields: {} });
  });
});

describe("MergeDialog", () => {
  const current = row(7, 9, { c_name: "Theirs", c_owner: 2 }, { c_owner: "Bea" });
  const fields = threeWayMerge(
    columns,
    input({
      changes: { c_name: "Mine", c_owner: 1 },
      display: { c_owner: "Al" },
      base: { c_name: "Base", c_owner: 0 },
      baseDisplay: { c_owner: "Zed" },
    }),
    current,
  ).conflicts;

  it("shows the value before and saves the user's radio choices with the auto-merged fields", async () => {
    const onSave = vi.fn();
    render(<MergeDialog columns={columns} fields={fields} auto={{ c_qty: 3 }} current={current} busy={false} onSave={onSave} onDiscard={vi.fn()} />);
    const nameGroup = screen.getByRole("group", { name: "Name" });
    expect(nameGroup).toHaveTextContent("Before");
    expect(nameGroup).toHaveTextContent("Base");
    expect(nameGroup).toHaveTextContent("Mine");
    expect(nameGroup).toHaveTextContent("Theirs");
    expect(screen.getByRole("group", { name: "Owner" })).toHaveTextContent("Zed");
    await userEvent.click(screen.getAllByRole("radio", { name: /Theirs/ })[0] as HTMLElement);
    await userEvent.click(screen.getByRole("button", { name: "Save merged" }));
    expect(onSave).toHaveBeenCalledWith({ version: 9, fields: { c_qty: 3, c_owner: 1 } });
  });

  it("discards on request", async () => {
    const onDiscard = vi.fn();
    render(<MergeDialog columns={columns} fields={fields} current={current} busy={false} onSave={vi.fn()} onDiscard={onDiscard} />);
    await userEvent.click(screen.getByRole("button", { name: "Discard mine" }));
    expect(onDiscard).toHaveBeenCalled();
  });
});

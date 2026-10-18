import { describe, expect, it, vi } from "vitest";
import { canvasMenu, controlAt } from "./canvasMenu";

const actions = () => ({ duplicate: vi.fn(), remove: vi.fn(), align: vi.fn(), selectAll: vi.fn() });
const item = (entries: ReturnType<typeof canvasMenu>, id: string) => {
  const e = entries.find((x) => x.id === id);
  if (!e || "separator" in e) throw new Error(`no ${id}`);
  return e;
};

describe("canvasMenu", () => {
  it("disables actions that need a selection when nothing is selected", () => {
    const m = canvasMenu({ selected: 0, total: 3, ...actions() });
    expect(item(m, "duplicate").disabled).toBe(true);
    expect(item(m, "delete").disabled).toBe(true);
    expect(item(m, "left").disabled).toBe(true);
    expect(item(m, "all").disabled).toBe(false);
  });

  it("aligns only two or more controls, and names how many a delete removes", () => {
    const one = canvasMenu({ selected: 1, total: 3, ...actions() });
    expect(item(one, "delete").label).toBe("Delete");
    expect(item(one, "top").disabled).toBe(true);
    const a = actions();
    const two = canvasMenu({ selected: 2, total: 3, ...a });
    expect(item(two, "delete")).toMatchObject({ label: "Delete 2 controls", danger: true, disabled: false });
    item(two, "bottom").run();
    expect(a.align).toHaveBeenCalledWith("bottom");
  });

  it("cannot select all on an empty canvas", () => {
    expect(item(canvasMenu({ selected: 0, total: 0, ...actions() }), "all").disabled).toBe(true);
  });
});

describe("controlAt", () => {
  it("finds the control that contains the clicked element", () => {
    const a = document.createElement("div");
    const b = document.createElement("div");
    const inner = document.createElement("span");
    b.appendChild(inner);
    const nodes = new Map([
      ["a", a],
      ["b", b],
    ]);
    expect(controlAt(inner, nodes)).toBe("b");
    expect(controlAt(a, nodes)).toBe("a");
    expect(controlAt(document.createElement("p"), nodes)).toBeNull();
    expect(controlAt(null, nodes)).toBeNull();
  });
});

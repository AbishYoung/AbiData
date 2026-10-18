import { describe, expect, it } from "vitest";
import type { App, PublicColumn, PublicForm } from "./appsApi";
import { appState, move, validateApp, waiting } from "./appLogic";
import { buildSubmission, encode, entries, inputType, readingOrder } from "./publicFormLogic";

const col = (id: string, field_type: string, extra: Partial<PublicColumn> = {}): PublicColumn => ({ id, name: id.toUpperCase(), field_type, required: false, ...extra });

type Ctl = NonNullable<PublicForm["layout"]["sections"]["detail"]["controls"]>[number];
const ctl = (name: string, type: string, x: number, y: number, extra: Partial<Ctl> = {}): Ctl =>
  ({ id: name, name, type, x, y, w: 100, h: 20, tab_index: 0, ...extra }) as Ctl;

function form(controls: Ctl[], columns: PublicColumn[], header: Ctl[] = []): PublicForm {
  return {
    name: "F",
    columns,
    layout: { width: 400, sections: { header: { height: 20, controls: header }, detail: { height: 200, controls }, footer: { height: 0, controls: [] } } },
  } as PublicForm;
}

describe("public form layout", () => {
  it("reads top to bottom, then left to right, header first", () => {
    const f = form([ctl("b", "textbox", 0, 40), ctl("c", "textbox", 50, 10), ctl("a", "textbox", 0, 10)], [], [ctl("h", "label", 0, 0)]);
    expect(readingOrder(f).map((c) => c.name)).toEqual(["h", "a", "c", "b"]);
  });

  it("turns a field's own caption label into its label, keeps other text and drops lines", () => {
    const f = form(
      [
        ctl("lbl", "label", 0, 0, { caption: "Name:" }),
        ctl("name", "textbox", 110, 0, { bind: { column_id: "c1" } }),
        ctl("intro", "label", 0, 30, { caption: "Tell us what you saw." }),
        ctl("rule", "line", 0, 50),
        ctl("total", "textbox", 0, 60, { calc: { lua_expr: "" }, caption: "Total" }),
        ctl("ok", "checkbox", 0, 90, { bind: { column_id: "c2" }, caption: "I agree" }),
        ctl("ghost", "textbox", 0, 120, { bind: { column_id: "missing" } }),
      ],
      [col("c1", "short_text", { name: "Name" }), col("c2", "yes_no")],
    );
    expect(entries(f).map((e) => [e.kind, e.kind === "text" ? e.text : e.label])).toEqual([
      ["field", "Name"],
      ["text", "Tell us what you saw."],
      ["calc", "Total"],
      ["field", "I agree"],
    ]);
  });
});

describe("public form values", () => {
  it("picks an input for each field type", () => {
    expect(inputType(col("a", "long_text"))).toBe("textarea");
    expect(inputType(col("a", "currency"))).toBe("number");
    expect(inputType(col("a", "datetime"))).toBe("datetime-local");
    expect(inputType(col("a", "yes_no"))).toBe("checkbox");
    expect(inputType(col("a", "hyperlink"))).toBe("url");
    expect(inputType(col("a", "short_text"))).toBe("text");
  });

  it("encodes what was typed in the rows API representation", () => {
    expect(encode(col("a", "integer"), "42")).toEqual({ value: 42 });
    expect(encode(col("a", "integer"), "4.2")).toHaveProperty("error");
    expect(encode(col("a", "number"), "4.25")).toEqual({ value: 4.25 });
    expect(encode(col("a", "currency"), " 12.50 ")).toEqual({ value: "12.50" });
    expect(encode(col("a", "currency"), "1.23456")).toHaveProperty("error");
    expect(encode(col("a", "yes_no"), undefined)).toEqual({ value: false });
    expect(encode(col("a", "short_text"), "")).toEqual({ value: null });
    expect(encode(col("a", "short_text", { max_length: 3 }), "abcd")).toHaveProperty("error");
    expect(encode(col("a", "short_text", { max_length: 3 }), "abc")).toEqual({ value: "abc" });
    const dt = encode(col("a", "datetime"), "2026-03-01T12:30");
    expect("value" in dt && typeof dt.value === "string" && dt.value.endsWith("Z")).toBe(true);
  });

  it("lists required and malformed fields instead of submitting", () => {
    const fields = [
      { column: col("n", "short_text", { name: "Name" }), required: true },
      { column: col("q", "integer", { name: "Qty" }), required: false },
      { column: col("ok", "yes_no"), required: true },
    ];
    expect(buildSubmission(fields, { q: "x" })).toEqual({ errors: { n: "Name is required.", q: "Qty must be a whole number." } });
    expect(buildSubmission(fields, { n: "Ann", q: "" })).toEqual({ fields: { n: "Ann", ok: false } });
  });
});

describe("app helpers", () => {
  const app = (over: Partial<App>): App =>
    ({ id: "a", name: "A", enabled: true, taken_down: false, items: [], start_item: "", workspace_id: "w", created_by_name: "", created_at: "", updated_at: "", ...over }) as App;

  it("says whose switch keeps an app off, the administrator's first", () => {
    expect(appState(app({}), true)).toBe("live");
    expect(appState(app({ enabled: false }), true)).toBe("off");
    expect(appState(app({}), false)).toBe("server-off");
    expect(appState(app({ enabled: false, taken_down: true }), false)).toBe("taken-down");
  });

  it("finds pages waiting for approval", () => {
    const a = app({ items: [{ id: "1", kind: "form", object_id: "f", title: "F", status: "changed" }, { id: "2", kind: "report", object_id: "r", title: "R", status: "live" }] });
    expect(waiting(a).map((i) => i.id)).toEqual(["1"]);
  });

  it("moves menu items and ignores moves past either end", () => {
    expect(move(["a", "b", "c"], 2, -1)).toEqual(["a", "c", "b"]);
    expect(move(["a", "b", "c"], 0, -1)).toEqual(["a", "b", "c"]);
    expect(move(["a", "b", "c"], 2, 1)).toEqual(["a", "b", "c"]);
  });

  it("validates the owner's dialog", () => {
    expect(validateApp(" ", [])).toEqual({ name: "Enter a name for the app.", items: "Add at least one form or report." });
    expect(validateApp("Shop", [{ kind: "form", object_id: "f", title: " " }])).toEqual({ items: "Give every page a title." });
    expect(validateApp("Shop", [{ kind: "form", object_id: "f", title: "Order" }])).toEqual({});
  });
});

import { describe, expect, it } from "vitest";
import type { Table } from "@/lib/schema";
import {
  TEMPLATES,
  bindTable,
  findTemplate,
  luaKey,
  luaString,
  moduleNameFor,
  sqlIdent,
  tablesFor,
  templatesFor,
  type BoundTable,
} from "./templates";

function tpl(id: string) {
  const t = findTemplate(id);
  if (!t) throw new Error(`no template ${id}`);
  return t;
}

const MODULE_NAME = /^[A-Za-z][A-Za-z0-9_.]{0,99}$/;

function col(ordinal: number, physical: string, display: string, type: string, hidden = false) {
  return { ordinal, physical_name: physical, display_name: display, field_type: type, is_hidden: hidden };
}

function makeTable(physical: string, display: string, extra: ReturnType<typeof col>[] = []): Table {
  return {
    physical_name: physical,
    display_name: display,
    columns: [
      col(7, "Total", "Order Total Amount", "number"),
      col(1, "Number", "Order Number Display", "autonumber"),
      col(2, 'Order "Date"', "When Ordered Display", "date"),
      col(3, "end", "Ends Display", "datetime"),
      col(4, "Name", "Customer Name Display", "short_text"),
      col(5, "Double", "Calculated Display", "calculated"),
      col(6, "Customer", "Customer Link Display", "lookup"),
      col(8, "secret", "Secret Display", "short_text", true),
      ...extra,
    ],
  } as unknown as Table;
}

const fixture = bindTable(makeTable("Order Items", "Order Items Display"));
const awkward = ["1 Sales.Orders", "my table", "a.b", "ünï", "", "Orders"].map((n) =>
  bindTable(makeTable(n, `${n} label`)),
);

describe("bindTable", () => {
  it("drops hidden columns and orders by ordinal", () => {
    expect(fixture.columns.map((c) => c.name)).toEqual([
      "Number", 'Order "Date"', "end", "Name", "Double", "Customer", "Total",
    ]);
    expect(fixture.name).toBe("Order Items");
  });
});

describe("escaping", () => {
  it("luaString", () => {
    expect(luaString('a"b\\c\nd\re')).toBe('"a\\"b\\\\c\\nd\\re"');
  });
  it("sqlIdent", () => {
    expect(sqlIdent('Order "Date"')).toBe('"Order ""Date"""');
  });
  it("luaKey", () => {
    expect(luaKey("Name")).toBe("Name");
    expect(luaKey("end")).toBe('["end"]');
    expect(luaKey("a b")).toBe('["a b"]');
    expect(luaKey("1a")).toBe('["1a"]');
  });
});

describe("catalog", () => {
  it("has unique ids and complete metadata", () => {
    const ids = TEMPLATES.map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids).toEqual([
      "job.count-rows", "job.export-csv", "job.import-json", "job.clean-old-rows", "job.blank",
      "module.helpers", "module.table-access",
    ]);
    for (const t of TEMPLATES) {
      expect(["job", "module"]).toContain(t.kind);
      expect(t.title.trim()).not.toBe("");
      expect(t.description.trim()).not.toBe("");
      expect(Boolean(t.job)).toBe(t.kind === "job");
      expect(findTemplate(t.id)).toBe(t);
    }
    expect(templatesFor("job")).toHaveLength(5);
    expect(templatesFor("module")).toHaveLength(2);
    expect(findTemplate("nope")).toBeUndefined();
  });

  it("tablesFor filters clean-old-rows to tables with a date column", () => {
    const noDate: BoundTable = { name: "T", label: "T", columns: [{ name: "a", label: "a", type: "short_text" }] };
    const dateOnly: BoundTable = { name: "D", label: "D", columns: [{ name: "d", label: "d", type: "date" }] };
    const clean = tpl("job.clean-old-rows");
    expect(tablesFor(clean, [noDate, dateOnly, fixture])).toEqual([dateOnly, fixture]);
    expect(tablesFor(tpl("job.count-rows"), [noDate, dateOnly])).toHaveLength(2);
  });
});

function sources() {
  const out: { id: string; table: BoundTable | null; text: string; name: string }[] = [];
  for (const t of TEMPLATES) {
    const tables: (BoundTable | null)[] = [...tablesFor(t, [fixture, ...awkward])];
    if (t.table !== "required") tables.push(null);
    for (const b of tables) out.push({ id: t.id, table: b, text: t.source(b), name: t.name(b) });
  }
  return out;
}

describe("generated sources", () => {
  it("are tidy Lua text", () => {
    for (const s of sources()) {
      expect(s.text.endsWith("\n"), s.id).toBe(true);
      expect(s.text.endsWith("\n\n"), s.id).toBe(false);
      expect(s.text, s.id).not.toMatch(/[ \t]+\n/);
      expect(s.text, s.id).not.toMatch(/\t/);
      expect(s.name.length, s.id).toBeGreaterThan(0);
      expect(s.name.length, s.id).toBeLessThanOrEqual(200);
    }
  });

  it("modules return M and have valid names", () => {
    for (const s of sources().filter((x) => tpl(x.id).kind === "module")) {
      expect(s.text.trimEnd().endsWith("return M"), s.id).toBe(true);
      expect(s.name, s.id).toMatch(MODULE_NAME);
    }
    for (const raw of ["1 Sales.Orders", "my table", "a.b", "", "ünï"]) {
      expect(moduleNameFor(raw)).toMatch(MODULE_NAME);
    }
    expect(moduleNameFor("1 Sales.Orders")).toBe("t1_sales_orders_data");
    expect(tpl("module.helpers").name(null)).toBe("helpers");
  });

  it("modules avoid job-only modules", () => {
    for (const s of sources().filter((x) => tpl(x.id).kind === "module")) {
      expect(s.text, s.id).not.toMatch(/\b(fetch|artifacts|staging|job)\b[.(]/);
    }
  });

  it("quotes physical names and keeps display names out of code", () => {
    const get = (id: string, t: BoundTable | null = fixture) => tpl(id).source(t);

    const count = get("job.count-rows");
    expect(count).toContain('db.query_value("SELECT count(*) FROM \\"Order Items\\"")');
    expect(count).toContain('"Rows in Order Items Display"'); // log message, not SQL

    const csv = get("job.export-csv");
    expect(csv).toContain(
      'SELECT \\"Number\\", \\"Order \\"\\"Date\\"\\"\\", \\"end\\", \\"Name\\", \\"Double\\", \\"Customer\\", \\"Total\\" FROM \\"Order Items\\" ORDER BY \\"id\\" LIMIT :limit',
    );
    expect(csv).toContain('{columns = {"Number", "Order \\"Date\\"", "end"');
    expect(csv).toContain('artifacts.save("Order Items.csv"');
    expect(csv).toContain('mime = "text/csv"');

    const imp = get("job.import-json");
    expect(imp).toContain('records.insert("Order Items", {');
    expect(imp).toContain('["Order \\"Date\\""] = item["Order \\"Date\\""],');
    expect(imp).toContain('["end"] = item["end"],');
    expect(imp).toContain("Name = item.Name,");
    expect(imp).toContain("Customer = item.Customer, -- id of the related row");
    expect(imp).not.toMatch(/Number = item/);
    expect(imp).not.toMatch(/Double = item/);
    expect(imp).not.toContain("secret");
    expect(imp).toContain("egress allowlist");

    const clean = get("job.clean-old-rows");
    expect(clean).toContain('WHERE \\"Order \\"\\"Date\\"\\"\\" < :cutoff');
    expect(clean).toContain("datetime.today()");
    expect(clean).toContain('records.delete("Order Items", row.id)');

    const access = get("module.table-access");
    expect(access).toContain('local TABLE_SQL = "\\"Order Items\\""');
    expect(access).toContain('["Order \\"Date\\""] = true,');
    expect(access).toContain("--   Order \"Date\" - When Ordered Display - date");

    for (const s of sources()) {
      if (!s.table) continue;
      const sqlLines = s.text.split("\n").filter((l) => /SELECT|FROM|WHERE/.test(l) && !l.trim().startsWith("--"));
      for (const l of sqlLines) {
        expect(l, s.id).not.toContain("Display");
        expect(l, s.id).not.toContain("label");
      }
    }
  });

  it("import without a table logs items", () => {
    const text = tpl("job.import-json").source(null);
    expect(text).toContain("fetch(url)");
    expect(text).toContain("res.ok");
    expect(text).toContain("log.info");
    expect(text).not.toMatch(/^\s*records\.insert/m);
  });

  it("clean-old-rows uses a timestamp for datetime columns", () => {
    const t: BoundTable = { name: "Log", label: "Log", columns: [{ name: "at", label: "At", type: "datetime" }] };
    expect(tpl("job.clean-old-rows").source(t)).toContain("datetime.now()");
  });

  it("table-access lists only the columns it was given", () => {
    const text = tpl("module.table-access").source(fixture);
    expect(text).not.toContain("secret");
    expect(text).toContain("error(");
    expect(text).toContain("COLUMNS[column]");
  });
});

import { describe, expect, it } from "vitest";
import { cellTone, operatorsFor, parseRules, rowTone, ruleMatches, type FormattingRule } from "./conditionalFormatting";
import { col } from "./testutil";

const rule = (over: Partial<FormattingRule>): FormattingRule => ({ id: "r", columnId: "c", operator: "eq", target: "cell", tone: "danger", ...over });
const NOW = new Date(2026, 5, 15, 12, 0, 0); // 15 June 2026, local

describe("ruleMatches", () => {
  it("compares text, numbers and yes/no", () => {
    const text = col("c", "short_text");
    expect(ruleMatches(rule({ value: "Overdue" }), text, "Overdue")).toBe(true);
    expect(ruleMatches(rule({ value: "Overdue" }), text, "Paid")).toBe(false);
    expect(ruleMatches(rule({ operator: "ne", value: "Overdue" }), text, "Paid")).toBe(true);
    const qty = col("c", "integer");
    expect(ruleMatches(rule({ operator: "lt", value: "5" }), qty, 3)).toBe(true);
    expect(ruleMatches(rule({ operator: "lt", value: "5" }), qty, 5)).toBe(false);
    expect(ruleMatches(rule({ operator: "gt", value: "5" }), qty, 9)).toBe(true);
    expect(ruleMatches(rule({ operator: "gt", value: "5" }), qty, null)).toBe(false);
    const paid = col("c", "yes_no");
    expect(ruleMatches(rule({ value: "false" }), paid, false)).toBe(true);
    expect(ruleMatches(rule({ value: "true" }), paid, false)).toBe(false);
  });

  it("compares currency by amount, not text", () => {
    const money = col("c", "currency");
    expect(ruleMatches(rule({ operator: "gt", value: "100" }), money, "250.0000")).toBe(true);
    expect(ruleMatches(rule({ value: "250" }), money, "250.0000")).toBe(true);
  });

  it("never matches a value that does not parse for the field", () => {
    expect(ruleMatches(rule({ operator: "gt", value: "abc" }), col("c", "integer"), 7)).toBe(false);
    expect(ruleMatches(rule({ value: "" }), col("c", "short_text"), "x")).toBe(false);
  });

  it("matches substrings ignoring case, and empty values", () => {
    const text = col("c", "short_text");
    expect(ruleMatches(rule({ operator: "contains", value: "ush" }), text, "RUSH order")).toBe(true);
    expect(ruleMatches(rule({ operator: "contains", value: "" }), text, "x")).toBe(false);
    expect(ruleMatches(rule({ operator: "is_null" }), text, "")).toBe(true);
    expect(ruleMatches(rule({ operator: "is_null" }), text, null)).toBe(true);
    expect(ruleMatches(rule({ operator: "is_null" }), text, "x")).toBe(false);
  });

  it("flags dates before today, and date-times already past, as overdue", () => {
    const due = col("c", "date");
    expect(ruleMatches(rule({ operator: "is_overdue" }), due, "2026-06-14", undefined, NOW)).toBe(true);
    expect(ruleMatches(rule({ operator: "is_overdue" }), due, "2026-06-15", undefined, NOW)).toBe(false);
    expect(ruleMatches(rule({ operator: "is_overdue" }), due, "2026-07-01", undefined, NOW)).toBe(false);
    expect(ruleMatches(rule({ operator: "is_overdue" }), due, null, undefined, NOW)).toBe(false);
    const at = col("c", "datetime");
    expect(ruleMatches(rule({ operator: "is_overdue" }), at, new Date(2026, 5, 15, 11, 0).toISOString(), undefined, NOW)).toBe(true);
    expect(ruleMatches(rule({ operator: "is_overdue" }), at, new Date(2026, 5, 15, 13, 0).toISOString(), undefined, NOW)).toBe(false);
    expect(ruleMatches(rule({ operator: "is_overdue" }), col("c", "short_text"), "2020-01-01", undefined, NOW)).toBe(false);
  });

  it("matches a lookup by its shown text", () => {
    expect(ruleMatches(rule({ value: "acme" }), col("c", "lookup"), 4, "Acme")).toBe(true);
  });
});

describe("rule lists", () => {
  const status = col("status", "short_text");
  const qty = col("qty", "integer");
  const rules: FormattingRule[] = [
    rule({ id: "a", columnId: "qty", operator: "lt", value: "5", tone: "warning" }),
    rule({ id: "b", columnId: "qty", operator: "lt", value: "10", tone: "danger" }),
    rule({ id: "c", columnId: "status", value: "Late", target: "row", tone: "success" }),
  ];

  it("takes the first matching cell rule for the column", () => {
    expect(cellTone(rules, qty, 3, undefined)).toBe("warning");
    expect(cellTone(rules, qty, 7, undefined)).toBe("danger");
    expect(cellTone(rules, qty, 50, undefined)).toBeNull();
    expect(cellTone(rules, status, "Late", undefined)).toBeNull(); // the row rule is not a cell rule
  });

  it("evaluates row rules against the record's fields", () => {
    const byId = new Map([status, qty].map((c) => [c.id, c]));
    const of = (s: string) => (c: typeof status) => ({ value: c.id === "status" ? s : 1 });
    expect(rowTone(rules, byId, of("Late"))).toBe("success");
    expect(rowTone(rules, byId, of("OK"))).toBeNull();
  });

  it("offers only the operators that fit a field", () => {
    expect(operatorsFor(col("c", "short_text"))).toEqual(["eq", "ne", "contains", "is_null"]);
    expect(operatorsFor(col("c", "integer"))).toEqual(["eq", "ne", "gt", "lt", "is_null"]);
    expect(operatorsFor(col("c", "date"))).toEqual(["eq", "ne", "gt", "lt", "is_overdue", "is_null"]);
    expect(operatorsFor(col("c", "yes_no"))).toEqual(["eq", "ne", "is_null"]);
  });
});

describe("parseRules", () => {
  it("keeps valid rules and drops malformed ones", () => {
    const good = rule({ id: "x", value: "v" });
    expect(parseRules([good, { id: "y" }, null, { ...good, tone: "purple" }, { ...good, target: "page" }, { ...good, operator: "regex" }])).toEqual([good]);
    expect(parseRules("nope")).toEqual([]);
  });
});

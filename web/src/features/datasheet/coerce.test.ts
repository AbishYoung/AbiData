import { describe, expect, it } from "vitest";
import { excelSerialToDate, normalizeDate, normalizeDateTime, normalizeNumber, normalizeTime, parseBooleanText } from "./coerce";

describe("normalizeNumber", () => {
  it.each([
    ["1234", "1234"],
    ["1,234", "1234"],
    ["1,234.50", "1234.50"],
    ["$1,234.5", "1234.5"],
    ["-$5", "-5"],
    ["$-5", "-5"],
    ["€ 1.234,50", "1234.50"],
    ["1.234,50 €", "1234.50"],
    ["£1,000,000", "1000000"],
    ["¥1000", "1000"],
    ["₹ 12,345.00", "12345.00"],
    ["CHF 1'234.50", "1234.50"],
    ["R$ 99,90", "99.90"],
    ["12,5", "12.5"],
    ["1.5", "1.5"],
    ["1.234.567", "1234567"],
    ["1 234,5", "1234.5"],
    ["(1,234.50)", "-1234.50"],
    ["($12)", "-12"],
    ["1234-", "-1234"],
    ["−7", "-7"],
    ["USD 15", "15"],
    ["15 kr", "15"],
    [".5", "0.5"],
  ])("reads %j as %j", (input, want) => {
    expect(normalizeNumber(input)).toBe(want);
  });

  it.each(["", "abc", "1-4", "12a", "1,2,3,x", "$", "1.2.3,4", "--5", "1e5 apples"])("rejects %j", (input) => {
    expect(normalizeNumber(input)).toBeNull();
  });
});

describe("normalizeDate", () => {
  it.each([
    ["2026-10-24", "mdy", "2026-10-24"],
    ["10/24/2026", "mdy", "2026-10-24"],
    ["24/10/2026", "dmy", "2026-10-24"],
    ["24/10/2026", "mdy", "2026-10-24"],
    ["3/2/2026", "mdy", "2026-03-02"],
    ["3/2/2026", "dmy", "2026-02-03"],
    ["24.10.2026", "dmy", "2026-10-24"],
    ["24-Oct-2026", "mdy", "2026-10-24"],
    ["24 October 2026", "mdy", "2026-10-24"],
    ["Oct 24, 2026", "mdy", "2026-10-24"],
    ["October 24th 2026", "mdy", "2026-10-24"],
    ["1/2/26", "mdy", "2026-01-02"],
    ["45589", "mdy", "2024-10-24"],
  ] as const)("reads %j (%s) as %j", (input, order, want) => {
    expect(normalizeDate(input, order)).toBe(want);
  });

  it.each(["", "31/02/2026", "13/13/2026", "24-Foo-2026", "marketing 24 2026", "123", "2026"])("rejects %j", (input) => {
    expect(normalizeDate(input)).toBeNull();
  });

  it("converts Excel serials", () => {
    expect(excelSerialToDate(45589)).toBe("2024-10-24");
    expect(excelSerialToDate(45292)).toBe("2024-01-01");
    expect(excelSerialToDate(5)).toBeNull();
  });
});

describe("normalizeTime and normalizeDateTime", () => {
  it("reads 12 and 24 hour times", () => {
    expect(normalizeTime("14:30")).toBe("14:30");
    expect(normalizeTime("2:30 PM")).toBe("14:30");
    expect(normalizeTime("12:05 AM")).toBe("00:05");
    expect(normalizeTime("12:05 pm")).toBe("12:05");
    expect(normalizeTime("23:59:59")).toBe("23:59:59");
    expect(normalizeTime("25:00")).toBeNull();
    expect(normalizeTime("13:00 PM")).toBeNull();
  });

  it("joins a date and a time", () => {
    expect(normalizeDateTime("2026-02-03 14:30")).toBe("2026-02-03T14:30");
    expect(normalizeDateTime("10/24/2026 2:30 PM", "mdy")).toBe("2026-10-24T14:30");
    expect(normalizeDateTime("24/10/2026")).toBe("2026-10-24T00:00");
    expect(normalizeDateTime("45589.5")).toBe("2024-10-24T12:00:00");
    expect(normalizeDateTime("2026-02-03T10:00:00Z")).toBe("2026-02-03T10:00:00.000Z");
    expect(normalizeDateTime("2026-02-03T10:00:00+02:00")).toBe("2026-02-03T08:00:00.000Z");
    expect(normalizeDateTime("tomorrow")).toBeNull();
    expect(normalizeDateTime("2026-02-03 99:00")).toBeNull();
  });
});

describe("parseBooleanText", () => {
  it("accepts the common spellings", () => {
    for (const t of ["TRUE", "yes", "Y", "t", "1", "✓", "Checked", "x"]) expect(parseBooleanText(t)).toBe(true);
    for (const t of ["FALSE", "no", "N", "f", "0", "✗", "Unchecked"]) expect(parseBooleanText(t)).toBe(false);
    expect(parseBooleanText("maybe")).toBeUndefined();
  });
});

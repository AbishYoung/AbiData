import { describe, expect, it } from "vitest";
import { emptyQuotaForm, parseQuotaForm, quotaSummary, quotaToForm } from "./quota";

describe("parseQuotaForm", () => {
  it("leaves blank fields out so defaults apply", () => {
    const { quota, errors } = parseQuotaForm(emptyQuotaForm());
    expect(quota).toEqual({});
    expect(errors).toEqual({});
  });

  it("parses whole numbers and trims", () => {
    const form = { ...emptyQuotaForm(), max_page_count: " 5000 ", max_concurrent_jobs: "3" };
    const { quota, errors } = parseQuotaForm(form);
    expect(errors).toEqual({});
    expect(quota).toEqual({ max_page_count: 5000, max_concurrent_jobs: 3 });
  });

  it("treats 0 as the default", () => {
    expect(parseQuotaForm({ ...emptyQuotaForm(), egress_per_minute: "0" }).quota).toEqual({});
  });

  it("rejects non-numbers, negatives, decimals and over-limit values per field", () => {
    const form = {
      ...emptyQuotaForm(),
      max_page_count: "abc",
      artifact_bytes: "-5",
      max_concurrent_jobs: "1.5",
      script_concurrency: "1001",
      ui_event_budget_ms: "99999999999999999999",
    };
    const { quota, errors } = parseQuotaForm(form);
    expect(quota).toEqual({});
    expect(Object.keys(errors).sort()).toEqual(["artifact_bytes", "max_concurrent_jobs", "max_page_count", "script_concurrency", "ui_event_budget_ms"]);
  });
});

describe("quota helpers", () => {
  it("round-trips through the form", () => {
    const q = { max_page_count: 100, script_concurrency: 2 };
    expect(parseQuotaForm(quotaToForm(q)).quota).toEqual(q);
  });
  it("summarises", () => {
    expect(quotaSummary({})).toBe("Platform defaults");
    expect(quotaSummary({ max_concurrent_jobs: 4 })).toContain("Concurrent jobs: 4");
  });
});

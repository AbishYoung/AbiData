import { describe, expect, it } from "vitest";
import { demoSummary } from "./adminApi";

describe("demoSummary", () => {
  it("counts people created and skipped", () => {
    expect(demoSummary({ created: ["a@x.test", "b@x.test"], skipped: ["c@x.test"], failed: [] }, "traders")).toBe(
      "Created a Northwind Traders workspace for 2 people; 1 person already had one.",
    );
  });
  it("says when nothing was needed", () => {
    expect(demoSummary({ created: [], skipped: ["a@x.test", "b@x.test"], failed: [] }, "invasives")).toBe(
      "No new Northwind Invasives workspaces were needed; 2 people already had one.",
    );
  });
  it("reads lists the server sent as null (empty Go slices)", () => {
    const empty = { created: null, skipped: null, failed: null } as unknown as Parameters<typeof demoSummary>[0];
    expect(demoSummary(empty, "traders")).toBe("No new Northwind Traders workspaces were needed.");
    const one = { created: ["a@x.test"], skipped: null, failed: null } as unknown as Parameters<typeof demoSummary>[0];
    expect(demoSummary(one, "invasives")).toBe("Created a Northwind Invasives workspace for 1 person.");
  });
  it("names the people it could not serve", () => {
    expect(demoSummary({ created: ["a@x.test"], skipped: [], failed: [{ email: "b@x.test", message: "m" }] }, "traders")).toBe(
      "Created a Northwind Traders workspace for 1 person. Could not create one for b@x.test: see the server log.",
    );
  });
});

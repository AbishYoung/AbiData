import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { handleEvent } from "./events";

function msg(data: unknown): MessageEvent {
  return new MessageEvent("message", { data: JSON.stringify(data) });
}

describe("handleEvent design.changed", () => {
  it("invalidates the cache of the changed object kind only", () => {
    const qc = new QueryClient();
    for (const k of ["forms", "queries", "reports", "scripts"]) qc.setQueryData([k, "w1", "list"], []);
    qc.setQueryData(["forms", "w2", "list"], []);
    handleEvent(qc, "w1", "design.changed", msg({ kind: "form", id: "f1", version: 2 }));
    const stale = (k: unknown[]) => qc.getQueryState(k)?.isInvalidated;
    expect(stale(["forms", "w1", "list"])).toBe(true);
    expect(stale(["forms", "w2", "list"])).toBe(false);
    expect(stale(["queries", "w1", "list"])).toBe(false);
    handleEvent(qc, "w1", "design.changed", msg({ kind: "report", id: "r1", deleted: true }));
    expect(stale(["reports", "w1", "list"])).toBe(true);
    handleEvent(qc, "w1", "design.changed", msg({ kind: "nonsense" }));
    handleEvent(qc, "w1", "reset", msg({}));
    expect(stale(["scripts", "w1", "list"])).toBe(true);
  });
});

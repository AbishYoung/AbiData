import { QueryClient } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { handleEvent } from "./events";
import { operationArtifactUrl, parseProgress, useActiveOperations, useActiveOperationsStore } from "./operations";

function msg(data: unknown): MessageEvent {
  return new MessageEvent("message", { data: JSON.stringify(data) });
}

const running = { id: "op1", kind: "import", status: "running", progress: 0.4, message: "Row 40", error_code: "", error_message: "" };

beforeEach(() => useActiveOperationsStore.setState({ byWs: {} }));

describe("parseProgress", () => {
  it("maps and clamps the payload", () => {
    expect(parseProgress({ ...running, progress: 7 })).toMatchObject({ id: "op1", progress: 1, message: "Row 40" });
    expect(parseProgress({ id: "x" })).toBeNull();
    expect(parseProgress("nope")).toBeNull();
  });
});

describe("active operations store", () => {
  it("tracks running operations per workspace and drops finished ones", () => {
    const qc = new QueryClient();
    const { result } = renderHook(() => useActiveOperations("w1"));
    expect(result.current).toEqual([]);
    act(() => handleEvent(qc, "w1", "operation.progress", msg(running)));
    act(() => handleEvent(qc, "w2", "operation.progress", msg({ ...running, id: "other" })));
    expect(result.current.map((o) => o.id)).toEqual(["op1"]);
    act(() => handleEvent(qc, "w1", "operation.progress", msg({ ...running, progress: 0.9 })));
    expect(result.current[0]?.progress).toBe(0.9);
    act(() => handleEvent(qc, "w1", "operation.progress", msg({ ...running, status: "succeeded", progress: 1 })));
    expect(result.current).toEqual([]);
  });

  it("invalidates that operation's cached detail and clears on reset", () => {
    const qc = new QueryClient();
    qc.setQueryData(["operations", "w1", "op1"], {});
    handleEvent(qc, "w1", "operation.progress", msg(running));
    expect(qc.getQueryState(["operations", "w1", "op1"])?.isInvalidated).toBe(true);
    handleEvent(qc, "w1", "reset", msg({}));
    expect(useActiveOperationsStore.getState().byWs["w1"]).toBeUndefined();
  });
});

describe("handleEvent job.run", () => {
  it("invalidates the job's runs and the run detail only", () => {
    const qc = new QueryClient();
    qc.setQueryData(["jobs", "w1", "runs", "j1"], {});
    qc.setQueryData(["jobs", "w1", "runs", "j2"], {});
    qc.setQueryData(["jobs", "w1", "run", "r1"], {});
    handleEvent(qc, "w1", "job.run", msg({ id: "r1", job_id: "j1", status: "running" }));
    const stale = (k: unknown[]) => qc.getQueryState(k)?.isInvalidated;
    expect(stale(["jobs", "w1", "runs", "j1"])).toBe(true);
    expect(stale(["jobs", "w1", "run", "r1"])).toBe(true);
    expect(stale(["jobs", "w1", "runs", "j2"])).toBe(false);
  });
});

describe("operationArtifactUrl", () => {
  it("encodes path segments", () => {
    expect(operationArtifactUrl("w 1", "op", "a b.csv")).toBe("/api/v1/w/w%201/operations/op/artifacts/a%20b.csv");
  });
});

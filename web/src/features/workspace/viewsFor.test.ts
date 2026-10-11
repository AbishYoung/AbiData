import { describe, expect, it } from "vitest";
import type { Tab } from "@/app/tabs";
import { viewsFor } from "./WorkspaceShell";

const tab = (kind: Tab["kind"], objectId: string | null): Tab => ({ key: `${kind}:${objectId}`, kind, objectId, view: "datasheet", title: "T", dirty: false });

describe("viewsFor", () => {
  it("gives a SQL query only Datasheet and SQL (no redundant Design)", () => {
    expect(viewsFor(tab("query", "q1"), new Set(["q1"]))).toEqual(["datasheet", "sql"]);
  });
  it("keeps Design for visual queries and other objects", () => {
    expect(viewsFor(tab("query", "q2"), new Set(["q1"]))).toEqual(["datasheet", "design", "sql"]);
    expect(viewsFor(tab("table", "q1"), new Set(["q1"]))).toEqual(["datasheet", "design"]);
  });
});

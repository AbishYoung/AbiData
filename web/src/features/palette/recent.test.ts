import { beforeEach, describe, expect, it } from "vitest";
import {
  addRecent,
  formatRelativeTime,
  parseRecent,
  useRecent,
  type RecentObjectEntry,
} from "./recent";

describe("recent.ts", () => {
  beforeEach(() => {
    window.localStorage.clear();
    useRecent.setState({ entries: [] });
  });

  describe("addRecent", () => {
    it("puts new entry at head of the list", () => {
      const e1: RecentObjectEntry = {
        wsId: "w1",
        wsName: "Workspace 1",
        kind: "table",
        id: "t1",
        name: "Customers",
        timestamp: 1000,
      };
      const e2: RecentObjectEntry = {
        wsId: "w2",
        wsName: "Workspace 2",
        kind: "query",
        id: "q1",
        name: "Open Orders",
        timestamp: 2000,
      };

      const res = addRecent([e1], e2);
      expect(res).toHaveLength(2);
      expect(res[0]?.id).toBe("q1");
      expect(res[1]?.id).toBe("t1");
    });

    it("deduplicates existing object across access and brings it to the head", () => {
      const initial: RecentObjectEntry[] = [
        {
          wsId: "w1",
          wsName: "Workspace 1",
          kind: "table",
          id: "t1",
          name: "Customers",
          timestamp: 1000,
        },
        {
          wsId: "w2",
          wsName: "Workspace 2",
          kind: "query",
          id: "q1",
          name: "Open Orders",
          timestamp: 2000,
        },
      ];

      const accessed: RecentObjectEntry = {
        wsId: "w1",
        wsName: "Workspace 1",
        kind: "table",
        id: "t1",
        name: "Customers Updated",
        timestamp: 3000,
      };

      const res = addRecent(initial, accessed);
      expect(res).toHaveLength(2);
      expect(res[0]).toMatchObject({
        wsId: "w1",
        id: "t1",
        name: "Customers Updated",
        timestamp: 3000,
      });
      expect(res[1]?.id).toBe("q1");
    });

    it("caps list at 20 items (LRU eviction)", () => {
      let list: RecentObjectEntry[] = [];
      for (let i = 1; i <= 25; i++) {
        list = addRecent(list, {
          wsId: `w${i}`,
          wsName: `Workspace ${i}`,
          kind: "table",
          id: `t${i}`,
          name: `Table ${i}`,
          timestamp: i * 1000,
        });
      }

      expect(list).toHaveLength(20);
      expect(list[0]?.id).toBe("t25");
      expect(list[19]?.id).toBe("t6");
    });
  });

  describe("parseRecent", () => {
    it("parses valid json array of recent objects", () => {
      const json = JSON.stringify([
        {
          wsId: "w1",
          wsName: "Sales",
          kind: "table",
          id: "t1",
          name: "Leads",
          timestamp: 1000,
        },
        {
          wsId: "w2",
          wsName: "Ops",
          kind: "report",
          id: "r1",
          name: "Annual Review",
          timestamp: 2000,
        },
      ]);

      const res = parseRecent(json);
      expect(res).toHaveLength(2);
      expect(res[0]).toMatchObject({ wsId: "w1", wsName: "Sales", kind: "table", id: "t1", name: "Leads" });
      expect(res[1]).toMatchObject({ wsId: "w2", wsName: "Ops", kind: "report", id: "r1", name: "Annual Review" });
    });

    it("gracefully handles invalid JSON or unexpected shapes", () => {
      expect(parseRecent(null)).toEqual([]);
      expect(parseRecent("invalid json")).toEqual([]);
      expect(parseRecent(JSON.stringify([{ invalid: true }]))).toEqual([]);
    });
  });

  describe("formatRelativeTime", () => {
    it("formats relative time correctly", () => {
      const now = 1_000_000_000;
      expect(formatRelativeTime(now - 30_000, now)).toBe("Just now");
      expect(formatRelativeTime(now - 120_000, now)).toBe("2m ago");
      expect(formatRelativeTime(now - 3_600_000, now)).toBe("1h ago");
      expect(formatRelativeTime(now - 3 * 3_600_000, now)).toBe("3h ago");
      expect(formatRelativeTime(now - 86_400_000, now)).toBe("Yesterday");
      expect(formatRelativeTime(now - 3 * 86_400_000, now)).toBe("3d ago");
    });
  });

  describe("useRecent store", () => {
    it("records entries and persists to localStorage", () => {
      const entry: RecentObjectEntry = {
        wsId: "w1",
        wsName: "Engineering",
        kind: "form",
        id: "f1",
        name: "Bug Report",
        timestamp: Date.now(),
      };

      useRecent.getState().record(entry);
      expect(useRecent.getState().entries).toHaveLength(1);
      expect(useRecent.getState().entries[0]?.name).toBe("Bug Report");

      const saved = window.localStorage.getItem("abidata.palette.recent.v2");
      expect(saved).toBeTruthy();
      expect(JSON.parse(saved ?? "[]")[0].name).toBe("Bug Report");
    });
  });
});

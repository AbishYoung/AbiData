import { describe, expect, it } from "vitest";
import { ApiClient } from "@/lib/api";
import { applyChange, hasChanges, patchRequest, persistBuffer, persistWithMerge, rebaseBuffer, type RowBuffer } from "./buffer";
import { classifyFailure } from "./rowsApi";
import { col, row } from "./testutil";

const name = col("c_name", "short_text");
const amount = col("c_amount", "currency");
const note = col("c_note", "long_text");

interface Captured {
  method: string;
  url: string;
  ifMatch: string | null;
  idempotencyKey: string | null;
  body: unknown;
}

function clientReturning(status: number, body: unknown, captured: Captured[]): ApiClient {
  return new ApiClient({
    baseUrl: "http://localhost",
    fetch: async (req) => {
      captured.push({
        method: req.method,
        url: new URL(req.url).pathname,
        ifMatch: req.headers.get("If-Match"),
        idempotencyKey: req.headers.get("Idempotency-Key"),
        body: req.method === "GET" ? null : await req.clone().json().catch(() => null),
      });
      return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
    },
  });
}

describe("edit buffer", () => {
  const original = row(7, 3, { c_name: "Alice", c_amount: "10.0000", c_note: "hello" });

  it("records only fields that differ from the stored row", () => {
    let buf = applyChange(undefined, 7, original, name, "Alicia");
    expect(buf?.changes).toEqual({ c_name: "Alicia" });
    buf = applyChange(buf, 7, original, amount, "10.00");
    expect(buf?.changes).toEqual({ c_name: "Alicia" });
    buf = applyChange(buf, 7, original, note, "bye");
    expect(buf?.changes).toEqual({ c_name: "Alicia", c_note: "bye" });
    expect(buf?.baseVersion).toBe(3);
  });

  it("drops the buffer when the edit is reverted", () => {
    const buf = applyChange(undefined, 7, original, name, "Alicia");
    expect(applyChange(buf, 7, original, name, "Alice")).toBeUndefined();
  });

  it("clears a field error when the field is edited again", () => {
    const failed: RowBuffer = {
      ...(applyChange(undefined, 7, original, name, "") as RowBuffer),
      errors: { c_name: "required" },
    };
    const next = applyChange(failed, 7, original, name, "Bob");
    expect(next?.errors).toEqual({});
  });

  it("keeps new-record buffers without empty values", () => {
    const buf = applyChange(undefined, "new", undefined, name, "Zed");
    expect(buf?.baseVersion).toBeNull();
    expect(hasChanges(buf)).toBe(true);
    expect(hasChanges(applyChange(buf, "new", undefined, name, ""))).toBe(false);
  });

  it("builds the PATCH request from the changes and the base version", () => {
    let buf = applyChange(undefined, 7, original, name, "Alicia");
    buf = applyChange(buf, 7, original, amount, "12.3400");
    expect(buf && patchRequest(buf)).toEqual({ version: 3, fields: { c_name: "Alicia", c_amount: "12.3400" } });
  });
});

describe("persistBuffer", () => {
  const original = row(7, 3, { c_name: "Alice", c_amount: "10.0000", c_note: "hello" });

  it("PATCHes only changed fields with If-Match set to the version the user edited", async () => {
    const captured: Captured[] = [];
    const saved = row(7, 4, { c_name: "Alicia", c_amount: "10.0000", c_note: "hello" });
    const client = clientReturning(200, saved, captured);
    const buf = applyChange(undefined, 7, original, name, "Alicia");
    if (!buf) throw new Error("expected a buffer");
    const outcome = await persistBuffer("ws1", "t1", buf, client);
    expect(outcome).toEqual({ kind: "ok", row: saved, created: false });
    expect(captured).toHaveLength(1);
    expect(captured[0]).toMatchObject({
      method: "PATCH",
      url: "/api/v1/w/ws1/tables/t1/rows/7",
      ifMatch: '"3"',
      body: { fields: { c_name: "Alicia" } },
    });
    expect(captured[0]?.idempotencyKey).toBeTruthy();
  });

  it("sends currency as a decimal string", async () => {
    const captured: Captured[] = [];
    const client = clientReturning(200, original, captured);
    const buf = applyChange(undefined, 7, original, amount, "9007199254740993.1234");
    if (!buf) throw new Error("expected a buffer");
    await persistBuffer("ws1", "t1", buf, client);
    expect(captured[0]?.body).toEqual({ fields: { c_amount: "9007199254740993.1234" } });
  });

  it("POSTs a new record without If-Match", async () => {
    const captured: Captured[] = [];
    const created = row(8, 1, { c_name: "Zed" });
    const client = clientReturning(201, created, captured);
    const buf = applyChange(undefined, "new", undefined, name, "Zed");
    if (!buf) throw new Error("expected a buffer");
    const outcome = await persistBuffer("ws1", "t1", buf, client);
    expect(outcome).toEqual({ kind: "ok", row: created, created: true });
    expect(captured[0]).toMatchObject({ method: "POST", url: "/api/v1/w/ws1/tables/t1/rows", ifMatch: null, body: { fields: { c_name: "Zed" } } });
  });

  it("reports a concurrent modification with the server's current row", async () => {
    const current = row(7, 5, { c_name: "Theirs" });
    const envelope = {
      error: { code: "CONCURRENT_MODIFICATION", title: "Changed", message: "Someone else saved.", details: { current } },
    };
    const client = clientReturning(409, envelope, []);
    const buf = applyChange(undefined, 7, original, name, "Mine");
    if (!buf) throw new Error("expected a buffer");
    const outcome = await persistBuffer("ws1", "t1", buf, client);
    expect(outcome.kind).toBe("conflict");
    if (outcome.kind === "conflict") expect(outcome.current).toEqual(current);
  });

  it("maps field errors to column ids and leaves others for the global dialog", async () => {
    const field = clientReturning(422, { error: { code: "REQUIRED_FIELD", title: "Required", message: "Name is required.", details: { field: "c_name" } } }, []);
    const buf = applyChange(undefined, "new", undefined, note, "x");
    if (!buf) throw new Error("expected a buffer");
    const outcome = await persistBuffer("ws1", "t1", buf, field);
    expect(outcome.kind).toBe("fields");
    if (outcome.kind === "fields") expect(outcome.errors).toEqual({ c_name: "Name is required." });

    const dup = clientReturning(409, { error: { code: "DUPLICATE_VALUE", title: "Duplicate", message: "Already used.", details: { fields: ["a", "b"] } } }, []);
    const dupOutcome = await persistBuffer("ws1", "t1", buf, dup);
    if (dupOutcome.kind === "fields") expect(Object.keys(dupOutcome.errors)).toEqual(["a", "b"]);
    else throw new Error("expected field errors");

    const other = clientReturning(403, { error: { code: "FORBIDDEN", title: "No", message: "Not allowed." } }, []);
    expect((await persistBuffer("ws1", "t1", buf, other)).kind).toBe("error");
  });

  it("classifies unknown thrown values as errors", () => {
    expect(classifyFailure(new Error("boom")).kind).toBe("error");
  });
});

describe("base values", () => {
  const original = row(7, 3, { c_name: "Alice", c_amount: "10.0000", c_note: "hello" }, { c_name: "shown" });

  it("keeps the values a field had when editing started, even after further edits", () => {
    let buf = applyChange(undefined, 7, original, name, "A");
    buf = applyChange(buf, 7, original, name, "AB");
    expect(buf?.base).toEqual({ c_name: "Alice" });
    expect(buf?.baseDisplay).toEqual({ c_name: "shown" });
    buf = applyChange(buf, 7, original, note, "bye");
    expect(buf?.base).toEqual({ c_name: "Alice", c_note: "hello" });
  });

  it("forgets the base of a field the user reverted", () => {
    let buf = applyChange(undefined, 7, original, name, "A");
    buf = applyChange(buf, 7, original, note, "bye");
    buf = applyChange(buf, 7, original, name, "Alice");
    expect(buf?.changes).toEqual({ c_note: "bye" });
    expect(buf?.base).toEqual({ c_note: "hello" });
    expect(applyChange(buf, 7, original, note, "hello")).toBeUndefined();
  });

  it("rebases onto the server record the user resolved against", () => {
    const buf = applyChange(undefined, 7, original, name, "Mine") as RowBuffer;
    const current = row(7, 9, { c_name: "Theirs" }, { c_name: "T" });
    expect(rebaseBuffer(buf, current, { c_name: "Mine" })).toMatchObject({
      baseVersion: 9,
      changes: { c_name: "Mine" },
      base: { c_name: "Theirs" },
      baseDisplay: { c_name: "T" },
    });
  });
});

describe("persistWithMerge", () => {
  const columns = [name, amount, note];
  const original = row(7, 3, { c_name: "Alice", c_amount: "10.0000", c_note: "hello" });

  /** A server that answers PATCHes from a script of statuses and bodies, recording what it received. */
  function scripted(replies: { status: number; body: unknown }[], captured: Captured[]): ApiClient {
    let i = 0;
    return new ApiClient({
      baseUrl: "http://localhost",
      fetch: async (req) => {
        captured.push({
          method: req.method,
          url: new URL(req.url).pathname,
          ifMatch: req.headers.get("If-Match"),
          idempotencyKey: null,
          body: await req.clone().json().catch(() => null),
        });
        const r = replies[Math.min(i++, replies.length - 1)] as { status: number; body: unknown };
        return new Response(JSON.stringify(r.body), { status: r.status, headers: { "Content-Type": "application/json" } });
      },
    });
  }
  const conflict = (current: unknown) => ({
    status: 409,
    body: { error: { code: "CONCURRENT_MODIFICATION", title: "Changed", message: "Someone else saved.", details: { current } } },
  });

  function edited(): RowBuffer {
    let buf = applyChange(undefined, 7, original, name, "Mine");
    buf = applyChange(buf, 7, original, note, "mine note");
    return buf as RowBuffer;
  }

  it("retries silently with only my fields when the other user changed different ones", async () => {
    const captured: Captured[] = [];
    const theirs = row(7, 4, { c_name: "Alice", c_amount: "11.0000", c_note: "hello" });
    const saved = row(7, 5, { c_name: "Mine", c_amount: "11.0000", c_note: "mine note" });
    const client = scripted([conflict(theirs), { status: 200, body: saved }], captured);
    const outcome = await persistWithMerge("ws1", "t1", columns, edited(), client);
    expect(outcome).toEqual({ kind: "ok", row: saved, created: false, merged: true });
    expect(captured).toHaveLength(2);
    expect(captured[1]).toMatchObject({ ifMatch: '"4"', body: { fields: { c_name: "Mine", c_note: "mine note" } } });
  });

  it("returns the conflicting field for the dialog and the rest as auto", async () => {
    const captured: Captured[] = [];
    const theirs = row(7, 4, { c_name: "Theirs", c_amount: "10.0000", c_note: "hello" });
    const outcome = await persistWithMerge("ws1", "t1", columns, edited(), scripted([conflict(theirs)], captured));
    expect(captured).toHaveLength(1);
    expect(outcome.kind).toBe("merge");
    if (outcome.kind !== "merge") return;
    expect(outcome.fields.map((f) => f.colId)).toEqual(["c_name"]);
    expect(outcome.fields[0]).toMatchObject({ base: "Alice", mine: "Mine", theirs: "Theirs" });
    expect(outcome.auto).toEqual({ c_note: "mine note" });
    expect(outcome.current).toEqual(theirs);
  });

  it("does nothing more when the server already holds my values", async () => {
    const captured: Captured[] = [];
    const theirs = row(7, 4, { c_name: "Mine", c_amount: "10.0000", c_note: "mine note" });
    const outcome = await persistWithMerge("ws1", "t1", columns, edited(), scripted([conflict(theirs)], captured));
    expect(outcome).toEqual({ kind: "ok", row: theirs, created: false });
    expect(captured).toHaveLength(1);
  });

  it("merges again when another save lands during the retry, then turns real conflicts into the dialog", async () => {
    const captured: Captured[] = [];
    const first = row(7, 4, { c_name: "Alice", c_amount: "11.0000", c_note: "hello" });
    const second = row(7, 5, { c_name: "Theirs", c_amount: "11.0000", c_note: "hello" });
    const outcome = await persistWithMerge("ws1", "t1", columns, edited(), scripted([conflict(first), conflict(second)], captured));
    expect(captured).toHaveLength(2);
    expect(outcome.kind).toBe("merge");
    if (outcome.kind === "merge") {
      expect(outcome.fields.map((f) => f.colId)).toEqual(["c_name"]);
      expect(outcome.current._version).toBe(5);
    }
  });

  it("gives up after the bounded number of retries and asks about everything still differing", async () => {
    const captured: Captured[] = [];
    const versions = [4, 5, 6, 7, 8].map((v) => conflict(row(7, v, { c_name: "Alice", c_amount: String(v), c_note: "hello" })));
    const outcome = await persistWithMerge("ws1", "t1", columns, edited(), scripted(versions, captured), 3);
    expect(captured).toHaveLength(4); // the first save plus three retries
    expect(outcome.kind).toBe("merge");
    if (outcome.kind === "merge") {
      expect(outcome.fields.map((f) => f.colId).sort()).toEqual(["c_name", "c_note"]);
      expect(outcome.auto).toEqual({});
    }
  });

  it("passes other failures through", async () => {
    const outcome = await persistWithMerge("ws1", "t1", columns, edited(), scripted([{ status: 404, body: { error: { code: "NOT_FOUND", title: "Gone", message: "Deleted." } } }], []));
    expect(outcome.kind).toBe("error");
  });
});

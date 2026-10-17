import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./api";
import { useActivityStore } from "./activity";
import { useSessionStore } from "./sessionStore";

const BASE = "http://localhost";

function json(body: unknown, status: number, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

const envelope = {
  error: {
    code: "FORBIDDEN",
    title: "Not allowed",
    message: "You may not do that.",
    recovery: "Ask an owner.",
    request_id: "req-1",
    details: { field: "x" },
  },
};

function client(fetchFn: (r: Request) => Promise<Response>) {
  return new ApiClient({ baseUrl: BASE, fetch: fetchFn });
}

function login(c: ApiClient, opts = {}) {
  return c.requestData(
    (cl, init) => cl.POST("/api/v1/auth/login", { ...init, body: { email: "a@b.c", password: "pw" } }),
    opts,
  );
}

function me(c: ApiClient) {
  return c.request((cl, init) => cl.GET("/api/v1/auth/me", init));
}

async function failure(p: Promise<unknown>): Promise<ApiError> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiError) return e;
    throw e;
  }
  throw new Error("expected rejection");
}

beforeEach(() => {
  useSessionStore.getState().setCsrf(null);
  useActivityStore.setState({ activities: [], operations: 0 });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ApiError normalization", () => {
  it("maps the JSON envelope", async () => {
    const c = client(async () => json(envelope, 403));
    const err = await failure(login(c));
    expect(err).toMatchObject({
      status: 403,
      code: "FORBIDDEN",
      title: "Not allowed",
      message: "You may not do that.",
      recovery: "Ask an owner.",
      requestId: "req-1",
      details: { field: "x" },
    });
  });

  it("maps a non-JSON body to INTERNAL with the status and header request id", async () => {
    const c = client(async () => new Response("<html>bad gateway</html>", { status: 502, headers: { "X-Request-ID": "hdr-9" } }));
    const err = await failure(login(c));
    expect(err.code).toBe("INTERNAL");
    expect(err.status).toBe(502);
    expect(err.requestId).toBe("hdr-9");
    expect(err.message).toContain("502");
  });

  it("maps an empty body to INTERNAL with the status", async () => {
    const c = client(async () => new Response(null, { status: 500 }));
    const err = await failure(login(c));
    expect(err.code).toBe("INTERNAL");
    expect(err.status).toBe(500);
  });

  it("maps a network failure to INTERNAL with status 0", async () => {
    const c = client(async () => {
      throw new TypeError("Failed to fetch");
    });
    const err = await failure(login(c));
    expect(err.code).toBe("INTERNAL");
    expect(err.status).toBe(0);
    expect(err.message).toMatch(/could not be reached/);
  });

  it("maps a timeout without claiming anything was or was not saved", async () => {
    const c = client(
      (r) =>
        new Promise<Response>((_resolve, reject) => {
          r.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
        }),
    );
    const err = await failure(login(c, { timeoutMs: 20 }));
    expect(err.code).toBe("INTERNAL");
    expect(err.title).toBe("Request timed out");
    expect(err.message).toBe("The server did not answer in time. The change may or may not have been saved.");
  });
});

describe("headers", () => {
  it("sends the same Idempotency-Key when retry() resends a POST", async () => {
    const keys: (string | null)[] = [];
    let calls = 0;
    const c = client(async (r) => {
      keys.push(r.headers.get("Idempotency-Key"));
      calls += 1;
      return calls === 1 ? json(envelope, 500) : json({ csrf_token: "t", memberships: [], user: {} }, 200);
    });
    const err = await failure(login(c));
    expect(err.retry).toBeTypeOf("function");
    await err.retry?.();
    expect(keys).toHaveLength(2);
    expect(keys[0]).toMatch(/^[0-9a-f-]{36}$/);
    expect(keys[1]).toBe(keys[0]);
  });

  it("sends CSRF only on unsafe methods and Idempotency-Key only on mutations", async () => {
    useSessionStore.getState().setCsrf("csrf-123");
    const seen: Record<string, Headers> = {};
    const c = client(async (r) => {
      seen[r.method] = r.headers;
      return json({}, 200);
    });
    await me(c);
    await login(c);
    expect(seen.GET?.get("X-CSRF-Token")).toBeNull();
    expect(seen.GET?.get("Idempotency-Key")).toBeNull();
    expect(seen.POST?.get("X-CSRF-Token")).toBe("csrf-123");
    expect(seen.POST?.get("Idempotency-Key")).not.toBeNull();
  });
});

describe("interactive activity", () => {
  it("flags the activity slow after 3 s and Cancel aborts only that request", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const aborted: string[] = [];
    const c = client(
      (r) =>
        new Promise<Response>((_resolve, reject) => {
          r.signal.addEventListener("abort", () => {
            aborted.push(r.method);
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const first = failure(me(c));
    const second = login(c).catch(() => undefined);

    expect(useActivityStore.getState().activities).toHaveLength(2);
    expect(useActivityStore.getState().activities.some((a) => a.slow)).toBe(false);

    await vi.advanceTimersByTimeAsync(3000);
    const activities = useActivityStore.getState().activities;
    expect(activities.every((a) => a.slow)).toBe(true);

    const [target] = activities;
    target?.cancel();
    const err = await first;
    expect(err.canceled).toBe(true);
    expect(err.retry).toBeUndefined(); // a cancelled request offers no "Try again"
    expect(aborted).toEqual(["GET"]);
    expect(useActivityStore.getState().activities).toHaveLength(1);

    useActivityStore.getState().activities[0]?.cancel();
    await second;
    expect(aborted).toEqual(["GET", "POST"]);
    expect(useActivityStore.getState().activities).toHaveLength(0);
  });

  it("does not register background requests as activities", async () => {
    const c = client(async () => json({}, 200));
    const p = c.request((cl, init) => cl.GET("/api/v1/auth/me", init), { class: "background" });
    expect(useActivityStore.getState().activities).toHaveLength(0);
    await p;
  });
});

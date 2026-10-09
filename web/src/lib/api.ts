import createClient, { type Client, type Middleware } from "openapi-fetch";
import type { components, paths } from "@/generated/api";
import { useActivityStore } from "./activity";
import { useSessionStore } from "./sessionStore";

export type ErrorCode = components["schemas"]["APIError"]["error"]["code"];

export type RequestClass = "interactive" | "background";

const INTERACTIVE_TIMEOUT_MS = 15_000;
const BACKGROUND_TIMEOUT_MS = 60_000;
const SLOW_AFTER_MS = 3_000;
const UNSAFE_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

export interface ApiErrorInit {
  status: number;
  code: ErrorCode;
  title: string;
  message: string;
  recovery?: string;
  requestId?: string;
  details?: Record<string, unknown>;
  canceled?: boolean;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: ErrorCode;
  readonly title: string;
  readonly recovery: string;
  readonly requestId: string;
  readonly details: Record<string, unknown> | undefined;
  /** True when the user cancelled the request; no error dialog should be shown. */
  readonly canceled: boolean;
  /** Resends the same logical request with the same Idempotency-Key. */
  retry?: () => Promise<unknown>;

  constructor(init: ApiErrorInit) {
    super(init.message);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.title = init.title;
    this.recovery = init.recovery ?? "";
    this.requestId = init.requestId ?? "";
    this.details = init.details;
    this.canceled = init.canceled ?? false;
  }
}

const KNOWN_CODES: ReadonlySet<string> = new Set([
  "BACKUP_INVALID", "BAD_REQUEST", "CONCURRENT_MODIFICATION", "DUPLICATE_VALUE", "EGRESS_DENIED",
  "EGRESS_FAILED", "EGRESS_RESPONSE_TOO_LARGE", "FORBIDDEN", "IDEMPOTENCY_CONFLICT",
  "IMPORT_ROW_ERRORS", "INTERNAL", "LINKED_RECORDS", "NOT_FOUND", "QUOTA_EXCEEDED", "RATE_LIMITED",
  "REQUIRED_FIELD", "RESTORE_FAILED", "SCHEMA_CHANGE_BLOCKED", "SCRIPT_ERROR",
  "SCRIPT_LIMIT_EXCEEDED", "SCRIPT_TIMEOUT", "SQL_NOT_ALLOWED", "UNAUTHENTICATED",
  "VALIDATION_FAILED", "WORKSPACE_BUSY", "WORKSPACE_MAINTENANCE",
]);

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function asErrorCode(v: unknown): ErrorCode | null {
  return typeof v === "string" && KNOWN_CODES.has(v) ? (v as ErrorCode) : null;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/** Normalizes any failed HTTP response (JSON envelope, plain text, empty) into an ApiError. */
export function errorFromResponse(response: Response, body: unknown): ApiError {
  const headerId = response.headers.get("X-Request-ID") ?? "";
  const envelope = isRecord(body) && isRecord(body.error) ? body.error : null;
  const code = envelope ? asErrorCode(envelope.code) : null;
  if (envelope && code) {
    return new ApiError({
      status: response.status,
      code,
      title: str(envelope.title) || `Request failed (HTTP ${response.status})`,
      message: str(envelope.message) || `The server answered with HTTP ${response.status}.`,
      recovery: str(envelope.recovery),
      requestId: str(envelope.request_id) || headerId,
      details: isRecord(envelope.details) ? envelope.details : undefined,
    });
  }
  return new ApiError({
    status: response.status,
    code: "INTERNAL",
    title: `Unexpected server response (HTTP ${response.status})`,
    message: `The server answered with HTTP ${response.status} and no readable error details.`,
    recovery: "Try again. If the problem persists, give the request ID to your administrator.",
    requestId: headerId,
  });
}

function networkError(): ApiError {
  return new ApiError({
    status: 0,
    code: "INTERNAL",
    title: "Server unreachable",
    message: "The server could not be reached. Check your connection and try again.",
    recovery: "Check that the server is running and your network is up, then retry.",
  });
}

function timeoutError(): ApiError {
  return new ApiError({
    status: 0,
    code: "INTERNAL",
    title: "Request timed out",
    message: "The server did not answer in time. The change may or may not have been saved.",
    recovery: "Use Retry safely to resend the same request; it will not be applied twice.",
  });
}

function canceledError(): ApiError {
  return new ApiError({
    status: 0,
    code: "INTERNAL",
    title: "Request canceled",
    message: "The request was canceled. The change may or may not have been saved.",
    canceled: true,
  });
}

export type CallInit = {
  signal: AbortSignal;
  headers: Record<string, string>;
};

export interface RunResult<T> {
  data?: T;
  error?: unknown;
  response: Response;
}

export type Runner<T> = (client: Client<paths>, init: CallInit) => Promise<RunResult<T>>;

export interface RequestOptions {
  class?: RequestClass;
  /** Caller-supplied cancellation, merged with the timeout and the activity's own controller. */
  signal?: AbortSignal;
  /** Overrides the class default (15 s interactive, 60 s background). */
  timeoutMs?: number;
  /** Text shown in the activity store. */
  label?: string;
  /** Reused by retry(); generated when omitted. */
  idempotencyKey?: string;
}

export interface ApiClientConfig {
  baseUrl?: string;
  fetch?: (request: Request) => Promise<Response>;
}

export class ApiClient {
  readonly raw: Client<paths>;

  constructor(config: ApiClientConfig = {}) {
    const middleware: Middleware = {
      onRequest({ request }) {
        if (UNSAFE_METHODS.has(request.method.toUpperCase())) {
          const csrf = useSessionStore.getState().csrf;
          if (csrf) request.headers.set("X-CSRF-Token", csrf);
        } else {
          request.headers.delete("Idempotency-Key");
        }
        return request;
      },
    };
    this.raw = createClient<paths>({
      baseUrl: config.baseUrl ?? (typeof location === "undefined" ? "http://localhost" : location.origin),
      fetch: (request) => (config.fetch ?? globalThis.fetch)(request),
    });
    this.raw.use(middleware);
  }

  /**
   * Runs one logical API call. `run` receives the typed client plus the init
   * (signal + Idempotency-Key header) that it must spread into the call.
   * Resolves to the response data or rejects with an ApiError carrying retry().
   */
  async request<T>(run: Runner<T>, options: RequestOptions = {}): Promise<T | undefined> {
    const key = options.idempotencyKey ?? crypto.randomUUID();
    try {
      return await this.attempt(run, options, key);
    } catch (e) {
      if (e instanceof ApiError && !e.canceled) {
        e.retry = () => this.request(run, { ...options, idempotencyKey: key });
      }
      throw e;
    }
  }

  /** Like request(), but a missing body is an error (for endpoints that return JSON). */
  async requestData<T>(run: Runner<T>, options: RequestOptions = {}): Promise<T> {
    const data = await this.request(run, options);
    if (data === undefined) {
      throw new ApiError({
        status: 200,
        code: "INTERNAL",
        title: "Empty server response",
        message: "The server answered successfully but sent no data.",
      });
    }
    return data;
  }

  private async attempt<T>(run: Runner<T>, options: RequestOptions, key: string): Promise<T | undefined> {
    const cls: RequestClass = options.class ?? "interactive";
    const timeoutMs =
      options.timeoutMs ?? (cls === "interactive" ? INTERACTIVE_TIMEOUT_MS : BACKGROUND_TIMEOUT_MS);
    const own = new AbortController();
    const timeoutSignal = AbortSignal.timeout(timeoutMs);
    const parts = options.signal ? [options.signal, own.signal, timeoutSignal] : [own.signal, timeoutSignal];
    const signal = AbortSignal.any(parts);

    const id = crypto.randomUUID();
    let slowTimer: ReturnType<typeof setTimeout> | undefined;
    if (cls === "interactive") {
      useActivityStore
        .getState()
        .add({ id, label: options.label ?? "Working", slow: false, cancel: () => own.abort() });
      slowTimer = setTimeout(() => useActivityStore.getState().markSlow(id), SLOW_AFTER_MS);
    }

    try {
      let result: RunResult<T>;
      try {
        result = await run(this.raw, { signal, headers: { "Idempotency-Key": key } });
      } catch (e) {
        if (timeoutSignal.aborted) throw timeoutError();
        if (signal.aborted) throw canceledError();
        if (e instanceof ApiError) throw e;
        throw networkError();
      }
      if (!result.response.ok) throw errorFromResponse(result.response, result.error);
      return result.data;
    } finally {
      if (slowTimer !== undefined) clearTimeout(slowTimer);
      if (cls === "interactive") useActivityStore.getState().remove(id);
    }
  }
}

export const api = new ApiClient();

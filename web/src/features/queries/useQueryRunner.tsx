import { useEffect, useRef, useState, type ReactNode } from "react";
import { ApiError } from "@/lib/api";
import { ParameterDialog } from "./ParameterDialog";
import type { Param } from "./model";
import { recallParams, rememberParams, type ParamRaw } from "./params";
import type { Params, QueryResult } from "./savedQueries";

export interface Runner {
  result: QueryResult | null;
  error: string | null;
  running: boolean;
  /** Runs, prompting first when the query has parameters. */
  start: () => void;
  /** Re-runs with the last values (prompts when none were given yet). */
  refresh: () => void;
  /** Opens the prompt again to enter different values. */
  changeParams: () => void;
  hasParams: boolean;
  clear: () => void;
  prompt: ReactNode;
}

export function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return "Something went wrong while contacting the server.";
}

function missingNames(e: unknown): string[] | null {
  if (!(e instanceof ApiError)) return null;
  const m = e.details?.missing_parameters;
  return Array.isArray(m) ? m.filter((x): x is string => typeof x === "string") : null;
}

/**
 * Runs a query and handles the parameter prompt: declared parameters are asked
 * for first; a server `missing_parameters` answer asks for those.
 */
export function useQueryRunner(opts: {
  memoryKey: string;
  params: readonly Param[];
  exec: (values: Params) => Promise<QueryResult>;
}): Runner {
  const [result, setResult] = useState<QueryResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [asking, setAsking] = useState<Param[] | null>(null);
  const last = useRef<Params | null>(null);
  const execRef = useRef(opts.exec);
  const paramsRef = useRef(opts.params);
  useEffect(() => {
    execRef.current = opts.exec;
    paramsRef.current = opts.params;
  });

  async function execute(values: Params): Promise<void> {
    last.current = values;
    setRunning(true);
    setError(null);
    try {
      setResult(await execRef.current(values));
    } catch (e) {
      const missing = missingNames(e);
      if (missing && missing.length > 0) {
        setAsking(missing.map((name) => paramsRef.current.find((p) => p.name === name) ?? { name, type: "text" as const }));
      } else if (!(e instanceof ApiError && e.canceled)) {
        setError(errorText(e));
      }
    } finally {
      setRunning(false);
    }
  }

  function start(): void {
    if (paramsRef.current.length > 0) setAsking([...paramsRef.current]);
    else void execute({});
  }

  function submit(values: Params, raw: Record<string, ParamRaw>): void {
    rememberParams(opts.memoryKey, raw);
    setAsking(null);
    void execute({ ...last.current, ...values });
  }

  return {
    result,
    error,
    running,
    start,
    refresh: () => (last.current ? void execute(last.current) : start()),
    changeParams: start,
    hasParams: opts.params.length > 0,
    clear: () => {
      setResult(null);
      setError(null);
    },
    prompt: asking ? (
      <ParameterDialog params={asking} initial={recallParams(opts.memoryKey)} onSubmit={submit} onCancel={() => setAsking(null)} />
    ) : null,
  };
}

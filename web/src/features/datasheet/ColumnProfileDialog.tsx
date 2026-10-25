import { useEffect, useState } from "react";
import { ChartColumn } from "lucide-react";
import type { Column } from "@/lib/schema";
import { Button, Dialog } from "@/components/ui";
import { alertBox } from "@/components/styles";
import { fetchRows, type Row } from "./rowsApi";
import { PROFILE_ROW_LIMIT, createProfiler, formatScale, percent, topLabel, type Profile } from "./profile";
import { kindOf } from "./values";

const PAGE = 500;

interface Props {
  ws: string;
  tableId: string;
  column: Column;
  /** The datasheet's current filter, as the API takes it: the profile covers the records in view. */
  filter: string | undefined;
  onClose: () => void;
}

type State =
  | { phase: "loading"; read: number }
  | { phase: "done"; profile: Profile; truncated: boolean }
  | { phase: "failed"; message: string };

function valueOf(col: Column, row: Row): unknown {
  return col.field_type === "autonumber" ? row.id : row.fields[col.id];
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border bg-surface-2/50 px-3 py-2">
      <dt className="text-xs text-fg-muted">{label}</dt>
      <dd className="m-0 mt-0.5 text-sm font-semibold tabular-nums text-fg">{value}</dd>
    </div>
  );
}

/**
 * Statistics for one column over the records the datasheet shows: completeness, distinct values,
 * quartiles for numbers and dates, and the five most frequent values. The records are read page
 * by page (up to 20,000), so the screen stays responsive and the dialog can be closed at any time.
 */
export function ColumnProfileDialog({ ws, tableId, column, filter, onClose }: Props) {
  const [state, setState] = useState<State>({ phase: "loading", read: 0 });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const ctl = new AbortController();
    const profiler = createProfiler(column);
    void (async () => {
      try {
        let offset = 0;
        let truncated = false;
        for (;;) {
          const page = await fetchRows({ ws, tableId, offset, limit: PAGE, filter }, ctl.signal);
          const items = page.items ?? [];
          for (const row of items) profiler.add(valueOf(column, row), row.display?.[column.id]);
          offset += items.length;
          if (items.length < PAGE) break;
          if (offset >= PROFILE_ROW_LIMIT) {
            truncated = true;
            break;
          }
          setState({ phase: "loading", read: offset });
        }
        setState({ phase: "done", profile: profiler.result(), truncated });
      } catch (e) {
        if (ctl.signal.aborted) return;
        setState({ phase: "failed", message: e instanceof Error ? e.message : "The records could not be read." });
      }
    })();
    return () => ctl.abort();
  }, [ws, tableId, column, filter, attempt]);

  const kind = kindOf(column);
  const typeName = column.field_type.replace(/_/g, " ");

  return (
    <Dialog
      onClose={onClose}
      width="lg"
      icon={<ChartColumn size={18} aria-hidden="true" className="mt-1.5 text-accent" />}
      title={`Column profile: ${column.display_name} (${typeName})`}
      description={filter ? "Statistics for the records that match the current filter." : "Statistics for every record in the table."}
      footer={
        <Button variant="secondary" onClick={onClose}>
          Close
        </Button>
      }
    >
      {state.phase === "loading" && (
        <div role="status" className="space-y-2">
          <div className="h-2 w-full animate-shimmer rounded bg-surface-3" />
          <p className="m-0 text-sm text-fg-muted">Reading records… {state.read.toLocaleString("en-US")} so far</p>
        </div>
      )}
      {state.phase === "failed" && (
        <div role="alert" className={alertBox}>
          <p className="m-0 font-medium">The column could not be profiled</p>
          <p className="m-0">{state.message}</p>
          <Button variant="secondary" size="sm" className="mt-2" onClick={() => { setState({ phase: "loading", read: 0 }); setAttempt((a) => a + 1); }}>
            Try again
          </Button>
        </div>
      )}
      {state.phase === "done" && <ProfileBody column={column} kind={kind} profile={state.profile} truncated={state.truncated} />}
    </Dialog>
  );
}

function ProfileBody({ column, kind, profile, truncated }: { column: Column; kind: string; profile: Profile; truncated: boolean }) {
  const n = profile.numeric;
  const max = profile.top[0]?.count ?? 1;
  return (
    <>
      {truncated && (
        <p className="m-0 text-xs text-fg-muted">Based on the first {profile.total.toLocaleString("en-US")} records. Add a filter to profile a narrower set.</p>
      )}
      <dl className="m-0 grid grid-cols-2 gap-2 sm:grid-cols-3">
        <Stat label="Total rows" value={profile.total.toLocaleString("en-US")} />
        <Stat label="Non-empty" value={`${profile.nonNull.toLocaleString("en-US")} (${percent(profile.nonNull, profile.total)})`} />
        <Stat label="Empty" value={`${profile.nulls.toLocaleString("en-US")} (${percent(profile.nulls, profile.total)})`} />
        <Stat label="Distinct values" value={`${profile.distinct.toLocaleString("en-US")} (${percent(profile.distinct, profile.nonNull)} unique)`} />
        {n && (
          <>
            <Stat label="Minimum" value={formatScale(column, n.min)} />
            <Stat label="Maximum" value={formatScale(column, n.max)} />
            <Stat label={kind === "date" || kind === "datetime" ? "Average" : "Mean"} value={formatScale(column, n.mean)} />
            <Stat label="Median" value={formatScale(column, n.median)} />
            <Stat label="25th percentile" value={formatScale(column, n.q1)} />
            <Stat label="75th percentile" value={formatScale(column, n.q3)} />
          </>
        )}
      </dl>
      <section aria-label="Frequent values">
        <h3 className="m-0 mb-2 text-sm font-semibold text-fg">Frequent values</h3>
        {profile.top.length === 0 ? (
          <p className="m-0 text-sm text-fg-muted">There are no values in this column.</p>
        ) : (
          <ol className="m-0 list-none space-y-1.5 p-0">
            {profile.top.map((t) => (
              <li key={t.label} className="grid grid-cols-[minmax(0,9rem)_1fr_auto] items-center gap-2 text-sm">
                <span className="truncate">
                  {topLabel(column, t.label)}
                </span>
                <span aria-hidden="true" className="h-3 rounded bg-surface-3">
                  <span className="block h-3 rounded bg-accent" style={{ width: `${Math.max(2, (t.count / max) * 100)}%` }} />
                </span>
                <span className="tabular-nums text-fg-muted">
                  {t.count.toLocaleString("en-US")} {t.count === 1 ? "record" : "records"} ({percent(t.count, profile.total)})
                </span>
              </li>
            ))}
          </ol>
        )}
      </section>
    </>
  );
}

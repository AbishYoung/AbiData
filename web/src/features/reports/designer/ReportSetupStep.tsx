import { KindIcon } from "@/components/objectKinds";
import { useId, useState } from "react";
import { btnPrimary, input, label as labelClass } from "@/components/styles";
import { useSchema } from "@/lib/schema";
import { columnsOfSource } from "@/features/forms/designer/sourceColumns";
import { useSavedQueries } from "@/features/queries/savedQueries";
import { draftKey, useReportDrafts } from "./reportDraftStore";
import {
  PAPER_LABEL,
  newState,
  applyStarter,
  STARTER_LABEL,
  type StarterLayout,
  type Orientation,
  type Paper,
  type SourceKind,
} from "./reportModel";

// The first step of a new report: name, source and a starter layout.
// Split from ReportDesigner.tsx (ADR 0015, O4).

export function SetupStep({ ws, tabKey }: { ws: string; tabKey: string }) {
  const nameId = useId();
  const kindId = useId();
  const sourceId = useId();
  const paperId = useId();
  const orientId = useId();
  const marginId = useId();
  const layoutId = useId();
  const schema = useSchema(ws);
  const queries = useSavedQueries(ws);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<SourceKind>("table");
  const [source, setSource] = useState("");
  const [paper, setPaper] = useState<Paper>("letter");
  const [orientation, setOrientation] = useState<Orientation>("portrait");
  const [margin, setMargin] = useState("48");
  const [starter, setStarter] = useState<StarterLayout>("blank");
  const options =
    kind === "table"
      ? (schema.data?.tables ?? []).map((t) => ({ id: t.id, name: t.display_name }))
      : (queries.data ?? []).map((q) => ({ id: q.id, name: q.name }));
  const marginN = Number(margin);
  const marginOk = margin.trim() !== "" && Number.isInteger(marginN) && marginN >= 0 && marginN <= 300;
  const valid = name.trim() !== "" && source !== "" && marginOk;

  function start(e: React.FormEvent): void {
    e.preventDefault();
    if (!valid) return;
    useReportDrafts.getState().put(draftKey(ws, tabKey), {
      state: applyStarter(
        newState({ name, sourceKind: kind, sourceId: source, paper, orientation, margin: marginN }),
        starter,
        columnsOfSource(schema.data, queries.data, kind, source),
      ),
      base: "",
      saved: null,
      panel: "layout",
    });
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto bg-bg p-6" data-testid="report-setup">
      <form onSubmit={start} className="mx-auto max-w-md rounded-xl border border-border bg-surface p-5 shadow-sm" aria-label="New report setup">
        <div className="mb-4 flex items-center gap-2.5">
          <KindIcon kind="report" size="lg" />
          <h2 className="m-0 text-base font-semibold tracking-tight">New report</h2>
        </div>
        <label className={labelClass} htmlFor={nameId}>
          Report name
        </label>
        <input id={nameId} className={`${input} mb-3`} value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
        <label className={labelClass} htmlFor={kindId}>
          Record source type
        </label>
        <select
          id={kindId}
          className={`${input} mb-3`}
          value={kind}
          onChange={(e) => {
            setKind(e.target.value as SourceKind);
            setSource("");
          }}
        >
          <option value="table">Table</option>
          <option value="query">Saved query</option>
        </select>
        <label className={labelClass} htmlFor={sourceId}>
          Record source
        </label>
        <select id={sourceId} className={`${input} mb-3`} value={source} onChange={(e) => setSource(e.target.value)}>
          <option value="">Choose a {kind === "table" ? "table" : "saved query"}</option>
          {options.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name}
            </option>
          ))}
        </select>
        <label className={labelClass} htmlFor={layoutId}>
          Layout
        </label>
        <select
          id={layoutId}
          className={`${input} mb-1`}
          value={starter}
          onChange={(e) => setStarter(e.target.value as StarterLayout)}
          aria-describedby={`${layoutId}-hint`}
        >
          {(Object.keys(STARTER_LABEL) as StarterLayout[]).map((l) => (
            <option key={l} value={l}>
              {STARTER_LABEL[l]}
            </option>
          ))}
        </select>
        <p id={`${layoutId}-hint`} className="m-0 mb-3 text-xs text-fg-muted">
          {starter === "blank"
            ? "Start with empty bands and add fields yourself."
            : starter === "tabular"
              ? "Every field of the source, with headings across the top of each page and one row per record."
              : "Every field of the source, one block per record, with each field's name beside its value."}
        </p>
        <label className={labelClass} htmlFor={paperId}>
          Paper
        </label>
        <select id={paperId} className={`${input} mb-3`} value={paper} onChange={(e) => setPaper(e.target.value as Paper)}>
          {(Object.keys(PAPER_LABEL) as Paper[]).map((p) => (
            <option key={p} value={p}>
              {PAPER_LABEL[p]}
            </option>
          ))}
        </select>
        <label className={labelClass} htmlFor={orientId}>
          Orientation
        </label>
        <select id={orientId} className={`${input} mb-3`} value={orientation} onChange={(e) => setOrientation(e.target.value as Orientation)}>
          <option value="portrait">Portrait</option>
          <option value="landscape">Landscape</option>
        </select>
        <label className={labelClass} htmlFor={marginId}>
          Margins (px)
        </label>
        <input
          id={marginId}
          type="number"
          min={0}
          max={300}
          className={`${input} mb-1`}
          value={margin}
          onChange={(e) => setMargin(e.target.value)}
          aria-invalid={marginOk ? undefined : true}
        />
        <p className="m-0 mb-4 text-xs text-fg-muted">The same on every side, from 0 to 300 px (96 px = 1 inch). You can set each side later.</p>
        <button type="submit" className={btnPrimary} disabled={!valid}>
          Start designing
        </button>
      </form>
    </div>
  );
}

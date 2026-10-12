import { useId, useState } from "react";
import { Map as MapIcon } from "lucide-react";
import { AreaEditor, MapDialog } from "@/components/maps/GeoEditors";
import { formatGeoValue } from "@/lib/geo/format";
import { areaOf, type AreaGeometry } from "@/lib/geo/geometry";
import { btnSm } from "@/components/styles";
import { input } from "@/components/styles";
import { makeCriterion, opInfo, operatorsFor, kindForOp, valueTexts, type CriterionOp } from "./criteria";
import { selectClass } from "./Modal";
import type { Criterion, Param, ValueKind } from "./model";

function htmlType(kind: ValueKind): string {
  switch (kind) {
    case "date":
      return "date";
    case "datetime":
      return "datetime-local";
    default:
      return "text";
  }
}

function ValueInput({
  kind,
  label,
  value,
  onChange,
}: {
  kind: ValueKind;
  label: string;
  value: string;
  onChange: (v: string) => void;
}) {
  if (kind === "yes_no") {
    return (
      <select aria-label={label} className={selectClass} value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">Choose…</option>
        <option value="true">Yes</option>
        <option value="false">No</option>
      </select>
    );
  }
  return (
    <input
      aria-label={label}
      type={htmlType(kind)}
      inputMode={kind === "integer" ? "numeric" : kind === "number" || kind === "currency" ? "decimal" : undefined}
      className={input}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

/**
 * One cell of the criteria grid: an operator, then a typed value, or a declared
 * parameter to ask for when the query runs.
 */
type CellProps = {
  /** Names the cell for assistive technology, e.g. "Orders.Qty criteria". */
  label: string;
  row: number;
  kind: ValueKind;
  criterion: Criterion | undefined;
  params: readonly Param[];
  /** Area columns of other sources that an "is within" criterion may compare with. */
  areaRefs?: readonly { alias: string; columnId: string; label: string }[];
  onChange: (c: Criterion | null) => void;
};

export function CriteriaCell(props: CellProps) {
  const { criterion, kind } = props;
  if (criterion?.op === "within" || kind === "geo_point" || kind === "geo_area") {
    return <WithinCell label={props.label} row={props.row} criterion={criterion} params={props.params} areaRefs={kind === "geo_area" ? null : (props.areaRefs ?? [])} onChange={props.onChange} />;
  }
  return <ScalarCriteriaCell {...props} />;
}

function ScalarCriteriaCell({ label, row, kind, criterion, params, onChange }: CellProps) {
  const sourceId = useId();
  const op = criterion?.op;
  const arity = op ? opInfo(op).arity : 0;
  const usesParam = criterion?.param !== undefined;
  const effKind = op ? kindForOp(kind, op) : kind;
  // Text typed so far is kept locally: a half-typed number is not a valid value yet.
  const [texts, setTexts] = useState<string[]>(() => (criterion && op ? valueTexts(kind, op, criterion.value) : []));

  function chooseOp(value: string): void {
    if (value === "") {
      setTexts([]);
      onChange(null);
      return;
    }
    const next = value as CriterionOp;
    const keep = op !== undefined && opInfo(op).arity === opInfo(next).arity ? texts : [];
    setTexts(keep);
    onChange(makeCriterion(row, next, kind, keep, usesParam && opInfo(next).arity !== 0 ? (criterion?.param ?? null) : null));
  }

  function setText(i: number, v: string): void {
    if (!op) return;
    const next = [...texts];
    next[i] = v;
    setTexts(next);
    onChange(makeCriterion(row, op, kind, next, null));
  }

  function chooseSource(v: string): void {
    if (!op) return;
    if (v === "") onChange(makeCriterion(row, op, kind, texts, null));
    else onChange({ row, op, param: v });
  }

  const ops = operatorsFor(kind);
  const list = op && !ops.some((o) => o.op === op) ? [...ops, opInfo(op)] : ops;

  return (
    <div className="flex min-w-44 flex-col gap-1">
      <select aria-label={`${label} operator`} className={selectClass} value={op ?? ""} onChange={(e) => chooseOp(e.target.value)}>
        <option value="">(none)</option>
        {list.map((o) => (
          <option key={o.op} value={o.op}>
            {o.label}
          </option>
        ))}
      </select>
      {op && arity !== 0 && params.length > 0 && (
        <select
          id={sourceId}
          aria-label={`${label} compare to`}
          className={selectClass}
          value={criterion?.param ?? ""}
          onChange={(e) => chooseSource(e.target.value)}
        >
          <option value="">a value</option>
          {params.map((p) => (
            <option key={p.name} value={p.name}>
              parameter :{p.name}
            </option>
          ))}
        </select>
      )}
      {op && arity !== 0 && !usesParam && arity === 2 && (
        <div className="flex gap-1">
          <ValueInput kind={effKind} label={`${label} from`} value={texts[0] ?? ""} onChange={(v) => setText(0, v)} />
          <ValueInput kind={effKind} label={`${label} to`} value={texts[1] ?? ""} onChange={(v) => setText(1, v)} />
        </div>
      )}
      {op && arity === "list" && !usesParam && (
        <input
          aria-label={`${label} values, separated by commas`}
          placeholder="a, b, c"
          className={input}
          value={texts[0] ?? ""}
          onChange={(e) => setText(0, e.target.value)}
        />
      )}
      {op && arity === 1 && !usesParam && (
        <ValueInput kind={effKind} label={`${label} value`} value={texts[0] ?? ""} onChange={(v) => setText(0, v)} />
      )}
    </div>
  );
}

/**
 * Criteria for a location field: "is within" an area drawn on a map, an area parameter, or an
 * area field of a joined source; or is empty / not empty.
 */
function WithinCell({
  label,
  row,
  criterion,
  params,
  areaRefs,
  onChange,
}: {
  label: string;
  row: number;
  criterion: Criterion | undefined;
  params: readonly Param[];
  /** Null for an area field: only "is empty" / "is not empty" apply. */
  areaRefs: readonly { alias: string; columnId: string; label: string }[] | null;
  onChange: (c: Criterion | null) => void;
}) {
  const [drawing, setDrawing] = useState(false);
  const [draft, setDraft] = useState<AreaGeometry | null>(null);
  const op = criterion?.op;
  const areaParams = params.filter((p) => p.type === "area");
  const source = criterion?.param ? `p:${criterion.param}` : criterion?.ref_alias ? `r:${criterion.ref_alias}:${criterion.ref_column_id ?? ""}` : "value";
  const drawn = areaOf(criterion?.value);

  function chooseOp(v: string): void {
    if (v === "") onChange(null);
    else if (v === "within") onChange({ row, op: "within" });
    else onChange({ row, op: v as Criterion["op"] });
  }

  function chooseSource(v: string): void {
    if (v === "value") onChange(drawn ? { row, op: "within", value: drawn } : { row, op: "within" });
    else if (v.startsWith("p:")) onChange({ row, op: "within", param: v.slice(2) });
    else {
      const [, alias, col] = v.split(":");
      onChange({ row, op: "within", ref_alias: alias ?? "", ref_column_id: col ?? "" });
    }
  }

  return (
    <div className="flex min-w-44 flex-col gap-1">
      <select aria-label={`${label} operator`} className={selectClass} value={op ?? ""} onChange={(e) => chooseOp(e.target.value)}>
        <option value="">(none)</option>
        {areaRefs !== null && <option value="within">is within</option>}
        <option value="is_null">is empty</option>
        <option value="not_null">is not empty</option>
      </select>
      {op === "within" && (
        <select aria-label={`${label} area`} className={selectClass} value={source} onChange={(e) => chooseSource(e.target.value)}>
          <option value="value">an area drawn on a map</option>
          {areaParams.map((p) => (
            <option key={p.name} value={`p:${p.name}`}>
              parameter :{p.name}
            </option>
          ))}
          {(areaRefs ?? []).map((r) => (
            <option key={`${r.alias}.${r.columnId}`} value={`r:${r.alias}:${r.columnId}`}>
              area field {r.label}
            </option>
          ))}
        </select>
      )}
      {op === "within" && source === "value" && (
        <button
          type="button"
          className={`${btnSm} justify-start`}
          onClick={() => {
            setDraft(drawn);
            setDrawing(true);
          }}
        >
          <MapIcon size={12} aria-hidden="true" />
          <span className="truncate">{drawn ? formatGeoValue(drawn) : "Draw area…"}</span>
        </button>
      )}
      {drawing && (
        <MapDialog
          title={`${label}: is within`}
          description="Draw the area that matching locations must lie inside."
          onCancel={() => setDrawing(false)}
          submitLabel="Use this area"
          onSubmit={() => {
            setDrawing(false);
            onChange(draft ? { row, op: "within", value: draft } : { row, op: "within" });
          }}
        >
          <AreaEditor fieldName="Criteria area" value={drawn} onChange={(a) => setDraft(a)} />
        </MapDialog>
      )}
    </div>
  );
}

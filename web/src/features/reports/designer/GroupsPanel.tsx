import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { CheckField, SelectField, TextField } from "@/components/designer/fields";
import { Tip } from "@/components/Tip";
import { btn } from "@/components/styles";
import {
  MAX_GROUPS,
  newGroup,
  addGroup,
  moveGroup,
  moveItem,
  removeGroup,
  updateGroup,
  type ReportState,
  type SourceColumn,
} from "./reportModel";
import { NumField } from "./ReportProperties";

// The report's groups and sort order (ReportProperties.tsx shows it for the report).
// Split from ReportProperties.tsx (ADR 0015, O4).

const SMALL_BTN = `${btn} !px-2 !py-1`;

export function RowButtons({ label, index, count, onMove, onRemove }: { label: string; index: number; count: number; onMove: (dir: 1 | -1) => void; onRemove: () => void }) {
  return (
    <div className="mt-1 flex gap-1">
      <Tip label={`Move ${label} up`}>
        <span className="inline-flex">
          <button type="button" className={SMALL_BTN} aria-label={`Move ${label} up`} disabled={index === 0} onClick={() => onMove(-1)}>
            <ArrowUp size={14} aria-hidden="true" />
          </button>
        </span>
      </Tip>
      <Tip label={`Move ${label} down`}>
        <span className="inline-flex">
          <button type="button" className={SMALL_BTN} aria-label={`Move ${label} down`} disabled={index >= count - 1} onClick={() => onMove(1)}>
            <ArrowDown size={14} aria-hidden="true" />
          </button>
        </span>
      </Tip>
      <Tip label={`Remove ${label}`}>
        <button type="button" className={SMALL_BTN} aria-label={`Remove ${label}`} onClick={onRemove}>
          <Trash2 size={14} aria-hidden="true" />
        </button>
      </Tip>
    </div>
  );
}

export const EXPRESSION = "__expr__";

/** Grouping levels (outermost first) and the sort order of detail rows. */
export function GroupsPanel({ state, columns, onChange }: { state: ReportState; columns: SourceColumn[]; onChange: (s: ReportState) => void }) {
  const used = new Set(state.groups.map((g) => g.column_id));
  const firstFree = columns.find((c) => !used.has(c.id));
  const fieldOptions = columns.map((c) => (
    <option key={c.id} value={c.id}>
      {c.name}
    </option>
  ));
  return (
    <section aria-label="Groups and sorting" className="mb-3">
      <h3 className="m-0 mb-1 text-sm font-semibold">Groups and sorting</h3>
      <p className="m-0 mb-2 text-xs text-fg-muted">
        Each group adds a header and a footer band. The first group is the outermost; its footer is the last group footer before the page footer.
      </p>
      {state.groups.length === 0 ? (
        <p className="m-0 mb-2 text-sm text-fg-muted">No groups.</p>
      ) : (
        <ol className="m-0 mb-2 list-none p-0">
          {state.groups.map((g, i) => {
            const n = i + 1;
            const byExpr = g.column_id === "" && g.lua_expr !== "";
            const value = byExpr ? EXPRESSION : g.column_id;
            return (
              <li key={i} className="mb-2 rounded-md border border-border p-2" aria-label={`Group ${n}`}>
                <SelectField
                  label={`Group ${n} field`}
                  value={value}
                  onChange={(e) =>
                    onChange(
                      e.target.value === EXPRESSION
                        ? updateGroup(state, i, { column_id: "", lua_expr: g.lua_expr || " " })
                        : updateGroup(state, i, { column_id: e.target.value, lua_expr: "" }),
                    )
                  }
                >
                  <option value="">Choose a field</option>
                  {fieldOptions}
                  <option value={EXPRESSION}>Expression (Lua)</option>
                </SelectField>
                {byExpr && (
                  <TextField label={`Group ${n} expression`} autoComplete="off" value={g.lua_expr} onChange={(e) => onChange(updateGroup(state, i, { lua_expr: e.target.value }))} />
                )}
                <SelectField label={`Group ${n} order`} value={g.sort} onChange={(e) => onChange(updateGroup(state, i, { sort: e.target.value === "desc" ? "desc" : "asc" }))}>
                  <option value="asc">Ascending</option>
                  <option value="desc">Descending</option>
                </SelectField>
                <NumField
                  label={`Group ${n} interval`}
                  value={g.interval}
                  min={0}
                  max={1_000_000}
                  hint="0 groups on the whole value. Text: the first N characters. Numbers: multiples of N."
                  onCommit={(v) => onChange(updateGroup(state, i, { interval: v }))}
                />
                <CheckField
                  label={`Group ${n} repeat header on each page`}
                  hint="When the group runs onto another page, its header is shown again at the top with a continued note."
                  checked={g.repeat_header === true}
                  onChange={(e) => onChange(updateGroup(state, i, { repeat_header: e.target.checked }))}
                />
                {g.repeat_header === true && (
                  <TextField
                    label={`Group ${n} continued text`}
                    autoComplete="off"
                    maxLength={32}
                    placeholder=" (continued)"
                    value={g.continued_suffix ?? ""}
                    hint="Added after the repeated header's first text. Empty uses (continued)."
                    onChange={(e) => onChange(updateGroup(state, i, { continued_suffix: e.target.value }))}
                  />
                )}
                <SelectField
                  label={`Group ${n} page breaks`}
                  value={g.keep_together ?? "first_detail"}
                  onChange={(e) => onChange(updateGroup(state, i, { keep_together: e.target.value as NonNullable<typeof g.keep_together> }))}
                >
                  <option value="first_detail">Keep the header with its first row</option>
                  <option value="all">Keep the whole group on one page when it fits</option>
                  <option value="none">Allow a break anywhere</option>
                </SelectField>
                <RowButtons
                  label={`group ${n}`}
                  index={i}
                  count={state.groups.length}
                  onMove={(dir) => onChange(moveGroup(state, i, dir))}
                  onRemove={() => onChange(removeGroup(state, i))}
                />
              </li>
            );
          })}
        </ol>
      )}
      <button
        type="button"
        className={btn}
        disabled={state.groups.length >= MAX_GROUPS}
        onClick={() => onChange(addGroup(state, newGroup(firstFree?.id ?? "")))}
      >
        <Plus size={14} aria-hidden="true" />
        Add group
      </button>

      <h4 className="m-0 mb-1 mt-3 text-sm font-semibold">Sort order</h4>
      {state.sorts.length === 0 ? (
        <p className="m-0 mb-2 text-sm text-fg-muted">No extra sorts; rows follow the group order, then the source order.</p>
      ) : (
        <ol className="m-0 mb-2 list-none p-0">
          {state.sorts.map((s, i) => {
            const n = i + 1;
            return (
              <li key={i} className="mb-2 rounded-md border border-border p-2" aria-label={`Sort ${n}`}>
                <SelectField
                  label={`Sort ${n} field`}
                  value={s.column_id}
                  onChange={(e) => onChange({ ...state, sorts: state.sorts.map((x, k) => (k === i ? { ...x, column_id: e.target.value } : x)) })}
                >
                  <option value="">Choose a field</option>
                  {fieldOptions}
                </SelectField>
                <CheckField
                  label={`Sort ${n} descending`}
                  checked={s.desc}
                  onChange={(e) => onChange({ ...state, sorts: state.sorts.map((x, k) => (k === i ? { ...x, desc: e.target.checked } : x)) })}
                />
                <RowButtons
                  label={`sort ${n}`}
                  index={i}
                  count={state.sorts.length}
                  onMove={(dir) => onChange({ ...state, sorts: moveItem(state.sorts, i, dir) })}
                  onRemove={() => onChange({ ...state, sorts: state.sorts.filter((_x, k) => k !== i) })}
                />
              </li>
            );
          })}
        </ol>
      )}
      <button
        type="button"
        className={btn}
        onClick={() => onChange({ ...state, sorts: [...state.sorts, { column_id: columns.find((c) => !state.sorts.some((s) => s.column_id === c.id))?.id ?? "", desc: false }] })}
      >
        <Plus size={14} aria-hidden="true" />
        Add sort
      </button>
    </section>
  );
}

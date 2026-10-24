import { useState } from "react";
import { ArrowDown, ArrowUp, Paintbrush, Plus, Trash2 } from "lucide-react";
import type { Column } from "@/lib/schema";
import { Button, Dialog, IconButton } from "@/components/ui";
import { input, select } from "@/components/styles";
import {
  OPERATOR_LABELS,
  TONES,
  TONE_CLASSES,
  TONE_LABELS,
  TONE_WORD,
  needsValue,
  operatorsFor,
  type FormattingRule,
  type FormattingTone,
} from "./conditionalFormatting";
import { kindOf } from "./values";

interface Props {
  columns: readonly Column[];
  rules: readonly FormattingRule[];
  onSave: (rules: FormattingRule[]) => void;
  onClose: () => void;
}

let counter = 0;
function newId(): string {
  counter += 1;
  return `r${Date.now().toString(36)}${counter}`;
}

function Preview({ tone, target }: { tone: FormattingTone; target: "cell" | "row" }) {
  return (
    <span className={`inline-flex h-7 items-center rounded px-2 text-sm ${TONE_CLASSES[tone]}`}>
      {TONE_WORD[tone]}
      <span className="sr-only">{target === "row" ? ", whole record" : ", cell"}</span>
    </span>
  );
}

/**
 * Builds the datasheet's formatting rules: a field, a condition, whether the cell or the whole
 * record is coloured and in which tone. Rules are checked in order and the first match wins, so
 * the order can be changed. Nothing is kept until Save.
 */
export function ConditionalFormattingModal({ columns, rules, onSave, onClose }: Props) {
  const [draft, setDraft] = useState<FormattingRule[]>(() => rules.map((r) => ({ ...r })));
  const byId = new Map(columns.map((c) => [c.id, c]));

  function change(id: string, patch: Partial<FormattingRule>) {
    setDraft((d) => d.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  }

  function changeColumn(rule: FormattingRule, columnId: string) {
    const col = byId.get(columnId);
    if (!col) return;
    const ops = operatorsFor(col);
    change(rule.id, { columnId, operator: ops.includes(rule.operator) ? rule.operator : (ops[0] ?? "eq"), value: undefined });
  }

  function add() {
    const col = columns[0];
    if (!col) return;
    setDraft((d) => [...d, { id: newId(), columnId: col.id, operator: operatorsFor(col)[0] ?? "eq", target: "cell", tone: "danger" }]);
  }

  function move(index: number, delta: number) {
    setDraft((d) => {
      const to = index + delta;
      if (to < 0 || to >= d.length) return d;
      const next = [...d];
      const [item] = next.splice(index, 1);
      if (item) next.splice(to, 0, item);
      return next;
    });
  }

  return (
    <Dialog
      onClose={onClose}
      width="xl"
      icon={<Paintbrush size={18} aria-hidden="true" className="mt-1.5 text-accent" />}
      title="Formatting rules"
      description="Colour cells or whole records when a field meets a condition. Rules are checked from the top and the first match wins. They are saved for you in this browser."
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => onSave(draft)}>
            Save rules
          </Button>
        </>
      }
    >
      {draft.length === 0 && <p className="m-0 text-sm text-fg-muted">There are no rules yet. Add one to highlight overdue dates, low stock or empty fields.</p>}
      <ol className="m-0 list-none space-y-2 p-0" aria-label="Formatting rules">
        {draft.map((r, i) => {
          const col = byId.get(r.columnId);
          const kind = col ? kindOf(col) : "text";
          return (
            <li key={r.id} className="rounded-lg border border-border bg-surface-2/40 p-2">
              <div className="flex flex-wrap items-center gap-2" role="group" aria-label={`Rule ${i + 1}`}>
                <span className="w-5 text-center text-xs font-semibold text-fg-muted">{i + 1}</span>
                <select className={`${select} w-40`} aria-label={`Rule ${i + 1} field`} value={r.columnId} onChange={(e) => changeColumn(r, e.target.value)}>
                  {!col && <option value={r.columnId}>(removed field)</option>}
                  {columns.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.display_name}
                    </option>
                  ))}
                </select>
                <select
                  className={`${select} w-40`}
                  aria-label={`Rule ${i + 1} condition`}
                  value={r.operator}
                  onChange={(e) => change(r.id, { operator: e.target.value as FormattingRule["operator"] })}
                >
                  {(col ? operatorsFor(col) : []).map((op) => (
                    <option key={op} value={op}>
                      {OPERATOR_LABELS[op]}
                    </option>
                  ))}
                </select>
                {needsValue(r.operator) &&
                  (kind === "boolean" ? (
                    <select className={`${select} w-24`} aria-label={`Rule ${i + 1} value`} value={String(r.value ?? "true")} onChange={(e) => change(r.id, { value: e.target.value })}>
                      <option value="true">Yes</option>
                      <option value="false">No</option>
                    </select>
                  ) : (
                    <input
                      className={`${input} w-36`}
                      aria-label={`Rule ${i + 1} value`}
                      type={kind === "date" ? "date" : kind === "datetime" ? "datetime-local" : "text"}
                      value={typeof r.value === "string" ? r.value : ""}
                      onChange={(e) => change(r.id, { value: e.target.value })}
                    />
                  ))}
                <select className={`${select} w-28`} aria-label={`Rule ${i + 1} applies to`} value={r.target} onChange={(e) => change(r.id, { target: e.target.value as "cell" | "row" })}>
                  <option value="cell">Cell</option>
                  <option value="row">Whole record</option>
                </select>
                <select className={`${select} w-36`} aria-label={`Rule ${i + 1} colour`} value={r.tone} onChange={(e) => change(r.id, { tone: e.target.value as FormattingTone })}>
                  {TONES.map((t) => (
                    <option key={t} value={t}>
                      {TONE_LABELS[t]}
                    </option>
                  ))}
                </select>
                <Preview tone={r.tone} target={r.target} />
                <span className="ml-auto flex items-center">
                  <IconButton label={`Move rule ${i + 1} up`} disabled={i === 0} onClick={() => move(i, -1)}>
                    <ArrowUp size={16} aria-hidden="true" />
                  </IconButton>
                  <IconButton label={`Move rule ${i + 1} down`} disabled={i === draft.length - 1} onClick={() => move(i, 1)}>
                    <ArrowDown size={16} aria-hidden="true" />
                  </IconButton>
                  <IconButton label={`Remove rule ${i + 1}`} onClick={() => setDraft((d) => d.filter((x) => x.id !== r.id))}>
                    <Trash2 size={16} aria-hidden="true" />
                  </IconButton>
                </span>
              </div>
            </li>
          );
        })}
      </ol>
      <Button variant="secondary" onClick={add} disabled={columns.length === 0}>
        <Plus size={14} aria-hidden="true" />
        Add rule
      </Button>
    </Dialog>
  );
}

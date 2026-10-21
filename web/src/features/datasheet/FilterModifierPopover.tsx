import { useId, useState, type ReactNode } from "react";
import { Popover } from "radix-ui";
import type { Column } from "@/lib/schema";
import { badge, btnGhostSm, btnPrimary, input, label, select } from "@/components/styles";
import { fieldTypeLabel } from "@/features/tabledesign/draft";
import type { Condition, FilterOp } from "./filter";
import { operatorLabel } from "./filter";
import { isNumericKind, kindOf, type ValueKind } from "./values";

export function operatorsForKind(kind: ValueKind): { op: FilterOp; label: string }[] {
  switch (kind) {
    case "integer":
    case "number":
    case "currency":
    case "autonumber":
      return [
        { op: "eq", label: "= (equals)" },
        { op: "ne", label: "≠ (does not equal)" },
        { op: "gt", label: "> (greater than)" },
        { op: "ge", label: "≥ (greater than or equal)" },
        { op: "lt", label: "< (less than)" },
        { op: "le", label: "≤ (less than or equal)" },
        { op: "is_null", label: "is empty" },
        { op: "not_null", label: "is not empty" },
      ];
    case "date":
    case "datetime":
      return [
        { op: "eq", label: "equals" },
        { op: "ne", label: "does not equal" },
        { op: "gt", label: "after (>)" },
        { op: "ge", label: "on or after (≥)" },
        { op: "lt", label: "before (<)" },
        { op: "le", label: "on or before (≤)" },
        { op: "is_null", label: "is empty" },
        { op: "not_null", label: "is not empty" },
      ];
    case "boolean":
      return [
        { op: "eq", label: "is" },
        { op: "is_null", label: "is empty" },
      ];
    case "attachments":
      return [
        { op: "not_null", label: "has files" },
        { op: "is_null", label: "is empty" },
      ];
    case "geo_point":
      return [
        { op: "near", label: "near location" },
        { op: "within", label: "within area" },
        { op: "is_null", label: "is empty" },
        { op: "not_null", label: "is not empty" },
      ];
    case "geo_area":
      return [
        { op: "is_null", label: "is empty" },
        { op: "not_null", label: "is not empty" },
      ];
    case "text":
    case "longtext":
    case "hyperlink":
    case "lookup":
    default:
      return [
        { op: "contains", label: "contains" },
        { op: "eq", label: "equals" },
        { op: "ne", label: "does not equal" },
        { op: "starts", label: "starts with" },
        { op: "ends", label: "ends with" },
        { op: "is_null", label: "is empty" },
        { op: "not_null", label: "is not empty" },
      ];
  }
}

interface FilterModifierFormProps {
  condition: Condition;
  column?: Column;
  onApply: (operator: FilterOp, value: unknown) => void;
  onRemove: () => void;
}

function FilterModifierForm({
  condition,
  column,
  onApply,
  onRemove,
}: FilterModifierFormProps) {
  const [op, setOp] = useState<FilterOp>(condition.op);
  const [val, setVal] = useState<string>(() => {
    if (condition.value === undefined || condition.value === null) return "";
    return typeof condition.value === "object" ? JSON.stringify(condition.value) : String(condition.value);
  });

  const opId = useId();
  const valId = useId();

  const kind: ValueKind = column ? kindOf(column) : "text";
  const colName = column?.display_name ?? condition.col;
  const typeLabel = column ? fieldTypeLabel(column.field_type) : "Field";

  const availableOps = operatorsForKind(kind);
  if (!availableOps.some((o) => o.op === condition.op)) {
    availableOps.unshift({ op: condition.op, label: operatorLabel(condition.op) });
  }

  const isNullOrNotNull = op === "is_null" || op === "not_null";
  const isBoolean = kind === "boolean";
  const isNumeric = isNumericKind(kind);

  function handleApply() {
    let finalValue: unknown = val;
    if (isNullOrNotNull) {
      finalValue = undefined;
    } else if (isBoolean) {
      finalValue = val === "true";
    } else if (kind === "integer") {
      const parsed = Number.parseInt(val.trim(), 10);
      finalValue = Number.isNaN(parsed) ? val : parsed;
    } else if (kind === "number") {
      const parsed = Number.parseFloat(val.trim());
      finalValue = Number.isNaN(parsed) ? val : parsed;
    }
    onApply(op, finalValue);
  }

  return (
    <>
      <div className="flex items-center justify-between border-b border-border bg-surface-2/60 px-3 py-2">
        <span className="truncate font-semibold text-xs text-fg">{colName}</span>
        <span className={badge}>{typeLabel}</span>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          handleApply();
        }}
        className="space-y-3 p-3"
      >
        <div>
          <label htmlFor={opId} className={label}>
            Operator
          </label>
          <select
            id={opId}
            value={op}
            onChange={(e) => setOp(e.target.value as FilterOp)}
            className={select}
            aria-label="Operator"
          >
            {availableOps.map((o) => (
              <option key={o.op} value={o.op}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
        {!isNullOrNotNull && (
          <div>
            <label htmlFor={valId} className={label}>
              Value
            </label>
            {isBoolean ? (
              <select
                id={valId}
                value={val}
                onChange={(e) => setVal(e.target.value)}
                className={select}
                aria-label="Value"
              >
                <option value="true">Yes</option>
                <option value="false">No</option>
              </select>
            ) : (
              <input
                id={valId}
                type={isNumeric ? "number" : "text"}
                step={kind === "integer" ? "1" : "any"}
                value={val}
                onChange={(e) => setVal(e.target.value)}
                placeholder="Filter value…"
                className={input}
                aria-label="Value"
                autoFocus
              />
            )}
          </div>
        )}
        <div className="flex items-center justify-between gap-2 border-t border-border pt-2">
          <button
            type="button"
            className={`${btnGhostSm} text-danger hover:text-danger`}
            onClick={onRemove}
          >
            Remove Filter
          </button>
          <button type="submit" className={`${btnPrimary} h-7 px-3 text-xs`}>
            Apply Changes
          </button>
        </div>
      </form>
    </>
  );
}

interface FilterModifierPopoverProps {
  condition: Condition;
  column?: Column;
  onApply: (operator: FilterOp, value: unknown) => void;
  onRemove: () => void;
  children: ReactNode;
}

export function FilterModifierPopover({
  condition,
  column,
  onApply,
  onRemove,
  children,
}: FilterModifierPopoverProps) {
  const [open, setOpen] = useState(false);

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>{children}</Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="bottom"
          align="start"
          sideOffset={6}
          className="ui-menu z-50 w-72 rounded-xl border border-border bg-surface text-sm text-fg shadow-lg animate-in fade-in-0 zoom-in-95"
        >
          {open && (
            <FilterModifierForm
              condition={condition}
              column={column}
              onApply={(op, val) => {
                onApply(op, val);
                setOpen(false);
              }}
              onRemove={() => {
                onRemove();
                setOpen(false);
              }}
            />
          )}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

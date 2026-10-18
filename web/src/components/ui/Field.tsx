import { createContext, use, useId, type InputHTMLAttributes, type ReactNode, type Ref, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { fieldError, hint as hintClass, input, label as labelClass, select, textarea } from "@/components/styles";

interface FieldWiring {
  id: string;
  describedBy: string | undefined;
  invalid: boolean;
  required: boolean;
}

const FieldContext = createContext<FieldWiring | null>(null);

/**
 * A labelled form field (DESIGN.md "Forms and settings"): the label above, the hint and the error
 * below. The `Input`, `Select` or `Textarea` inside picks up the id, `aria-describedby` (hint and
 * error) and `aria-invalid` from the field, so the wiring is never done by hand.
 *
 *   <Field label="Name" hint="Shown in the navigation pane." error={errors.name}><Input value={…} /></Field>
 */
export function Field({
  label,
  hint,
  error,
  required = false,
  id: givenId,
  className = "",
  children,
}: {
  label: ReactNode;
  hint?: ReactNode;
  /** The validation message; when set the control is marked invalid. */
  error?: ReactNode;
  required?: boolean;
  id?: string;
  className?: string;
  children: ReactNode;
}) {
  const auto = useId();
  const id = givenId ?? `f${auto}`;
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(" ") || undefined;
  return (
    <div className={className}>
      <label htmlFor={id} className={labelClass}>
        {label}
        {required && (
          <span aria-hidden="true" className="ml-0.5 text-danger">
            *
          </span>
        )}
      </label>
      <FieldContext value={{ id, describedBy, invalid: !!error, required }}>{children}</FieldContext>
      {hint && (
        <p id={hintId} className={hintClass}>
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} className={fieldError}>
          {error}
        </p>
      )}
    </div>
  );
}

/** The props a control takes from its enclosing `Field`; its own props win. */
function useWiring<P extends { id?: string; "aria-describedby"?: string; "aria-invalid"?: InputHTMLAttributes<HTMLInputElement>["aria-invalid"]; required?: boolean }>(props: P): P {
  const f = use(FieldContext);
  if (!f) return props;
  return {
    ...props,
    id: props.id ?? f.id,
    "aria-describedby": [f.describedBy, props["aria-describedby"]].filter(Boolean).join(" ") || undefined,
    "aria-invalid": props["aria-invalid"] ?? (f.invalid || undefined),
    required: props.required ?? (f.required || undefined),
  };
}

export function Input({ className = "", ref, ...props }: InputHTMLAttributes<HTMLInputElement> & { ref?: Ref<HTMLInputElement> }) {
  return <input ref={ref} className={`${input} ${className}`.trim()} {...useWiring(props)} />;
}

export function Select({ className = "", ref, ...props }: SelectHTMLAttributes<HTMLSelectElement> & { ref?: Ref<HTMLSelectElement> }) {
  return <select ref={ref} className={`${select} ${className}`.trim()} {...useWiring(props)} />;
}

export function Textarea({ className = "", ref, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement> & { ref?: Ref<HTMLTextAreaElement> }) {
  return <textarea ref={ref} className={`${textarea} ${className}`.trim()} {...useWiring(props)} />;
}

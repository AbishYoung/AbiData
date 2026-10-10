import { useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { input, label as labelClass } from "@/components/styles";

interface Common {
  label: string;
  error?: string | undefined;
  hint?: string | undefined;
}

function Described({ id, error, hint }: { id: string; error?: string | undefined; hint?: string | undefined }): ReactNode {
  return (
    <>
      {hint && (
        <p id={`${id}-hint`} className="m-0 mt-0.5 text-xs text-fg-muted">
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-err`} className="m-0 mt-0.5 text-xs font-medium text-danger">
          Problem: {error}
        </p>
      )}
    </>
  );
}

function describedBy(id: string, error?: string, hint?: string): string | undefined {
  const parts = [hint ? `${id}-hint` : "", error ? `${id}-err` : ""].filter(Boolean);
  return parts.length > 0 ? parts.join(" ") : undefined;
}

export function TextField({ label, error, hint, ...rest }: Common & InputHTMLAttributes<HTMLInputElement>) {
  const id = useId();
  return (
    <div className="mb-2">
      <label className={`${labelClass} !mb-0.5`} htmlFor={id}>
        {label}
      </label>
      <input id={id} className={input} aria-invalid={error ? true : undefined} aria-describedby={describedBy(id, error, hint)} {...rest} />
      <Described id={id} error={error} hint={hint} />
    </div>
  );
}

export function AreaField({ label, error, hint, ...rest }: Common & TextareaHTMLAttributes<HTMLTextAreaElement>) {
  const id = useId();
  return (
    <div className="mb-2">
      <label className={`${labelClass} !mb-0.5`} htmlFor={id}>
        {label}
      </label>
      <textarea
        id={id}
        rows={3}
        spellCheck={false}
        className={`${input} font-mono`}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy(id, error, hint)}
        {...rest}
      />
      <Described id={id} error={error} hint={hint} />
    </div>
  );
}

export function SelectField({ label, error, hint, children, ...rest }: Common & SelectHTMLAttributes<HTMLSelectElement>) {
  const id = useId();
  return (
    <div className="mb-2">
      <label className={`${labelClass} !mb-0.5`} htmlFor={id}>
        {label}
      </label>
      <select id={id} className={input} aria-invalid={error ? true : undefined} aria-describedby={describedBy(id, error, hint)} {...rest}>
        {children}
      </select>
      <Described id={id} error={error} hint={hint} />
    </div>
  );
}

export function CheckField({ label, hint, ...rest }: Omit<Common, "error"> & InputHTMLAttributes<HTMLInputElement>) {
  const id = useId();
  return (
    <div className="mb-1">
      <label htmlFor={id} className="inline-flex cursor-pointer items-center gap-2 text-sm">
        <input id={id} type="checkbox" aria-describedby={hint ? `${id}-hint` : undefined} {...rest} />
        {label}
      </label>
      {hint && (
        <p id={`${id}-hint`} className="m-0 text-xs text-fg-muted">
          {hint}
        </p>
      )}
    </div>
  );
}

export function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="m-0 mb-3 min-w-0 border-0 border-t border-border p-0 pt-2">
      <legend className="px-0 text-sm font-semibold">{title}</legend>
      {children}
    </fieldset>
  );
}

import { useCallback, useEffect, useId, useMemo, useRef, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, FileText, ScrollText } from "lucide-react";
import { SkeletonLines } from "@/components/EditorParts";
import { alertBox, btn, btnPrimary, card, fieldError, input, label as labelClass, select, successBox, textarea, warningBox } from "@/components/styles";
import { ApiError } from "@/lib/api";
import type { ParamType } from "@/features/queries/model";
import { ReportRunView } from "@/features/reports/ReportTab";
import { applyPatches, type Overrides } from "@/features/forms/runtime/model";
import { formatCalcValue } from "@/features/forms/runtime/format";
import {
  appKeys,
  getLookupOptions,
  getPublicApp,
  getPublicForm,
  getPublicReport,
  publicFormEvent,
  runPublicReport,
  submitPublicForm,
  type FormEventResponse,
  type PublicColumn,
  type PublicForm,
} from "./appsApi";
import { buildSubmission, encode, entries, inputType, type Entry } from "./publicFormLogic";

// The page a visitor reaches through an app's secret link (ADR 0014). It shows
// only the app: no workspace, navigation pane or account menu.

function problem(e: unknown, fallback: string): string {
  if (e instanceof ApiError) {
    if (e.status === 404) return "This link does not work. It may have been turned off or replaced. Ask whoever shared it for a new one.";
    if (e.code === "APP_ITEM_CHANGED") return "This page is being updated. Try again later.";
    return e.message;
  }
  return fallback;
}

export function PublicAppPage({ token, item, onNavigate }: { token: string; item: string | undefined; onNavigate: (item: string) => void }) {
  const app = useQuery({ queryKey: appKeys.public(token), queryFn: () => getPublicApp(token), retry: false });
  const items = app.data?.items ?? [];
  const current = items.find((i) => i.id === (item ?? app.data?.start_item)) ?? items[0];

  useEffect(() => {
    if (app.data) document.title = current ? `${current.title} · ${app.data.name}` : app.data.name;
  }, [app.data, current]);

  if (app.isError) {
    return (
      <main className="mx-auto max-w-xl p-6">
        <h1 className="mb-3 text-xl font-semibold">Page unavailable</h1>
        <div role="alert" className={alertBox}>
          {problem(app.error, "The page could not be opened.")}
        </div>
      </main>
    );
  }
  if (!app.data) return <SkeletonLines rows={4} label="Opening…" />;

  return (
    <div className="flex min-h-dvh flex-col bg-bg text-fg" data-testid="public-app">
      <header className="border-b border-border bg-surface">
        <div className="mx-auto flex max-w-4xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <h1 className="m-0 text-lg font-semibold tracking-tight">{app.data.name}</h1>
          {items.length > 1 && (
            <nav aria-label="Pages">
              <ul className="m-0 flex list-none flex-wrap gap-1 p-0">
                {items.map((i) => (
                  <li key={i.id}>
                    <a
                      href={`/a/${token}/${i.id}`}
                      aria-current={i.id === current?.id ? "page" : undefined}
                      onClick={(e) => {
                        e.preventDefault();
                        onNavigate(i.id);
                      }}
                      className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm ${
                        i.id === current?.id ? "bg-accent-soft font-medium text-accent" : "text-fg-muted hover:bg-surface-2 hover:text-fg"
                      }`}
                    >
                      {i.kind === "form" ? <FileText size={14} aria-hidden="true" /> : <ScrollText size={14} aria-hidden="true" />}
                      {i.title}
                    </a>
                  </li>
                ))}
              </ul>
            </nav>
          )}
        </div>
      </header>
      <main className="flex min-h-0 flex-1 flex-col">
        {!current && <p className="mx-auto max-w-xl p-6 text-fg-muted">This app has no pages yet.</p>}
        {current?.kind === "form" && <PublicFormView key={current.id} token={token} item={current.id} title={current.title} />}
        {current?.kind === "report" && <PublicReportView key={current.id} token={token} item={current.id} />}
      </main>
    </div>
  );
}

// ---- forms ----------------------------------------------------------------------------

function PublicFormView({ token, item, title }: { token: string; item: string; title: string }) {
  const form = useQuery({ queryKey: appKeys.form(token, item), queryFn: () => getPublicForm(token, item), retry: false });
  if (form.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4 mx-auto max-w-xl`}>
        {problem(form.error, "The form could not be opened.")}
      </div>
    );
  }
  if (!form.data) return <SkeletonLines rows={6} label="Opening form…" />;
  return <FormBody token={token} item={item} title={title} form={form.data} />;
}

interface Notice {
  level: string;
  text: string;
}

function FormBody({ token, item, title, form }: { token: string; item: string; title: string; form: PublicForm }) {
  const list = useMemo(() => entries(form), [form]);
  const fieldEntries = useMemo(() => list.filter((e): e is Extract<Entry, { kind: "field" }> => e.kind === "field"), [list]);
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [overrides, setOverrides] = useState<Overrides>({});
  const [calc, setCalc] = useState<Record<string, unknown>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [notices, setNotices] = useState<Notice[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<{ continuation: string; message: string } | null>(null);
  const [phase, setPhase] = useState<"editing" | "submitting" | "done">("editing");
  const idem = useRef(crypto.randomUUID());
  const ids = useId();
  const valuesRef = useRef(values);
  const overridesRef = useRef(overrides);
  useEffect(() => {
    valuesRef.current = values;
    overridesRef.current = overrides;
  });

  const colById = useMemo(() => new Map(fieldEntries.map((e) => [e.column.id, e.column])), [fieldEntries]);
  const record = useCallback(() => {
    const out: Record<string, unknown> = {};
    for (const [id, raw] of Object.entries(valuesRef.current)) {
      const col = colById.get(id);
      if (!col) continue;
      const r = encode(col, raw);
      if ("value" in r) out[id] = r.value;
    }
    return out;
  }, [colById]);

  const apply = useCallback(
    (resp: FormEventResponse) => {
      setOverrides((o) => applyPatches(o, resp.patches));
      setCalc((c) => ({ ...c, ...(resp.calc ?? {}) }));
      if (resp.record && Object.keys(resp.record).length > 0) {
        setValues((v) => {
          const next = { ...v };
          for (const [id, val] of Object.entries(resp.record)) {
            const col = colById.get(id);
            if (col) next[id] = col.field_type === "yes_no" ? val === true || val === 1 : val === null ? "" : val;
          }
          return next;
        });
      }
      const n = (resp.notifications ?? []).map((x) => ({ level: x.level, text: x.text }));
      if (resp.cancel) n.push({ level: "warning", text: resp.cancel.message });
      if (n.length > 0) setNotices((prev) => [...prev, ...n]);
      if (resp.confirm) setConfirm(resp.confirm);
      if (resp.focus) document.getElementById(`${ids}-${resp.focus}`)?.focus();
    },
    [colById, ids],
  );

  const send = useCallback(
    (body: Parameters<typeof publicFormEvent>[2]) =>
      publicFormEvent(token, item, { ...body, is_new: true, record: record(), controls: overridesRef.current as Record<string, Record<string, unknown>> })
        .then(apply)
        .catch((e: unknown) => setFailure(problem(e, "The form could not be updated."))),
    [token, item, record, apply],
  );

  const start = useCallback(() => {
    setValues({});
    setOverrides({});
    setCalc({});
    setErrors({});
    setNotices([]);
    setFailure(null);
    setPhase("editing");
    idem.current = crypto.randomUUID();
    void send({ event: "load" });
  }, [send]);

  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    start();
  }, [start]);

  function change(control: string, colId: string, value: unknown, notify: boolean) {
    const old = valuesRef.current[colId];
    valuesRef.current = { ...valuesRef.current, [colId]: value };
    setValues(valuesRef.current);
    setErrors((e) => {
      if (!(colId in e)) return e;
      return Object.fromEntries(Object.entries(e).filter(([k]) => k !== colId));
    });
    if (notify) void send({ event: "change", control, old_value: old ?? null, new_value: value ?? null, dirty_fields: [colId] });
  }

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setNotices([]);
    setFailure(null);
    const active = fieldEntries.filter((e) => {
      const o = overrides[e.control.name];
      return (o?.visible ?? e.control.visible ?? true) && (o?.enabled ?? e.control.enabled ?? true) && !(o?.locked ?? e.control.locked);
    });
    const built = buildSubmission(
      active.map((e) => ({ column: e.column, required: overrides[e.control.name]?.required ?? e.column.required })),
      values,
    );
    if ("errors" in built) {
      setErrors(built.errors);
      const first = active.find((e) => built.errors[e.column.id]);
      if (first) document.getElementById(`${ids}-${first.control.name}`)?.focus();
      return;
    }
    setPhase("submitting");
    try {
      const res = await submitPublicForm(token, item, built.fields, idem.current);
      const n = (res.notifications ?? []).map((x) => ({ level: x.level, text: x.text }));
      if (res.cancel) {
        setNotices([...n, { level: "warning", text: res.cancel.message }]);
        setPhase("editing");
        return;
      }
      setNotices(n);
      setPhase(res.submitted ? "done" : "editing");
    } catch (e) {
      setFailure(problem(e, "Your answers could not be sent. Try again."));
      setPhase("editing");
    }
  }

  if (phase === "done") {
    return (
      <section className={`${card} mx-auto my-6 w-[min(36rem,calc(100%-2rem))] p-6`} aria-labelledby={`${ids}-done`}>
        <h2 id={`${ids}-done`} className="m-0 mb-2 flex items-center gap-2 text-lg font-semibold">
          <CheckCircle2 size={20} aria-hidden="true" className="text-success" />
          Thank you
        </h2>
        <p role="status" className="m-0 text-fg-muted">
          Your answers to “{title}” were sent.
        </p>
        {notices.map((n, i) => (
          <p key={i} className={`${successBox} mt-3`}>
            {n.text}
          </p>
        ))}
        <button type="button" className={`${btn} mt-4`} onClick={start}>
          Fill in the form again
        </button>
      </section>
    );
  }

  const busy = phase === "submitting";
  return (
    <form
      noValidate
      onSubmit={(e) => void submit(e)}
      aria-labelledby={`${ids}-title`}
      className={`${card} mx-auto my-6 w-[min(36rem,calc(100%-2rem))] p-6`}
      data-testid="public-form"
    >
      <h2 id={`${ids}-title`} className="m-0 mb-4 text-lg font-semibold">
        {title}
      </h2>
      {list.map((e) => {
        const o = overrides[e.control.name];
        if (!(o?.visible ?? e.control.visible ?? true)) return null;
        if (e.kind === "text") {
          return (
            <p key={e.control.id} className="m-0 mb-4 text-sm">
              {o?.caption ?? e.text}
            </p>
          );
        }
        if (e.kind === "calc") {
          return (
            <div key={e.control.id} className="mb-4">
              <span className={labelClass}>{o?.caption ?? e.label}</span>
              <output id={`${ids}-${e.control.name}`} className="block text-sm" aria-live="polite">
                {formatCalcValue(calc[e.control.name] ?? calc[e.control.id], e.control.format ?? "")}
              </output>
            </div>
          );
        }
        const disabled = busy || !(o?.enabled ?? e.control.enabled ?? true) || (o?.locked ?? e.control.locked ?? false);
        const required = o?.required ?? e.column.required;
        return (
          <Field
            key={e.control.id}
            id={`${ids}-${e.control.name}`}
            entry={e}
            label={o?.caption && e.control.type !== "textbox" ? o.caption : e.label}
            required={required}
            disabled={disabled}
            value={values[e.column.id]}
            error={errors[e.column.id]}
            token={token}
            item={item}
            onChange={(v, notify) => change(e.control.name, e.column.id, v, notify)}
          />
        );
      })}
      <div aria-live="polite">
        {notices.map((n, i) => (
          <p key={i} className={`${n.level === "error" ? alertBox : n.level === "success" ? successBox : warningBox} mb-3`}>
            {n.text}
          </p>
        ))}
      </div>
      {failure && (
        <div role="alert" className={`${alertBox} mb-3`}>
          {failure}
        </div>
      )}
      {Object.keys(errors).length > 0 && (
        <div role="alert" className={`${alertBox} mb-3`}>
          Check the highlighted fields.
        </div>
      )}
      {confirm && (
        <div role="alertdialog" aria-labelledby={`${ids}-confirm`} className={`${warningBox} mb-3`}>
          <p id={`${ids}-confirm`} className="m-0 mb-2">
            {confirm.message}
          </p>
          <div className="flex gap-2">
            {[true, false].map((answer) => (
              <button
                key={String(answer)}
                type="button"
                className={answer ? btnPrimary : btn}
                onClick={() => {
                  setConfirm(null);
                  void send({ event: "confirm", continuation: confirm.continuation, answer });
                }}
              >
                {answer ? "Yes" : "No"}
              </button>
            ))}
          </div>
        </div>
      )}
      <button type="submit" className={btnPrimary} disabled={busy} aria-busy={busy}>
        {busy ? "Sending…" : "Submit"}
      </button>
    </form>
  );
}

function Field({
  id,
  entry,
  label,
  required,
  disabled,
  value,
  error,
  token,
  item,
  onChange,
}: {
  id: string;
  entry: Extract<Entry, { kind: "field" }>;
  label: string;
  required: boolean;
  disabled: boolean;
  value: unknown;
  error: string | undefined;
  token: string;
  item: string;
  onChange: (value: unknown, notify: boolean) => void;
}) {
  const col: PublicColumn = entry.column;
  const errId = `${id}-error`;
  const described = error ? { "aria-invalid": true as const, "aria-describedby": errId } : {};
  const kind = entry.control.type === "combobox" ? "select" : entry.control.type === "checkbox" ? "checkbox" : inputType(col);
  const text = value === undefined || value === null ? "" : String(value);
  const mark = required ? (
    <span aria-hidden="true" className="ml-0.5 text-danger">
      *
    </span>
  ) : null;

  let control;
  if (kind === "checkbox") {
    return (
      <div className="mb-4">
        <label className="flex items-center gap-2 text-sm font-medium">
          <input id={id} type="checkbox" checked={value === true} disabled={disabled} onChange={(e) => onChange(e.target.checked, true)} {...described} />
          {label}
        </label>
        {error && (
          <p id={errId} className={fieldError}>
            {error}
          </p>
        )}
      </div>
    );
  }
  if (kind === "select") {
    control = <LookupSelect id={id} token={token} item={item} control={entry.control.name} value={text} disabled={disabled} required={required} described={described} onChange={(v) => onChange(v, true)} />;
  } else if (kind === "textarea") {
    control = (
      <textarea id={id} className={textarea} value={text} disabled={disabled} required={required} onChange={(e) => onChange(e.target.value, false)} onBlur={() => onChange(value, true)} {...described} />
    );
  } else {
    control = (
      <input
        id={id}
        className={input}
        type={kind}
        inputMode={col.field_type === "currency" ? "decimal" : undefined}
        step={col.field_type === "integer" ? 1 : col.field_type === "number" || col.field_type === "currency" ? "any" : undefined}
        maxLength={col.max_length || undefined}
        value={text}
        disabled={disabled}
        required={required}
        autoComplete="off"
        onChange={(e) => onChange(e.target.value, false)}
        onBlur={() => onChange(value, true)}
        {...described}
      />
    );
  }
  return (
    <div className="mb-4">
      <label htmlFor={id} className={labelClass}>
        {label}
        {mark}
      </label>
      {control}
      {error && (
        <p id={errId} className={fieldError}>
          {error}
        </p>
      )}
    </div>
  );
}

function LookupSelect({
  id,
  token,
  item,
  control,
  value,
  disabled,
  required,
  described,
  onChange,
}: {
  id: string;
  token: string;
  item: string;
  control: string;
  value: string;
  disabled: boolean;
  required: boolean;
  described: Record<string, unknown>;
  onChange: (v: unknown) => void;
}) {
  const opts = useQuery({ queryKey: appKeys.lookup(token, item, control), queryFn: () => getLookupOptions(token, item, control), staleTime: 60_000 });
  const list = opts.data ?? [];
  return (
    <select
      id={id}
      className={select}
      value={value}
      disabled={disabled || !opts.data}
      required={required}
      aria-busy={opts.isPending}
      onChange={(e) => {
        const hit = list.find((o) => String(o.value) === e.target.value);
        onChange(hit ? hit.value : "");
      }}
      {...described}
    >
      <option value="">{opts.isPending ? "Loading…" : opts.isError ? "The choices could not be loaded" : "Choose…"}</option>
      {list.map((o) => (
        <option key={String(o.value)} value={String(o.value)}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

// ---- reports ------------------------------------------------------------------------

function PublicReportView({ token, item }: { token: string; item: string }) {
  const rep = useQuery({ queryKey: appKeys.report(token, item), queryFn: () => getPublicReport(token, item), retry: false });
  const declared = useMemo(() => (rep.data?.parameters ?? []).map((p) => ({ name: p.name, type: p.type as ParamType })), [rep.data]);
  const run = useCallback(
    (values: Record<string, string | number | boolean>, signal: AbortSignal, tick: (n: number) => void) => runPublicReport(token, item, values, signal, tick),
    [token, item],
  );
  if (rep.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4 mx-auto max-w-xl`}>
        {problem(rep.error, "The report could not be opened.")}
      </div>
    );
  }
  return (
    <ReportRunView
      def={rep.data ? { layout: rep.data.layout, page_setup: rep.data.page_setup } : undefined}
      loadError={null}
      sourceReady={rep.data !== undefined}
      declared={declared}
      memoryKey={`app:${token}:${item}`}
      run={run}
    />
  );
}

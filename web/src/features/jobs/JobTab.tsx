import { KindIcon } from "@/components/objectKinds";
import { EmptyState, ToolbarDivider, docToolbar } from "@/components/EditorParts";
import { useMemo, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, Play, Save, Trash2 } from "lucide-react";
import { useTabs, useWorkspaceTabs } from "@/app/tabs";
import { alertBox, badgeSuccess, badgeWarning, btn, btnGhost, btnPrimary, card, eyebrow, input, textarea, label as labelClass } from "@/components/styles";
import { Tip } from "@/components/Tip";
import { ApiError } from "@/lib/api";
import { createScript, listScripts, scriptKeys, type Script } from "@/features/scripts/scriptsApi";
import { generate, NO_TEMPLATE, TemplatePicker, type TemplateChoice } from "@/features/scripts/TemplatePicker";
import { useSchema } from "@/lib/schema";
import { DeleteJobDialog, RunWithArgsDialog } from "./JobDialogs";
import {
  formToInput,
  intError,
  jobToForm,
  MISFIRE_HELP,
  newJobValues,
  OVERLAP_HELP,
  parseArgs,
  timeZones,
  type JobFormValues,
} from "./jobForm";
import { createJob, getJob, jobKeys, runJob, updateJob, type Job } from "./jobsApi";
import { RunsPane } from "./RunsPane";
import {
  describeSchedule,
  isValidMinute,
  isValidTime,
  PRESET_LABEL,
  PRESETS,
  type SchedulePreset,
} from "./schedule";
import { useDirtyGuard } from "@/lib/useDirtyGuard";
import { matchesShortcut } from "@/lib/shortcuts";

function isSave(e: React.KeyboardEvent): boolean {
  return matchesShortcut(e, "design.save");
}

function ErrorBox({ error, children }: { error: unknown; children?: ReactNode }) {
  if (!(error instanceof ApiError)) return null;
  return (
    <div role="alert" className={`${alertBox} mx-3 my-2`}>
      <p className="m-0 font-semibold">{error.title}</p>
      <p className="m-0">{error.message}</p>
      {error.recovery && <p className="m-0 text-fg-muted">{error.recovery}</p>}
      {children}
    </div>
  );
}

/** A job: settings form on one side, run history and run detail on the other. jobId null creates one. */
export function JobTab({ ws, jobId, tabKey }: { ws: string; jobId: string | null; tabKey: string }) {
  return jobId === null ? <JobEditor key={tabKey} ws={ws} tabKey={tabKey} job={null} /> : <SavedJob key={jobId} ws={ws} jobId={jobId} tabKey={tabKey} />;
}

function SavedJob({ ws, jobId, tabKey }: { ws: string; jobId: string; tabKey: string }) {
  const q = useQuery({ queryKey: jobKeys.one(ws, jobId), queryFn: () => getJob(ws, jobId) });
  const { tabs } = useWorkspaceTabs(ws);
  const dirty = tabs.find((t) => t.key === tabKey)?.dirty ?? false;
  if (q.data) {
    // While the form is clean it re-mounts on each saved version, so it always shows the latest.
    return <JobEditor key={dirty ? "editing" : q.data.version} ws={ws} tabKey={tabKey} job={q.data} />;
  }
  if (q.isError) {
    return (
      <div className="p-4">
        <ErrorBox error={q.error} />
        <button type="button" className={btn} onClick={() => void q.refetch()}>
          Try again
        </button>
      </div>
    );
  }
  return (
    <p role="status" className="p-4 text-fg-muted">
      Loading job…
    </p>
  );
}

/** Creates a job script, adding " 2", " 3"… to the name while a script of that name exists. */
async function createJobScript(ws: string, name: string, source: string): Promise<Script> {
  for (let n = 1; ; n++) {
    try {
      return await createScript(ws, { name: n === 1 ? name : `${name} ${n}`, kind: "job", source });
    } catch (e) {
      if (!(e instanceof ApiError && e.code === "DUPLICATE_VALUE") || n >= 20) throw e;
    }
  }
}

const SERVER_FIELDS: Readonly<Record<string, keyof JobFormValues>> = { schedule: "cron", script_id: "script_id", timezone: "timezone" };

function JobEditor({ ws, tabKey, job }: { ws: string; tabKey: string; job: Job | null }) {
  const qc = useQueryClient();
  const renameTab = useTabs((s) => s.rename);
  const openTab = useTabs((s) => s.open);
  const [base, setBase] = useState(job?.version ?? 0);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [runWithArgs, setRunWithArgs] = useState(false);
  const [formError, setFormError] = useState<unknown>(null);
  // A new job can start from a job template: saving then creates the job script from it first.
  const [choice, setChoice] = useState<TemplateChoice>(NO_TEMPLATE);

  const form = useForm<JobFormValues>({ defaultValues: job ? jobToForm(job) : newJobValues("") });
  const {
    register,
    handleSubmit,
    reset,
    setError,
    clearErrors,
    watch,
    setValue,
    formState: { errors, isDirty },
  } = form;
  useDirtyGuard(ws, tabKey, isDirty);

  const v = watch();
  const scripts = useQuery({ queryKey: scriptKeys.list(ws), queryFn: () => listScripts(ws) });
  const jobScripts = (scripts.data ?? []).filter((s) => s.kind === "job").sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
  const zones = useMemo(() => timeZones(job?.timezone), [job?.timezone]);
  const changedElsewhere = job !== null && job.version !== base;
  const schema = useSchema(ws, choice.templateId !== "");
  const fromTemplate = job === null && choice.templateId !== "";
  const generated = fromTemplate ? generate(choice, schema.data) : null;

  function chooseTemplate(c: TemplateChoice): void {
    setChoice(c);
    const g = generate(c, schema.data);
    const suggestion = g?.template.job;
    if (!g || !suggestion) return;
    const opts = { shouldDirty: true, shouldValidate: false } as const;
    if (form.getValues("name").trim() === "") setValue("name", g.name, opts);
    setValue("preset", suggestion.preset, opts);
    if (suggestion.time) setValue("time", suggestion.time, opts);
    setValue("args", JSON.stringify(suggestion.args, null, 2), opts);
    clearErrors("script_id");
  }

  const save = useMutation({
    meta: { inline: true },
    mutationFn: async (values: JobFormValues) => {
      const body = formToInput(values);
      if (!body) throw new Error("invalid args");
      if (job) return updateJob(ws, job.id, base, body);
      if (fromTemplate) {
        if (!generated) throw new Error("template needs a table");
        const script = await createJobScript(ws, body.name, generated.source);
        // From here on the job uses the new script, so a failed job save is not retried with a second copy.
        void qc.invalidateQueries({ queryKey: scriptKeys.all(ws) });
        setChoice(NO_TEMPLATE);
        setValue("script_id", script.id);
        body.script_id = script.id;
      }
      return createJob(ws, body);
    },
    onSuccess: (saved) => {
      setFormError(null);
      qc.setQueryData(jobKeys.one(ws, saved.id), saved);
      void qc.invalidateQueries({ queryKey: jobKeys.list(ws) });
      reset(jobToForm(saved));
      setBase(saved.version);
      if (!job) renameTab(ws, tabKey, saved.name, saved.id);
      else renameTab(ws, tabKey, saved.name);
    },
    onError: (e) => {
      const field = e instanceof ApiError && typeof e.details?.field === "string" ? SERVER_FIELDS[e.details.field] : undefined;
      if (field && e instanceof ApiError && (field !== "cron" || form.getValues("preset") === "custom")) {
        // A schedule typed with a preset cannot be invalid, so the custom field is the place to show it.
        setError(field, { type: "server", message: e.message });
        setFormError(null);
      } else {
        setFormError(e);
      }
    },
  });

  const runNow = useMutation({
    meta: { inline: true },
    mutationFn: () => runJob(ws, job?.id ?? ""),
    onSuccess: (r) => {
      setFormError(null);
      setSelectedRun(r.id);
      if (job) void qc.invalidateQueries({ queryKey: jobKeys.runs(ws, job.id) });
    },
    onError: (e) => setFormError(e),
  });

  function submit(): void {
    setFormError(null);
    if (fromTemplate && !generated) {
      setFormError(new ApiError({ status: 0, code: "BAD_REQUEST", title: "Choose a table", message: "This template needs a table. Choose one, or start from an existing job script." }));
      return;
    }
    void handleSubmit((values) => save.mutate(values))();
  }

  async function reload(): Promise<void> {
    if (!job) return;
    save.reset();
    setFormError(null);
    clearErrors();
    const fresh = await qc.fetchQuery({ queryKey: jobKeys.one(ws, job.id), queryFn: () => getJob(ws, job.id), staleTime: 0 });
    reset(jobToForm(fresh));
    setBase(fresh.version);
  }

  const conflict = formError instanceof ApiError && formError.code === "CONCURRENT_MODIFICATION";
  const scriptName = jobScripts.find((s) => s.id === v.script_id)?.name;
  const scheduleText = describeSchedule(v, v.timezone || "workspace time zone");
  const mmErr = (name: keyof JobFormValues): string | undefined => errors[name]?.message;
  const describe = (name: keyof JobFormValues, hint?: string) => [errors[name] ? `${name}-err` : null, hint ?? null].filter(Boolean).join(" ") || undefined;

  const field = (name: keyof JobFormValues, text: string, body: ReactNode, hint?: ReactNode) => (
    <div>
      <label className={labelClass} htmlFor={`job-${name}`}>
        {text}
      </label>
      {body}
      {mmErr(name) && (
        <p id={`${name}-err`} role="alert" className="m-0 mt-1 text-sm text-danger">
          {mmErr(name)}
        </p>
      )}
      {hint && (
        <p id={`${name}-hint`} className="m-0 mt-1 text-sm text-fg-muted">
          {hint}
        </p>
      )}
    </div>
  );

  return (
    <div
      data-testid="job-tab"
      className="flex min-h-0 flex-1 flex-col overflow-auto"
      onKeyDown={(e) => {
        if (isSave(e)) {
          e.preventDefault();
          submit();
        }
      }}
    >
      <div className={docToolbar}>
        <KindIcon kind="job" size="sm" />
        <h2 className="m-0 mr-2 text-sm font-semibold">{job ? job.name : "New job"}</h2>
        <Tip label="Save the job" shortcut="Mod+S">
          <button type="button" className={btnPrimary} disabled={(job !== null && !isDirty) || save.isPending} onClick={submit}>
            <Save size={16} aria-hidden="true" />
            Save
          </button>
        </Tip>
        {job && (
          <>
            <ToolbarDivider />
            <button type="button" className={btnGhost} disabled={isDirty || runNow.isPending} onClick={() => runNow.mutate()}>
              <Play size={16} aria-hidden="true" />
              Run now
            </button>
            <button type="button" className={btnGhost} disabled={isDirty} onClick={() => setRunWithArgs(true)}>
              Run with arguments…
            </button>
            <button type="button" className={btnGhost} onClick={() => setConfirmDelete(true)}>
              <Trash2 size={16} aria-hidden="true" />
              Delete
            </button>
          </>
        )}
        {(isDirty || job) && <span className={`ml-auto ${isDirty ? badgeWarning : badgeSuccess}`}>{isDirty ? "Unsaved changes" : "Saved"}</span>}
      </div>
      {job && isDirty && <p className="m-0 px-3 pt-1 text-sm text-fg-muted">Save your changes to run the job; a run uses the saved settings.</p>}
      {changedElsewhere && !conflict && (
        <div role="alert" className={`${alertBox} mx-3 my-2`}>
          <p className="m-0">This job was changed by someone else after you started editing. Saving will be refused.</p>
          <button type="button" className={`${btn} mt-2`} onClick={() => void reload()}>
            Discard my edits and reload
          </button>
        </div>
      )}
      <ErrorBox error={formError}>
        {conflict && (
          <button type="button" className={`${btn} mt-2`} onClick={() => void reload()}>
            Discard my edits and reload
          </button>
        )}
      </ErrorBox>

      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        <form
          aria-label="Job settings"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
          className="flex w-full shrink-0 flex-col gap-4 bg-bg p-4 lg:w-[26rem] lg:overflow-y-auto"
        >
          <div className={`${card} flex flex-col gap-4 p-4`}>
          <h3 className={`${eyebrow} m-0`}>Basics</h3>
          {job === null && (
            <div className="flex flex-col gap-3">
              <TemplatePicker ws={ws} kinds={["job"]} blankLabel="An existing job script" value={choice} onChange={chooseTemplate} />
            </div>
          )}
          {field(
            "name",
            "Job name",
            <input
              id="job-name"
              className={input}
              aria-invalid={errors.name ? true : undefined}
              aria-describedby={describe("name")}
              {...register("name", { validate: (x) => (x.trim() === "" ? "Enter a name." : x.trim().length > 200 ? "At most 200 characters." : true) })}
            />,
          )}

          {fromTemplate ? (
            <div>
              <p className="m-0 text-sm text-fg-muted">
                {generated
                  ? `Saving creates a job script named after the job from the “${generated.template.title}” template; open it in Script Studio afterwards to change it.`
                  : "Choose a table to fill in the template."}
              </p>
              {generated && (
                <details className="mt-2">
                  <summary className="cursor-pointer text-sm font-medium">Preview the script</summary>
                  <pre aria-label="Template script preview" className="mt-2 max-h-72 overflow-auto rounded-md border border-border bg-surface-2 p-2 font-mono text-xs">
                    {generated.source}
                  </pre>
                </details>
              )}
            </div>
          ) : field(
            "script_id",
            "Job script",
            <div className="flex gap-2">
              <select
                id="job-script_id"
                className={input}
                aria-invalid={errors.script_id ? true : undefined}
                aria-describedby={describe("script_id", "script_id-hint")}
                {...register("script_id", { validate: (x) => (x === "" && !fromTemplate ? "Choose a job script." : true) })}
              >
                <option value="">Choose a job script…</option>
                {jobScripts.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className={btn}
                disabled={!scriptName}
                aria-label={scriptName ? `Open ${scriptName} in Script Studio` : "Open script in Script Studio"}
                onClick={() => {
                  const s = jobScripts.find((x) => x.id === v.script_id);
                  if (s) openTab(ws, { key: `script:${s.id}`, kind: "script", objectId: s.id, view: "design", title: s.name });
                }}
              >
                <ExternalLink size={14} aria-hidden="true" />
                Open
              </button>
            </div>,
            scripts.isSuccess && jobScripts.length === 0
              ? "There are no job scripts yet. Start from a template above, or create one under Scripts → New script, kind Job."
              : "Only scripts of kind Job can be scheduled.",
          )}

          <div className="flex items-start gap-2">
            <input id="job-enabled" type="checkbox" className="mt-0.5 h-4 w-4 accent-accent" {...register("enabled")} />
            <label htmlFor="job-enabled" className="text-sm font-medium">
              Enabled
            </label>
            <span className="text-sm text-fg-muted">A disabled job never starts on its schedule; Run now still works.</span>
          </div>
          </div>

          <div className={`${card} p-4`}>
          <fieldset className="m-0 flex flex-col gap-3 border-0 p-0">
            <legend className={`${eyebrow} mb-3 p-0`}>Schedule</legend>
            <div>
              <label className={labelClass} htmlFor="job-preset">
                Repeat
              </label>
              <select id="job-preset" className={input} {...register("preset")}>
                {PRESETS.map((p: SchedulePreset) => (
                  <option key={p} value={p}>
                    {PRESET_LABEL[p]}
                  </option>
                ))}
              </select>
            </div>
            {v.preset === "hourly" &&
              field(
                "minute",
                "Minute past the hour",
                <input
                  id="job-minute"
                  inputMode="numeric"
                  className={input}
                  aria-invalid={errors.minute ? true : undefined}
                  aria-describedby={describe("minute")}
                  {...register("minute", { validate: (x) => (isValidMinute(x) ? true : "Enter a minute from 0 to 59.") })}
                />,
              )}
            {(v.preset === "daily" || v.preset === "weekdays") &&
              field(
                "time",
                "Time of day",
                <input
                  id="job-time"
                  type="time"
                  className={input}
                  aria-invalid={errors.time ? true : undefined}
                  aria-describedby={describe("time")}
                  {...register("time", { validate: (x) => (isValidTime(x) ? true : "Enter a time such as 09:30.") })}
                />,
              )}
            {v.preset === "custom" &&
              field(
                "cron",
                "Cron expression",
                <Tip label="Five fields: minute hour day month weekday. For example 30 9 * * 1-5 runs at 09:30 on weekdays." side="right">
                  <input
                    id="job-cron"
                    className={`${input} font-mono`}
                    spellCheck={false}
                    autoComplete="off"
                    aria-invalid={errors.cron ? true : undefined}
                    aria-describedby={describe("cron", "cron-hint")}
                    {...register("cron", { validate: (x) => (x.trim() === "" ? "Enter a cron expression or choose another schedule." : true) })}
                  />
                </Tip>,
                "Five fields (minute hour day month weekday), or @hourly, @daily, @weekly.",
              )}
            <p aria-live="polite" className="m-0 text-sm text-fg-muted">
              {scheduleText}
            </p>
          </fieldset>

          {field(
            "timezone",
            "Time zone",
            <select id="job-timezone" className={input} aria-invalid={errors.timezone ? true : undefined} aria-describedby={describe("timezone")} {...register("timezone")}>
              <option value="">Workspace default</option>
              {zones.map((z) => (
                <option key={z} value={z}>
                  {z}
                </option>
              ))}
            </select>,
            "The schedule is evaluated in this zone, including daylight saving changes.",
          )}
          </div>

          <div className={`${card} flex flex-col gap-4 p-4`}>
          <h3 className={`${eyebrow} m-0`}>Run behaviour</h3>

          {field(
            "timeout_seconds",
            "Timeout (seconds)",
            <input
              id="job-timeout_seconds"
              inputMode="numeric"
              className={input}
              aria-invalid={errors.timeout_seconds ? true : undefined}
              aria-describedby={describe("timeout_seconds")}
              {...register("timeout_seconds", { validate: (x) => intError(x, "The timeout", 1, 3600) })}
            />,
            "A run that takes longer is stopped and marked Timed out (1 to 3600).",
          )}

          {field(
            "overlap_policy",
            "If the previous run is still going",
            <select id="job-overlap_policy" className={input} aria-describedby="overlap_policy-hint" {...register("overlap_policy")}>
              <option value="skip">Skip the new run</option>
              <option value="queue">Queue the new run</option>
            </select>,
            OVERLAP_HELP[v.overlap_policy],
          )}

          {field(
            "misfire_policy",
            "After downtime",
            <select id="job-misfire_policy" className={input} aria-describedby="misfire_policy-hint" {...register("misfire_policy")}>
              <option value="run_once">Run once for missed times</option>
              <option value="skip">Skip missed times</option>
            </select>,
            MISFIRE_HELP[v.misfire_policy],
          )}

          <div className="grid grid-cols-2 gap-3">
            {field(
              "max_retries",
              "Retries",
              <input
                id="job-max_retries"
                inputMode="numeric"
                className={input}
                aria-invalid={errors.max_retries ? true : undefined}
                aria-describedby={describe("max_retries")}
                {...register("max_retries", { validate: (x) => intError(x, "Retries", 0, 10) })}
              />,
              "0 to 10 more attempts after a failure.",
            )}
            {field(
              "retry_backoff_seconds",
              "Retry delay (seconds)",
              <input
                id="job-retry_backoff_seconds"
                inputMode="numeric"
                className={input}
                aria-invalid={errors.retry_backoff_seconds ? true : undefined}
                aria-describedby={describe("retry_backoff_seconds")}
                {...register("retry_backoff_seconds", { validate: (x) => intError(x, "The retry delay", 0, 86400) })}
              />,
              "Wait before retrying; 0 uses 60.",
            )}
          </div>
          </div>

          <div className={`${card} p-4`}>
          {field(
            "args",
            "Arguments (JSON)",
            <textarea
              id="job-args"
              rows={6}
              spellCheck={false}
              className={`${textarea} font-mono`}
              aria-invalid={errors.args ? true : undefined}
              aria-describedby={describe("args", "args-hint")}
              {...register("args", {
                validate: (x) => {
                  const p = parseArgs(x);
                  return p.ok ? true : p.error;
                },
              })}
            />,
            <>
              Read in the script as job.args. Add <code>"allow_enqueue": true</code> to let forms start this job.
            </>,
          )}
          </div>
        </form>

        {job && (
          <div className="flex min-h-0 min-w-0 flex-1 flex-col border-t border-border lg:border-l lg:border-t-0">
            <RunsPane ws={ws} jobId={job.id} selected={selectedRun} onSelect={setSelectedRun} />
          </div>
        )}
        {!job && (
          <div className="flex flex-col lg:flex-1">
            <EmptyState kind="job" title="No run history yet" hint="Save the job to see its run history here." />
          </div>
        )}
      </div>
      {confirmDelete && job && <DeleteJobDialog ws={ws} job={job} onClose={() => setConfirmDelete(false)} />}
      {runWithArgs && job && (
        <RunWithArgsDialog
          ws={ws}
          job={job}
          onClose={() => setRunWithArgs(false)}
          onStarted={(r) => {
            setSelectedRun(r.id);
            void qc.invalidateQueries({ queryKey: jobKeys.runs(ws, job.id) });
          }}
        />
      )}
    </div>
  );
}

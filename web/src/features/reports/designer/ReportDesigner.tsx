import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { DndContext, DragOverlay, PointerSensor, useSensor, useSensors, type DragEndEvent, type DragMoveEvent, type DragStartEvent } from "@dnd-kit/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlignEndHorizontal,
  AlignEndVertical,
  AlignHorizontalSpaceAround,
  AlignStartHorizontal,
  AlignStartVertical,
  AlignVerticalSpaceAround,
  BoxSelect,
  Copy,
  Save,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { useTabs } from "@/app/tabs";
import { handleCanvasKey } from "@/components/designer/canvasKeys";
import { ToolbarButton, useSaveShortcut } from "@/components/designer/toolbar";
import { Tip } from "@/components/Tip";
import { alertBox, btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { useSchema } from "@/lib/schema";
import { LuaEditor } from "@/features/scripts/LuaEditor";
import { columnsOfSource, useSourceInfo } from "@/features/forms/designer/sourceColumns";
import { useSavedQueries } from "@/features/queries/savedQueries";
import { createReport, getReport, reportKeys, updateReport } from "../reportsApi";
import { BandCanvas, type DragDelta } from "./BandCanvas";
import { FieldList, Toolbox } from "./ReportPalette";
import { ReportPreview } from "./ReportPreview";
import { BandProperties, ControlProperties, GroupsPanel, ReportProperties, type Catalog } from "./ReportProperties";
import { draftKey, isDirty, useReportDrafts, type ReportPanel } from "./reportDraftStore";
import {
  PAPER_LABEL,
  addControl,
  addField,
  alignControls,
  contentWidth,
  controlFromMessage,
  deleteControls,
  distributeControls,
  dropInvalidColumns,
  duplicateControls,
  inputFromState,
  layoutWarnings,
  moveControls,
  moveToBand,
  newState,
  resizeControls,
  setBandProps,
  setRect,
  snap,
  snapshot,
  stateFromReport,
  stepSelection,
  validateState,
  type AlignMode,
  type BandId,
  type ControlType,
  type DesignControl,
  type DistributeAxis,
  type Orientation,
  type Paper,
  type ReportState,
  type SourceColumn,
  type SourceKind,
} from "./reportModel";

/** Edits a report's bands, groups and module. reportId null = new report. */
export function ReportDesigner({ ws, reportId, tabKey }: { ws: string; reportId: string | null; tabKey: string }) {
  const key = draftKey(ws, tabKey);
  const draft = useReportDrafts((s) => s.entries[key]);
  if (draft) return <DesignerBody ws={ws} tabKey={tabKey} />;
  if (reportId === null) return <SetupStep ws={ws} tabKey={tabKey} />;
  return <LoadReport ws={ws} reportId={reportId} tabKey={tabKey} />;
}

function SetupStep({ ws, tabKey }: { ws: string; tabKey: string }) {
  const nameId = useId();
  const kindId = useId();
  const sourceId = useId();
  const paperId = useId();
  const orientId = useId();
  const marginId = useId();
  const schema = useSchema(ws);
  const queries = useSavedQueries(ws);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<SourceKind>("table");
  const [source, setSource] = useState("");
  const [paper, setPaper] = useState<Paper>("letter");
  const [orientation, setOrientation] = useState<Orientation>("portrait");
  const [margin, setMargin] = useState("48");
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
      state: newState({ name, sourceKind: kind, sourceId: source, paper, orientation, margin: marginN }),
      base: "",
      saved: null,
      panel: "layout",
    });
  }

  return (
    <div className="p-4" data-testid="report-setup">
      <form onSubmit={start} className="max-w-md" aria-label="New report setup">
        <h2 className="m-0 mb-3 text-base font-semibold">New report</h2>
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

function LoadReport({ ws, reportId, tabKey }: { ws: string; reportId: string; tabKey: string }) {
  const q = useQuery({ queryKey: reportKeys.one(ws, reportId), queryFn: () => getReport(ws, reportId), staleTime: 0, gcTime: 0 });
  const data = q.data;
  useEffect(() => {
    if (!data) return;
    const state = stateFromReport(data);
    useReportDrafts.getState().put(draftKey(ws, tabKey), { state, base: snapshot(state), saved: data, panel: "layout" });
  }, [data, ws, tabKey]);
  if (q.isError) {
    return (
      <div className="p-4">
        {q.error instanceof ApiError && (
          <div role="alert" className={alertBox}>
            <p className="m-0 font-semibold">{q.error.title}</p>
            <p className="m-0">{q.error.message}</p>
          </div>
        )}
        <button type="button" className={`${btn} mt-2`} onClick={() => void q.refetch()}>
          Try again
        </button>
      </div>
    );
  }
  return (
    <p role="status" className="p-4 text-fg-muted">
      Loading report…
    </p>
  );
}

function DesignerBody({ ws, tabKey }: { ws: string; tabKey: string }) {
  const key = draftKey(ws, tabKey);
  const draft = useReportDrafts((s) => s.entries[key]);
  const patch = useReportDrafts((s) => s.patch);
  const qc = useQueryClient();
  const setDirty = useTabs((s) => s.setDirty);
  const renameTab = useTabs((s) => s.rename);
  const [selected, setSelected] = useState<string[]>([]);
  const [activeBand, setActiveBand] = useState<BandId>("detail");
  const [drag, setDrag] = useState<DragDelta | null>(null);
  const [overlay, setOverlay] = useState<string | null>(null);
  const [announce, setAnnounce] = useState("");
  const [notice, setNotice] = useState("");
  const nodes = useRef(new Map<string, HTMLElement>());
  const state = draft?.state;
  const info = useSourceInfo(ws, state?.sourceKind ?? "table", state?.sourceId ?? "");

  const update = useCallback((fn: (s: ReportState) => ReportState) => patch(key, (d) => ({ ...d, state: fn(d.state) })), [patch, key]);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  const issues = useMemo(() => (state ? validateState(state) : []), [state]);
  const warnings = useMemo(() => (state ? layoutWarnings(state) : []), [state]);
  const dirty = draft ? isDirty(draft) : false;

  useEffect(() => {
    setDirty(ws, tabKey, dirty);
  }, [ws, tabKey, dirty, setDirty]);
  useEffect(() => {
    if (!dirty) return;
    const h = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", h);
    return () => window.removeEventListener("beforeunload", h);
  }, [dirty]);

  const save = useMutation({
    meta: { inline: true },
    mutationFn: (s: ReportState) => {
      const body = inputFromState(s);
      const saved = useReportDrafts.getState().entries[key]?.saved ?? null;
      return saved ? updateReport(ws, saved.id, saved.version, body) : createReport(ws, body);
    },
    onSuccess: (saved, s) => {
      const wasNew = useReportDrafts.getState().entries[key]?.saved == null;
      qc.setQueryData(reportKeys.one(ws, saved.id), saved);
      void qc.invalidateQueries({ queryKey: reportKeys.all(ws) });
      patch(key, (d) => ({ ...d, saved, base: snapshot(s) }));
      if (wasNew) {
        useReportDrafts.getState().move(key, draftKey(ws, `report:${saved.id}`));
        renameTab(ws, tabKey, saved.name, saved.id);
      } else {
        renameTab(ws, tabKey, saved.name);
      }
    },
  });

  const doSave = useCallback(() => {
    const cur = useReportDrafts.getState().entries[key];
    if (!cur || save.isPending) return;
    if (validateState(cur.state).length > 0) {
      setNotice("Fix the problems listed before saving.");
      return;
    }
    setNotice("");
    save.mutate(cur.state);
  }, [key, save]);
  useSaveShortcut(doSave);

  if (!draft || !state) return null;
  const present = new Set(state.controls.map((c) => c.id));
  const selection = new Set(selected.filter((id) => present.has(id)));
  const primary = selected.length > 0 ? (selected[selected.length - 1] ?? null) : null;
  const order = stepSelection(state, null, 1);
  const tabStopId = primary && present.has(primary) ? primary : order;
  const panel = draft.panel;
  const band: BandId = activeBand in state.bands ? activeBand : "detail";
  const catalog: Catalog = { schema: info.schema, queries: info.queries, columns: info.columns };
  const columnName = (id: string) => info.columns.find((c) => c.id === id)?.name ?? id;
  const single = selection.size === 1 ? state.controls.find((c) => selection.has(c.id)) : undefined;

  function select(ids: string[]): void {
    setSelected(ids);
    const last = ids[ids.length - 1];
    const c = last ? state?.controls.find((x) => x.id === last) : undefined;
    if (c) setActiveBand(c.band);
  }
  function focusNode(id: string): void {
    requestAnimationFrame(() => nodes.current.get(id)?.focus());
  }
  function say(msg: string): void {
    setAnnounce(msg);
  }

  function addType(type: ControlType, target: BandId = band, at?: { x: number; y: number }): void {
    const r = addControl(state as ReportState, type, target, at?.x, at?.y);
    update(() => r.state);
    setActiveBand(target);
    setSelected([r.id]);
    const c = r.state.controls.find((x) => x.id === r.id);
    say(`Added ${c ? c.name : type} to the ${target.replace(/_/g, " ")} band`);
    focusNode(r.id);
  }

  function addColumn(col: SourceColumn, target: BandId = band, at?: { x: number; y: number }): void {
    const r = addField(state as ReportState, col, target, at);
    update(() => r.state);
    setActiveBand(target);
    setSelected(r.ids.slice(-1));
    say(r.ids.length > 1 ? `Added ${col.name} to the ${target.replace(/_/g, " ")} band with a caption label in the page header` : `Added ${col.name}`);
    const last = r.ids[r.ids.length - 1];
    if (last) focusNode(last);
  }

  function applyOp(fn: (s: ReportState, ids: ReadonlySet<string>) => ReportState): void {
    update((s) => fn(s, selection));
  }

  function removeSelected(): void {
    const n = selection.size;
    applyOp(deleteControls);
    setSelected([]);
    say(`Deleted ${n} control${n === 1 ? "" : "s"}`);
  }

  function duplicateSelected(): void {
    const r = duplicateControls(state as ReportState, selection);
    update(() => r.state);
    setSelected(r.ids);
    say(`Duplicated ${r.ids.length} control${r.ids.length === 1 ? "" : "s"}`);
  }

  function onCanvasKey(e: KeyboardEvent<HTMLElement>): void {
    const st = state as ReportState;
    handleCanvasKey(e, selected, {
      move: (ids, dx, dy) => update((x) => moveControls(x, ids, dx, dy)),
      resize: (ids, dw, dh) => update((x) => resizeControls(x, ids, dw, dh)),
      remove: (ids) => {
        update((x) => deleteControls(x, ids));
        say(`Deleted ${ids.size} control${ids.size === 1 ? "" : "s"}`);
      },
      duplicate: (ids) => {
        const r = duplicateControls(st, ids);
        update(() => r.state);
        select(r.ids);
      },
      selectAll: () => select(st.controls.map((c) => c.id)),
      select,
      step: (id, dir) => stepSelection(st, id, dir),
      focus: focusNode,
    });
  }

  function onDragStart(e: DragStartEvent): void {
    const data = e.active.data.current;
    if (data?.kind === "control") {
      if (!selection.has(data.id as string)) select([data.id as string]);
    } else {
      setOverlay(data?.kind === "tool" ? String(data.type) : (info.columns.find((c) => c.id === data?.id)?.name ?? "Field"));
    }
  }
  function onDragMove(e: DragMoveEvent): void {
    const data = e.active.data.current;
    if (data?.kind === "control") setDrag({ id: data.id as string, dx: snap(e.delta.x), dy: snap(e.delta.y) });
  }
  function onDragEnd(e: DragEndEvent): void {
    const data = e.active.data.current;
    setDrag(null);
    setOverlay(null);
    if (!data) return;
    if (data.kind === "control") {
      const dx = snap(e.delta.x);
      const dy = snap(e.delta.y);
      if (dx === 0 && dy === 0) return;
      const ids = selection.has(data.id as string) ? selection : new Set([data.id as string]);
      update((s) => moveControls(s, ids, dx, dy));
      return;
    }
    const target = e.over?.data.current?.band as BandId | undefined;
    const rect = e.active.rect.current.translated;
    if (!target || !rect || !e.over) return;
    const at = { x: Math.max(0, snap(rect.left - e.over.rect.left)), y: Math.max(0, snap(rect.top - e.over.rect.top)) };
    if (data.kind === "tool") addType(data.type as ControlType, target, at);
    else {
      const col = info.columns.find((c) => c.id === data.id);
      if (col) addColumn(col, target, at);
    }
  }

  function changeSource(next: ReportState): void {
    let s = next;
    if (!info.loading && s.sourceId !== "") {
      const cols = new Set(columnsOfSource(info.schema, info.queries, s.sourceKind, s.sourceId).map((c) => c.id));
      const r = dropInvalidColumns(s, cols);
      s = r.state;
      setNotice(r.dropped > 0 ? `${r.dropped} reference${r.dropped === 1 ? " was" : "s were"} removed because the new record source has no such field.` : "");
    }
    update(() => s);
  }

  function setPanel(p: ReportPanel): void {
    patch(key, (d) => ({ ...d, panel: p }));
  }

  const err = save.error instanceof ApiError ? save.error : null;
  const conflict = err?.code === "CONCURRENT_MODIFICATION";
  const errControl = err ? controlFromMessage(state, err.message) : undefined;

  function reload(): void {
    save.reset();
    const id = draft?.saved?.id;
    useReportDrafts.getState().remove(key);
    if (id) void qc.invalidateQueries({ queryKey: reportKeys.one(ws, id) });
  }

  const saveOff = save.isPending || (!dirty && draft.saved !== null);
  const panels: [ReportPanel, string][] = [
    ["layout", "Layout"],
    ["module", "Module"],
    ["preview", "Preview"],
  ];
  const width = contentWidth(state.page);

  return (
    <div data-testid="report-designer" className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <h2 className="m-0 mr-2 text-base font-semibold">{state.name || "Untitled report"}</h2>
        <div role="tablist" aria-label="Designer panels" className="flex gap-1">
          {panels.map(([p, label]) => (
            <button
              key={p}
              type="button"
              role="tab"
              aria-selected={panel === p}
              aria-controls="designer-panel"
              onClick={() => setPanel(p)}
              className={`rounded px-3 py-1 text-sm ${panel === p ? "bg-accent text-accent-fg" : "border border-border hover:bg-surface-2"}`}
            >
              {label}
            </button>
          ))}
        </div>
        <Tip label={saveOff ? "Nothing to save" : "Save the report"} shortcut={saveOff ? undefined : "Mod+S"}>
          <span className="inline-flex">
            <button type="button" className={btnPrimary} disabled={saveOff} onClick={doSave}>
              <Save size={16} aria-hidden="true" />
              Save
            </button>
          </span>
        </Tip>
        <span className="ml-auto text-fg-muted">{dirty ? "Unsaved changes" : "Saved"}</span>
      </div>
      {(err || notice) && (
        <div role="alert" className={`${alertBox} mx-3 my-2`}>
          {err ? (
            <>
              <p className="m-0 font-semibold">{err.title}</p>
              <p className="m-0">{err.message}</p>
              {err.recovery && <p className="m-0 text-fg-muted">{err.recovery}</p>}
              {errControl && (
                <button
                  type="button"
                  className={`${btn} mt-2`}
                  onClick={() => {
                    setPanel("layout");
                    select([errControl.id]);
                    focusNode(errControl.id);
                  }}
                >
                  Select {errControl.name}
                </button>
              )}
              {conflict && (
                <button type="button" className={`${btn} mt-2`} onClick={reload}>
                  Discard my edits and reload
                </button>
              )}
            </>
          ) : (
            <p className="m-0">{notice}</p>
          )}
        </div>
      )}
      <p role="status" className="sr-only">
        {announce}
      </p>
      <div id="designer-panel" role="tabpanel" aria-label={panels.find(([p]) => p === panel)?.[1]} className="flex min-h-0 flex-1 flex-col">
        {panel === "module" && (
          <div className="flex min-h-0 flex-1 flex-col overflow-auto">
            <p className="m-0 px-3 py-2 text-sm text-fg-muted">Lua helper functions defined here can be called from calculated controls and group expressions.</p>
            <LuaEditor
              value={state.moduleSource}
              onChange={(v) => update((s) => ({ ...s, moduleSource: v }))}
              kind="report_module"
              ws={ws}
              label="Lua source of report module"
              height="28rem"
            />
          </div>
        )}
        {panel === "preview" && <ReportPreview ws={ws} saved={draft.saved} dirty={dirty} saving={save.isPending} onSave={doSave} />}
        {panel === "layout" && (
          <DndContext
            sensors={sensors}
            onDragStart={onDragStart}
            onDragMove={onDragMove}
            onDragEnd={onDragEnd}
            onDragCancel={() => {
              setDrag(null);
              setOverlay(null);
            }}
          >
            <div className="grid min-h-0 flex-1 grid-cols-[14rem_minmax(0,1fr)_22rem]">
              <div className="flex min-h-0 flex-col border-r border-border">
                <Toolbox activeBand={band} onAdd={(t) => addType(t)} />
                <FieldList columns={info.columns} activeBand={band} onAdd={(c) => addColumn(c)} />
              </div>
              <div className="flex min-h-0 min-w-0 flex-col">
                <div role="toolbar" aria-label="Arrange controls" className="flex flex-wrap items-center gap-1 border-b border-border px-2 py-1">
                  <ToolbarButton icon={BoxSelect} label="Select all controls" disabled={state.controls.length === 0} disabledReason="Add a control first" shortcut="Mod+A" onClick={() => select(state.controls.map((c) => c.id))} />
                  <ToolbarButton icon={Copy} label="Duplicate selected controls" disabled={selection.size === 0} disabledReason="Select controls to duplicate" shortcut="Mod+D" onClick={duplicateSelected} />
                  <ToolbarButton icon={Trash2} label="Delete selected controls" disabled={selection.size === 0} disabledReason="Select controls to delete" shortcut="Delete" onClick={removeSelected} />
                  <span aria-hidden="true" className="mx-1 h-5 border-l border-border" />
                  {(
                    [
                      [AlignStartVertical, "left"],
                      [AlignEndVertical, "right"],
                      [AlignStartHorizontal, "top"],
                      [AlignEndHorizontal, "bottom"],
                    ] as [LucideIcon, AlignMode][]
                  ).map(([icon, mode]) => (
                    <ToolbarButton key={mode} icon={icon} label={`Align ${mode}`} disabled={selection.size < 2} disabledReason="Select two or more controls to align" onClick={() => applyOp((s, ids) => alignControls(s, ids, mode))} />
                  ))}
                  {(
                    [
                      [AlignHorizontalSpaceAround, "horizontal"],
                      [AlignVerticalSpaceAround, "vertical"],
                    ] as [LucideIcon, DistributeAxis][]
                  ).map(([icon, axis]) => (
                    <ToolbarButton key={axis} icon={icon} label={`Distribute ${axis}ly`} disabled={selection.size < 3} disabledReason="Select three or more controls to distribute" onClick={() => applyOp((s, ids) => distributeControls(s, ids, axis))} />
                  ))}
                </div>
                <p className="m-0 border-b border-border px-3 py-1 text-xs text-fg-muted">
                  Select a band with its header button; the toolbox and field list add to the selected band. Arrow keys move the selected controls 8 px; Shift+Arrow resizes; Tab selects the next control; Space adds or removes
                  the focused control from the selection; Mod+Arrow moves focus only; Delete removes. The dashed line is the page content width ({width} px).
                </p>
                <div className="min-h-0 flex-1 overflow-auto bg-surface-2 p-4 pr-6">
                  <BandCanvas
                    state={state}
                    selection={selection}
                    tabStopId={tabStopId}
                    drag={drag}
                    activeBand={band}
                    columnName={columnName}
                    onActivateBand={setActiveBand}
                    onSelect={(id, mode) => select(mode === "only" ? [id] : selection.has(id) ? selected.filter((x) => x !== id) : [...selected, id])}
                    onClear={() => setSelected([])}
                    onBandHeight={(id, h) => update((s) => setBandProps(s, id, { height: h }))}
                    onResize={(id, w, h) => update((s) => setRect(s, id, { w, h }))}
                    registerNode={(id, el) => {
                      if (el) nodes.current.set(id, el);
                      else nodes.current.delete(id);
                    }}
                    onKeyDown={onCanvasKey}
                  />
                </div>
              </div>
              <aside aria-label="Properties" className="min-h-0 overflow-y-auto border-l border-border p-3">
                {issues.length > 0 && (
                  <section aria-label="Problems" className="mb-3 rounded-md border border-danger bg-danger-soft p-2 text-sm">
                    <h3 className="m-0 mb-1 text-sm font-semibold">Problems to fix before saving</h3>
                    <ul className="m-0 pl-4">
                      {issues.map((i, n) => (
                        <li key={n}>
                          {i.message}
                          {i.controlId && (
                            <>
                              {" "}
                              <button
                                type="button"
                                className="underline"
                                onClick={() => {
                                  select([i.controlId as string]);
                                  focusNode(i.controlId as string);
                                }}
                              >
                                Select
                              </button>
                            </>
                          )}
                        </li>
                      ))}
                    </ul>
                  </section>
                )}
                {warnings.length > 0 && (
                  <section aria-label="Layout warnings" className="mb-3 rounded-md border border-border bg-surface-2 p-2 text-sm">
                    <h3 className="m-0 mb-1 text-sm font-semibold">Layout warnings</h3>
                    <ul className="m-0 pl-4">
                      {warnings.map((w, n) => (
                        <li key={n}>
                          {w.message}{" "}
                          {w.controlId && (
                            <button
                              type="button"
                              className="underline"
                              onClick={() => {
                                select([w.controlId as string]);
                                focusNode(w.controlId as string);
                              }}
                            >
                              Select
                            </button>
                          )}
                        </li>
                      ))}
                    </ul>
                  </section>
                )}
                {single ? (
                  <ControlProperties
                    key={single.id}
                    control={single}
                    state={state}
                    columns={info.columns}
                    onChange={(c: DesignControl) => update((s) => moveToBand({ ...s, controls: s.controls.map((x) => (x.id === c.id ? c : x)) }, c.id, c.band))}
                  />
                ) : selection.size > 1 ? (
                  <div>
                    <h3 className="m-0 mb-2 text-sm font-semibold">{selection.size} controls selected</h3>
                    <p className="m-0 text-sm text-fg-muted">Use the toolbar to align, distribute, duplicate or delete them. Select a single control to edit its properties.</p>
                  </div>
                ) : (
                  <>
                    <BandProperties key={band} id={band} props={state.bands[band] ?? { height: 0, can_grow: false, keep_together: false, force_new_page: false, visible: true }} onChange={(p) => update((s) => setBandProps(s, band, p))} />
                    <GroupsPanel state={state} columns={info.columns} onChange={(s) => update(() => s)} />
                    <ReportProperties state={state} catalog={catalog} onChange={(s) => update(() => s)} onSourceChange={changeSource} />
                  </>
                )}
              </aside>
            </div>
            <DragOverlay dropAnimation={null}>
              {overlay && <div className="rounded border border-accent bg-surface px-2 py-1 text-sm shadow">{overlay}</div>}
            </DragOverlay>
          </DndContext>
        )}
      </div>
    </div>
  );
}

import { KindIcon } from "@/components/objectKinds";
import { SkeletonLines, ToolbarDivider, docToolbar } from "@/components/EditorParts";
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
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
import { alertBox, badgeSuccess, badgeWarning, btn, btnPrimary, segActive, segInactive, segTray } from "@/components/styles";
import { ToolbarButton, useSaveShortcut } from "@/components/designer/toolbar";
import { Tip } from "@/components/Tip";
import { handleCanvasKey } from "@/components/designer/canvasKeys";
import { canvasMenu, controlAt } from "@/components/designer/canvasMenu";
import { ContextArea, type MenuEntry } from "@/components/ContextArea";
import { ApiError } from "@/lib/api";
import { LuaEditor } from "@/features/scripts/LuaEditor";
import { createForm, formKeys, getForm, listForms, updateForm } from "../formsApi";
import { Canvas, type DragDelta } from "./Canvas";
import { FieldList, Toolbox } from "./Palette";
import { Preview } from "./Preview";
import { ControlProperties, FormProperties, type Catalog } from "./PropertySheet";
import { draftKey, isDirty, useDesignerDrafts, type DesignerPanel } from "./designerStore";
import {
  GRID,
  addControl,
  addField,
  alignControls,
  controlFromMessage,
  deleteControls,
  distributeControls,
  dropInvalidBindings,
  duplicateControls,
  inputFromState,
  moveControls,
  moveToSection,
  resizeControls,
  scanHandlers,
  setRect,
  snap,
  snapshot,
  stateFromForm,
  stepSelection,
  validateState,
  type AlignMode,
  type ControlType,
  type DesignControl,
  type DesignState,
  type DistributeAxis,
  type SectionName,
  type SourceColumn,
} from "./layoutModel";
import { columnsOfSource, useSourceInfo } from "./sourceColumns";
import { SetupStep } from "./FormSetupStep";

/** Edits a form's layout, properties and module. formId null = new form. */
export function FormDesigner({ ws, formId, tabKey }: { ws: string; formId: string | null; tabKey: string }) {
  const key = draftKey(ws, tabKey);
  const draft = useDesignerDrafts((s) => s.entries[key]);
  if (draft) return <DesignerBody ws={ws} tabKey={tabKey} />;
  if (formId === null) return <SetupStep ws={ws} tabKey={tabKey} />;
  return <LoadForm ws={ws} formId={formId} tabKey={tabKey} />;
}

function LoadForm({ ws, formId, tabKey }: { ws: string; formId: string; tabKey: string }) {
  const q = useQuery({ queryKey: formKeys.one(ws, formId), queryFn: () => getForm(ws, formId), staleTime: 0, gcTime: 0 });
  const data = q.data;
  useEffect(() => {
    if (!data) return;
    const state = stateFromForm(data);
    useDesignerDrafts.getState().put(draftKey(ws, tabKey), { state, base: snapshot(state), saved: data, panel: "layout" });
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
    <SkeletonLines rows={6} label="Loading form…" />
  );
}

function DesignerBody({ ws, tabKey }: { ws: string; tabKey: string }) {
  const key = draftKey(ws, tabKey);
  const draft = useDesignerDrafts((s) => s.entries[key]);
  const patch = useDesignerDrafts((s) => s.patch);
  const qc = useQueryClient();
  const setDirty = useTabs((s) => s.setDirty);
  const renameTab = useTabs((s) => s.rename);
  const [selected, setSelected] = useState<string[]>([]);
  const [drag, setDrag] = useState<DragDelta | null>(null);
  const [overlay, setOverlay] = useState<string | null>(null);
  const [announce, setAnnounce] = useState("");
  const [notice, setNotice] = useState("");
  const nodes = useRef(new Map<string, HTMLElement>());
  const menuIds = useRef<string[]>([]);
  const state = draft?.state;
  const info = useSourceInfo(ws, state?.sourceKind ?? "none", state?.sourceId ?? "");
  const formsQ = useQuery({ queryKey: formKeys.list(ws), queryFn: () => listForms(ws) });

  const update = useCallback((fn: (s: DesignState) => DesignState) => patch(key, (d) => ({ ...d, state: fn(d.state) })), [patch, key]);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  const handlers = useMemo(() => scanHandlers(state?.moduleSource ?? ""), [state?.moduleSource]);
  const issues = useMemo(() => (state ? validateState(state) : []), [state]);
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
    mutationFn: (s: DesignState) => {
      const body = inputFromState(s);
      const saved = useDesignerDrafts.getState().entries[key]?.saved ?? null;
      return saved ? updateForm(ws, saved.id, saved.version, body) : createForm(ws, body);
    },
    onSuccess: (saved, s) => {
      const wasNew = useDesignerDrafts.getState().entries[key]?.saved == null;
      qc.setQueryData(formKeys.one(ws, saved.id), saved);
      void qc.invalidateQueries({ queryKey: formKeys.all(ws) });
      patch(key, (d) => ({ ...d, saved, base: snapshot(s) }));
      if (wasNew) {
        useDesignerDrafts.getState().move(key, draftKey(ws, `form:${saved.id}`));
        renameTab(ws, tabKey, saved.name, saved.id);
      } else {
        renameTab(ws, tabKey, saved.name);
      }
    },
  });

  const doSave = useCallback(() => {
    const cur = useDesignerDrafts.getState().entries[key];
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
  const catalog: Catalog = {
    schema: info.schema,
    queries: info.queries,
    forms: formsQ.data ?? [],
    columns: info.columns,
    handlers,
    selfId: draft.saved?.id ?? null,
  };
  const columnName = (id: string) => info.columns.find((c) => c.id === id)?.name ?? id;
  const single = selection.size === 1 ? state.controls.find((c) => selection.has(c.id)) : undefined;

  function select(ids: string[]): void {
    setSelected(ids);
  }
  function focusNode(id: string): void {
    requestAnimationFrame(() => nodes.current.get(id)?.focus());
  }
  function say(msg: string): void {
    setAnnounce(msg);
  }

  function addType(type: ControlType, section: SectionName = "detail", at?: { x: number; y: number }): void {
    const r = addControl(state as DesignState, type, section, at?.x, at?.y);
    update(() => r.state);
    select([r.id]);
    const c = r.state.controls.find((x) => x.id === r.id);
    say(`Added ${c ? `${c.name}` : type}`);
    focusNode(r.id);
  }

  function addColumn(col: SourceColumn, section: SectionName = "detail", at?: { x: number; y: number }): void {
    const r = addField(state as DesignState, col, section, at);
    update(() => r.state);
    select(r.ids.slice(-1));
    say(`Added ${col.name} with a caption label`);
    const last = r.ids[r.ids.length - 1];
    if (last) focusNode(last);
  }

  function applyOp(fn: (s: DesignState, ids: ReadonlySet<string>) => DesignState): void {
    update((s) => fn(s, selection));
  }

  function removeSelected(): void {
    const n = selection.size;
    applyOp(deleteControls);
    select([]);
    say(`Deleted ${n} control${n === 1 ? "" : "s"}`);
  }

  function duplicateSelected(): void {
    const r = duplicateControls(state as DesignState, selection);
    update(() => r.state);
    select(r.ids);
    say(`Duplicated ${r.ids.length} control${r.ids.length === 1 ? "" : "s"}`);
  }

  // Right-click on the canvas: the arrange actions. A control outside the selection becomes the
  // selection first; the ids are kept here because the menu is built before React re-renders.
  function onCanvasMenu(e: React.MouseEvent): void {
    const id = controlAt(e.target, nodes.current);
    menuIds.current = id && !selected.includes(id) ? [id] : selected;
    if (id && !selected.includes(id)) select([id]);
  }
  function canvasEntries(): MenuEntry[] {
    const st = state as DesignState;
    const ids = new Set(menuIds.current);
    return canvasMenu({
      selected: ids.size,
      total: st.controls.length,
      duplicate: () => {
        const r = duplicateControls(st, ids);
        update(() => r.state);
        select(r.ids);
        say(`Duplicated ${r.ids.length} control${r.ids.length === 1 ? "" : "s"}`);
      },
      remove: () => {
        update((x) => deleteControls(x, ids));
        select([]);
        say(`Deleted ${ids.size} control${ids.size === 1 ? "" : "s"}`);
      },
      align: (mode) => update((x) => alignControls(x, ids, mode)),
      selectAll: () => select(st.controls.map((c) => c.id)),
    });
  }

  function onCanvasKey(e: KeyboardEvent<HTMLElement>): void {
    const st = state as DesignState;
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
    const section = e.over?.data.current?.section as SectionName | undefined;
    const rect = e.active.rect.current.translated;
    if (!section || !rect || !e.over) return;
    const at = { x: Math.max(0, snap(rect.left - e.over.rect.left)), y: Math.max(0, snap(rect.top - e.over.rect.top)) };
    if (data.kind === "tool") addType(data.type as ControlType, section, at);
    else {
      const col = info.columns.find((c) => c.id === data.id);
      if (col) addColumn(col, section, at);
    }
  }

  function changeSource(next: DesignState): void {
    let s = next;
    if (s.sourceKind !== state?.sourceKind) s = { ...s, sourceId: "" };
    if (!info.loading) {
      const cols = new Set(columnsOfSource(info.schema, info.queries, s.sourceKind, s.sourceId).map((c) => c.id));
      const r = dropInvalidBindings(s, cols);
      s = r.state;
      setNotice(r.dropped > 0 ? `${r.dropped} control${r.dropped === 1 ? " was" : "s were"} unbound because the new record source has no such field.` : "");
    }
    update(() => s);
  }

  function setPanel(p: DesignerPanel): void {
    patch(key, (d) => ({ ...d, panel: p }));
  }

  const err = save.error instanceof ApiError ? save.error : null;
  const conflict = err?.code === "CONCURRENT_MODIFICATION";
  const errControl = err ? controlFromMessage(state, err.message) : undefined;

  function reload(): void {
    save.reset();
    const id = draft?.saved?.id;
    useDesignerDrafts.getState().remove(key);
    if (id) void qc.invalidateQueries({ queryKey: formKeys.one(ws, id) });
  }

  const saveOff = save.isPending || (!dirty && draft.saved !== null);
  const panels: [DesignerPanel, string][] = [
    ["layout", "Layout"],
    ["module", "Module"],
    ["preview", "Preview"],
  ];

  return (
    <div data-testid="form-designer" className="flex min-h-0 flex-1 flex-col">
      <div className={docToolbar}>
        <KindIcon kind="form" size="sm" />
        <h2 className="m-0 mr-2 text-sm font-semibold">{state.name || "Untitled form"}</h2>
        <div role="tablist" aria-label="Designer panels" className={segTray}>
          {panels.map(([p, label]) => (
            <button
              key={p}
              type="button"
              role="tab"
              aria-selected={panel === p}
              aria-controls="designer-panel"
              onClick={() => setPanel(p)}
              className={`h-7 cursor-pointer rounded-md px-3 text-sm font-medium transition-colors ${panel === p ? segActive : segInactive}`}
            >
              {label}
            </button>
          ))}
        </div>
        <Tip label={saveOff ? "Nothing to save" : "Save the form"} shortcut={saveOff ? undefined : "Mod+S"}>
          <span className="inline-flex">
            <button type="button" className={btnPrimary} disabled={saveOff} onClick={doSave}>
              <Save size={16} aria-hidden="true" />
              Save
            </button>
          </span>
        </Tip>
        <span className={`ml-auto ${dirty ? badgeWarning : badgeSuccess}`}>{dirty ? "Unsaved changes" : "Saved"}</span>
      </div>
      {(err || notice) && (
        <div role="alert" className={`${alertBox} mx-3 my-2`}>
          {err ? (
            <>
              <p className="m-0 font-semibold">{err.title}</p>
              <p className="m-0">{err.message}</p>
              {err.recovery && <p className="m-0 text-fg-muted">{err.recovery}</p>}
              {errControl && (
                <button type="button" className={`${btn} mt-2`} onClick={() => { setPanel("layout"); select([errControl.id]); focusNode(errControl.id); }}>
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
            <p className="m-0 border-b border-border bg-surface px-3 py-2 text-sm text-fg-muted">
              Event handlers are functions in this module.{" "}
              {handlers.length > 0 ? `Functions found: ${handlers.join(", ")}.` : "No functions found yet."}
            </p>
            <LuaEditor
              value={state.moduleSource}
              onChange={(v) => update((s) => ({ ...s, moduleSource: v }))}
              kind="form_module"
              ws={ws}
              label="Lua source of form module"
              height="28rem"
            />
          </div>
        )}
        {panel === "preview" && <Preview ws={ws} state={state} queries={info.queries} columns={info.columns} />}
        {panel === "layout" && (
          <DndContext sensors={sensors} onDragStart={onDragStart} onDragMove={onDragMove} onDragEnd={onDragEnd} onDragCancel={() => { setDrag(null); setOverlay(null); }}>
            <div className="grid min-h-0 flex-1 grid-cols-[12rem_minmax(0,1fr)_17rem] 2xl:grid-cols-[14rem_minmax(0,1fr)_21rem]">
              <div className="flex min-h-0 flex-col border-r border-border bg-surface">
                <Toolbox onAdd={(t) => addType(t)} />
                <FieldList columns={info.columns} hasSource={state.sourceKind !== "none"} onAdd={(c) => addColumn(c)} />
              </div>
              <div className="flex min-h-0 min-w-0 flex-col">
                <div role="toolbar" aria-label="Arrange controls" className="flex flex-wrap items-center gap-1 border-b border-border bg-surface px-2 py-1">
                  <ToolbarButton icon={BoxSelect} label="Select all controls" disabled={state.controls.length === 0} disabledReason="Add a control first" shortcut="Mod+A" onClick={() => select(state.controls.map((c) => c.id))} />
                  <ToolbarButton icon={Copy} label="Duplicate selected controls" disabled={selection.size === 0} disabledReason="Select controls to duplicate" shortcut="Mod+D" onClick={duplicateSelected} />
                  <ToolbarButton icon={Trash2} label="Delete selected controls" disabled={selection.size === 0} disabledReason="Select controls to delete" shortcut="Delete" onClick={removeSelected} />
                  <ToolbarDivider />
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
                <p className="m-0 border-b border-border bg-surface-2/50 px-3 py-1 text-xs text-fg-muted">
                  Arrow keys move the selected controls 8 px; Shift+Arrow resizes; Tab selects the next control; Space adds or removes the focused control from the selection; Mod+Arrow moves focus only; Delete removes.
                </p>
                <ContextArea label="Arrange controls" onOpen={onCanvasMenu} entries={canvasEntries}>
                <div className="min-h-0 flex-1 overflow-auto bg-bg p-4 pr-6">
                  <Canvas
                    state={state}
                    selection={selection}
                    tabStopId={tabStopId}
                    drag={drag}
                    columnName={columnName}
                    onSelect={(id, mode) => select(mode === "only" ? [id] : selection.has(id) ? selected.filter((x) => x !== id) : [...selected, id])}
                    onClear={() => select([])}
                    onSectionHeight={(name, h) => update((s) => ({ ...s, heights: { ...s.heights, [name]: h } }))}
                    onWidth={(w) => update((s) => ({ ...s, width: Math.max(GRID, w) }))}
                    onResize={(id, w, h) => update((s) => setRect(s, id, { w, h }))}
                    registerNode={(id, el) => {
                      if (el) nodes.current.set(id, el);
                      else nodes.current.delete(id);
                    }}
                    onKeyDown={onCanvasKey}
                  />
                </div>
                </ContextArea>
              </div>
              <aside aria-label="Properties" className="min-h-0 overflow-y-auto border-l border-border bg-surface p-3">
                {issues.length > 0 && (
                  <section aria-label="Problems" className="mb-3 rounded-lg border border-danger/40 bg-danger-soft p-3 text-sm">
                    <h3 className="m-0 mb-1 text-sm font-semibold">Problems to fix before saving</h3>
                    <ul className="m-0 pl-4">
                      {issues.map((i, n) => (
                        <li key={n}>
                          {i.message}
                          {i.controlId && (
                            <>
                              {" "}
                              <button type="button" className="underline" onClick={() => { select([i.controlId as string]); focusNode(i.controlId as string); }}>
                                Select
                              </button>
                            </>
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
                    catalog={catalog}
                    onChange={(c: DesignControl) =>
                      update((s) => moveToSection({ ...s, controls: s.controls.map((x) => (x.id === c.id ? c : x)) }, c.id, c.section))
                    }
                  />
                ) : selection.size > 1 ? (
                  <div>
                    <h3 className="m-0 mb-2 text-sm font-semibold">{selection.size} controls selected</h3>
                    <p className="m-0 text-sm text-fg-muted">Use the toolbar to align, distribute, duplicate or delete them. Select a single control to edit its properties.</p>
                  </div>
                ) : (
                  <FormProperties state={state} catalog={catalog} onChange={(s) => update(() => s)} onSourceChange={changeSource} />
                )}
              </aside>
            </div>
            <DragOverlay dropAnimation={null}>
              {overlay && <div className="rounded-md border border-accent bg-surface px-2 py-1 text-sm shadow-md">{overlay}</div>}
            </DragOverlay>
          </DndContext>
        )}
      </div>
    </div>
  );
}

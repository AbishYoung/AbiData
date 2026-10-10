import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { createStore, type StoreApi } from "zustand/vanilla";
import { ApiError } from "@/lib/api";
import { keys } from "@/lib/events";
import { useErrorStore } from "@/lib/errorStore";
import type { Table } from "@/lib/schema";
import { classifyFailure, type Row } from "@/features/datasheet/rowsApi";
import { valuesEqual } from "@/features/datasheet/values";
import { formDeleteRecord, formEvent, formSave, type Form, type FormControl, type FormEventRequest, type FormEventResponse } from "../formsApi";
import { createDebouncer } from "./debounce";
import { useFormEnv, type ParentLink } from "./env";
import {
  applyCalc,
  applyPatches,
  bufferForServer,
  bufferOf,
  dirtyFields,
  emptyCalc,
  indexAfterDelete,
  isDirty,
  layoutControls,
  mergeAssigned,
  newRec,
  overridesPayload,
  planResponse,
  recFromRow,
  resolveNav,
  revert,
  saveFields,
  setField,
  strictEqual,
  type CalcState,
  type Effects,
  type Equal,
  type NavTarget,
  type Overrides,
  type RecState,
} from "./model";
import { formQueryKey, lookupKeys, type Link, type PageAccess } from "./sources";

export interface SessionState {
  index: number;
  rec: RecState | null;
  overrides: Overrides;
  calc: CalcState;
  calcBase: CalcState;
  message: string | null;
  conflict: Row | null;
  fieldErrors: Record<string, string>;
  confirm: { message: string; continuation: string; control: string } | null;
  busy: boolean;
  focus: { name: string; n: number } | null;
  /** Waiting for the row at `index` to arrive before opening it. */
  wantOpen: boolean;
  /** A just-deleted record id that must not be reopened from stale cache. */
  skipId: number | null;
  initialized: boolean;
}

export interface SessionOptions {
  form: Form;
  /** Table that records are written to (undefined: read-only source). */
  table: Table | undefined;
  /** True for the "none" source. */
  unbound: boolean;
  count: number;
  ready: boolean;
  access: PageAccess;
  canEdit: boolean;
  canAdd: boolean;
  canDelete: boolean;
  link: Link | null;
  parent: ParentLink | null;
  /** Flushes the subforms embedded in this form. */
  flushChildren: () => Promise<boolean>;
  /** Refetches the record source. */
  refetch: () => void;
}

const initial: SessionState = {
  index: 0,
  rec: null,
  overrides: {},
  calc: emptyCalc,
  calcBase: emptyCalc,
  message: null,
  conflict: null,
  fieldErrors: {},
  confirm: null,
  busy: false,
  focus: null,
  wantOpen: false,
  skipId: null,
  initialized: false,
};

function showError(e: unknown): void {
  if (e instanceof ApiError) {
    if (!e.canceled) useErrorStore.getState().show(e);
    return;
  }
  throw e;
}

export interface Session {
  store: StoreApi<SessionState>;
  eq: Equal;
  moveTo: (t: NavTarget) => Promise<boolean>;
  /** Goes to a position (the count is the new-record row). */
  goTo: (index: number) => Promise<boolean>;
  save: () => Promise<boolean>;
  leave: () => Promise<boolean>;
  cancelEdit: () => void;
  deleteRecord: () => Promise<void>;
  commitField: (control: FormControl, colId: string, value: unknown, display?: string) => Promise<boolean>;
  notifyChange: (control: FormControl, colId: string, value: unknown) => void;
  click: (control: FormControl) => Promise<void>;
  answerConfirm: (answer: boolean) => Promise<void>;
  refreshCalc: () => Promise<void>;
  dismissMessage: () => void;
  keepMine: () => void;
  discardMine: () => void;
  calcFor: (rec: RecState) => Promise<FormEventResponse | null>;
}

/** The record session of one form instance: current record, events, save, navigation. */
export function createSessionStore(): StoreApi<SessionState> {
  return createStore<SessionState>(() => initial);
}

export function useSessionState<T>(store: StoreApi<SessionState>, pick: (s: SessionState) => T): T {
  return useSyncExternalStore(store.subscribe, () => pick(store.getState()));
}

export function useFormSession(store: StoreApi<SessionState>, opts: SessionOptions): Session {
  const env = useFormEnv();
  const qc = useQueryClient();
  const [debouncer] = useState(() => createDebouncer(300));
  const o = useRef(opts);
  useEffect(() => {
    o.current = opts;
  });
  const epoch = useRef(0);
  const inflight = useRef(new Set<AbortController>());
  const focusN = useRef(0);
  const pendingCommits = useRef(new Set<Promise<boolean>>());

  const leaveRef = useRef<() => Promise<boolean>>(() => Promise.resolve(true));
  const get = store.getState;
  const set = store.setState;

  const eq: Equal = (colId, a, b) => {
    const c = o.current.table?.columns?.find((x) => x.id === colId);
    return c ? valuesEqual(c, a, b) : strictEqual(colId, a, b);
  };
  const known = (): ((id: string) => boolean) | undefined => {
    const t = o.current.table;
    if (!t) return undefined;
    const ids = new Set((t.columns ?? []).map((c) => c.id));
    return (id) => ids.has(id);
  };
  const calcIds = (): string[] => layoutControls(o.current.form.layout).filter((c) => c.calc).map((c) => c.id);

  function abortInFlight(): void {
    epoch.current++;
    for (const ac of inflight.current) ac.abort();
    inflight.current.clear();
    debouncer.cancelAll();
  }

  function invalidateSource(): void {
    const { form, table } = o.current;
    if (table) {
      void qc.invalidateQueries({ queryKey: keys.rows(env.ws, table.id) });
      void qc.invalidateQueries({ queryKey: keys.count(env.ws, table.id) });
    }
    if (form.source_kind === "query" && form.source_id) void qc.invalidateQueries({ queryKey: formQueryKey(env.ws, form.source_id) });
  }

  function buildEvent(event: string, extra: Partial<FormEventRequest> = {}, rec?: RecState): FormEventRequest {
    const st = get();
    const r = rec ?? st.rec ?? newRec();
    const req: FormEventRequest = {
      event,
      record: bufferForServer(r, known()),
      is_new: r.isNew,
      dirty_fields: dirtyFields(r),
    };
    if (r.id !== null) req.record_id = r.id;
    if (r.version !== null) req.record_version = r.version;
    const ov = overridesPayload(st.overrides);
    if (ov) req.controls = ov;
    return { ...req, ...extra };
  }

  async function dispatch(req: FormEventRequest): Promise<FormEventResponse | null> {
    const mine = epoch.current;
    const ac = new AbortController();
    inflight.current.add(ac);
    try {
      const resp = await formEvent(env.ws, o.current.form.id, req, ac.signal);
      return mine === epoch.current ? resp : null;
    } catch (e) {
      if (mine === epoch.current) showError(e);
      return null;
    } finally {
      inflight.current.delete(ac);
    }
  }

  function applyResponse(resp: FormEventResponse, skipRecord = false): Effects {
    set((s) => {
      const calc = applyCalc(s.calc, resp, calcIds());
      const rec = s.rec && !skipRecord ? mergeAssigned(s.rec, resp.record, eq) : s.rec;
      return {
        overrides: applyPatches(s.overrides, resp.patches),
        calc,
        calcBase: rec && !isDirty(rec) ? calc : s.calcBase,
        rec,
      };
    });
    const fx = planResponse(resp);
    for (const t of fx.toasts) env.toast(t);
    for (const a of fx.alerts) env.alert(a);
    if (fx.cancelMessage !== null) set({ message: fx.cancelMessage });
    if (fx.confirm) set({ confirm: { ...fx.confirm, control: "" } });
    if (fx.focus) set({ focus: { name: fx.focus, n: ++focusN.current } });
    if (fx.requery.length > 0) {
      invalidateSource();
      void qc.invalidateQueries({ queryKey: lookupKeys.all(env.ws) });
    }
    if (fx.openForm) env.openForm(fx.openForm);
    if (fx.close) env.closeTab();
    if (fx.record !== null) void moveTo(fx.record);
    return fx;
  }

  // ---- opening records ------------------------------------------------------------------

  function openRow(index: number, row: Row): void {
    abortInFlight();
    set({ index, rec: recFromRow(row), message: null, conflict: null, fieldErrors: {}, wantOpen: false, skipId: null, calc: emptyCalc, calcBase: emptyCalc });
    void dispatch(buildEvent("current")).then((resp) => resp && applyResponse(resp));
  }

  function startNew(): void {
    abortInFlight();
    const { link, count } = o.current;
    const defaults = link ? { [link.columnId]: link.value } : {};
    set({ index: count, rec: newRec(defaults), message: null, conflict: null, fieldErrors: {}, wantOpen: false, skipId: null, calc: emptyCalc, calcBase: emptyCalc });
    void dispatch(buildEvent("current")).then((resp) => resp && applyResponse(resp));
  }

  /** Saves the current record and any subform edits; false when the user has to stay. */
  async function leave(): Promise<boolean> {
    await Promise.allSettled([...pendingCommits.current]);
    if (!(await o.current.flushChildren())) return false;
    return save();
  }

  async function moveTo(target: NavTarget): Promise<boolean> {
    const { count, canAdd, access } = o.current;
    const st = get();
    const r = resolveNav(target, { index: st.index, count, canAdd, indexOfId: access.indexOfId });
    if (r === null) return false;
    return goToResolved(r);
  }

  async function goTo(index: number): Promise<boolean> {
    const { count, canAdd } = o.current;
    if (index >= count) return canAdd ? goToResolved("new") : false;
    return goToResolved(index);
  }

  async function goToResolved(r: number | "new"): Promise<boolean> {
    const st = get();
    if (r !== "new" && r === st.index && st.rec && !st.rec.isNew) return true;
    if (r === "new" && st.rec?.isNew) return true;
    if (!(await leave())) return false;
    if (r === "new") {
      startNew();
      return true;
    }
    const row = o.current.access.rowAt(r);
    if (row && row.id !== get().skipId) openRow(r, row);
    else set({ index: r, wantOpen: true });
    return true;
  }

  // ---- save / revert / delete --------------------------------------------------------------

  async function save(): Promise<boolean> {
    const st = get();
    const rec = st.rec;
    if (!rec || !isDirty(rec)) return true;
    const { form, canEdit, canAdd, table } = o.current;
    if (!table || (rec.isNew ? !canAdd : !canEdit)) return false;
    if (st.busy) return false;
    abortInFlight();
    set({ busy: true, message: null, conflict: null, fieldErrors: {} });
    let resp: FormEventResponse;
    try {
      resp = await formSave(env.ws, form.id, {
        ...(rec.id !== null ? { record_id: rec.id } : {}),
        ...(rec.version !== null ? { record_version: rec.version } : {}),
        is_new: rec.isNew,
        fields: saveFields(rec),
      });
    } catch (e) {
      set({ busy: false });
      const f = classifyFailure(e);
      if (f.kind === "conflict") set({ conflict: f.current });
      else if (f.kind === "fields") {
        set({ fieldErrors: f.errors });
        const first = Object.keys(f.errors)[0];
        const ctl = first ? allControls().find((c) => c.bind?.column_id === first) : undefined;
        if (ctl) set({ focus: { name: ctl.name, n: ++focusN.current } });
      } else if (f.kind === "error") showError(f.error);
      return false;
    }
    set({ busy: false });
    const cancelled = resp.cancel !== undefined && resp.cancel !== null;
    applyResponse(resp, true);
    if (cancelled || !resp.saved) return !cancelled;
    const saved = resp.saved;
    set((s) => ({
      rec: { ...recFromRow(saved), display: { ...(s.rec?.display ?? {}), ...(saved.display ?? {}) } },
    }));
    set((s) => ({ calcBase: s.calc }));
    invalidateSource();
    o.current.parent?.changed();
    return true;
  }

  function allControls(): FormControl[] {
    return layoutControls(o.current.form.layout);
  }

  function cancelEdit(): void {
    abortInFlight();
    set((s) => ({ rec: s.rec ? revert(s.rec) : s.rec, calc: s.calcBase, message: null, conflict: null, fieldErrors: {} }));
  }

  function keepMine(): void {
    const { conflict, rec } = get();
    if (!conflict || !rec) return;
    set({ rec: { ...rec, version: conflict._version, base: conflict.fields }, conflict: null });
  }

  function discardMine(): void {
    const { conflict } = get();
    if (!conflict) return;
    set({ rec: recFromRow(conflict), conflict: null, fieldErrors: {} });
    invalidateSource();
  }

  async function deleteRecord(): Promise<void> {
    const { rec, index } = get();
    const { form, canDelete, count } = o.current;
    if (!rec || rec.id === null || rec.version === null || !canDelete) return;
    abortInFlight();
    set({ busy: true });
    try {
      const resp = await formDeleteRecord(env.ws, form.id, rec.id, rec.version);
      set({ busy: false });
      applyResponse(resp, true);
      if (resp.cancel) return;
      set({ rec: null, wantOpen: true, index: indexAfterDelete(index, count), skipId: rec.id, calc: emptyCalc, calcBase: emptyCalc });
      invalidateSource();
      o.current.parent?.changed();
    } catch (e) {
      set({ busy: false });
      const f = classifyFailure(e);
      if (f.kind === "conflict") set({ conflict: f.current });
      else if (f.kind === "error") showError(f.error);
    }
  }

  // ---- controls -----------------------------------------------------------------------------

  function commitField(control: FormControl, colId: string, value: unknown, display?: string): Promise<boolean> {
    const p = commitFieldNow(control, colId, value, display);
    pendingCommits.current.add(p);
    void p.finally(() => pendingCommits.current.delete(p));
    return p;
  }

  async function commitFieldNow(control: FormControl, colId: string, value: unknown, display?: string): Promise<boolean> {
    const rec = get().rec;
    if (!rec) return false;
    const old = bufferOf(rec)[colId];
    if (eq(colId, value, old)) return true;
    debouncer.cancel(control.name);
    if (control.events?.before_update) {
      const resp = await dispatch(buildEvent("before_update", { control: control.name, old_value: old ?? null, new_value: value ?? null }));
      if (!resp) return false;
      applyResponse(resp);
      if (resp.cancel) return false;
    }
    set((s) => {
      const fieldErrors = Object.fromEntries(Object.entries(s.fieldErrors).filter(([k]) => k !== colId));
      return { rec: s.rec ? setField(s.rec, colId, value, eq, display) : s.rec, fieldErrors, message: null };
    });
    if (control.events?.after_update || calcIds().length > 0) {
      const resp = await dispatch(buildEvent("after_update", { control: control.name, old_value: old ?? null, new_value: value ?? null }));
      if (resp) applyResponse(resp);
    }
    return true;
  }

  function notifyChange(control: FormControl, colId: string, value: unknown): void {
    if (!control.events?.change) return;
    debouncer.schedule(control.name, () => {
      const rec = get().rec;
      if (!rec) return;
      const old = bufferOf(rec)[colId];
      const req = buildEvent("change", { control: control.name, old_value: old ?? null, new_value: value ?? null });
      req.record = { ...(req.record ?? {}), [colId]: value };
      void dispatch(req).then((resp) => resp && applyResponse(resp));
    });
  }

  async function click(control: FormControl): Promise<void> {
    if (!control.events?.click) return;
    const resp = await dispatch(buildEvent("click", { control: control.name }));
    if (!resp) return;
    const fx = applyResponse(resp);
    if (fx.confirm) set({ confirm: { ...fx.confirm, control: control.name } });
  }

  async function answerConfirm(answer: boolean): Promise<void> {
    const c = get().confirm;
    if (!c) return;
    set({ confirm: null });
    const resp = await dispatch(buildEvent("confirm", { continuation: c.continuation, answer, ...(c.control ? { control: c.control } : {}) }));
    if (resp) applyResponse(resp);
  }

  async function refreshCalc(): Promise<void> {
    if (!get().rec) return;
    const resp = await dispatch(buildEvent("current"));
    if (resp) applyResponse(resp);
  }

  /** Calculated values for a record that is not current (continuous and datasheet rows). */
  async function calcFor(rec: RecState): Promise<FormEventResponse | null> {
    try {
      return await formEvent(env.ws, o.current.form.id, buildEvent("current", {}, rec));
    } catch (e) {
      showError(e);
      return null;
    }
  }

  useEffect(() => {
    leaveRef.current = leave;
  });

  // ---- lifecycle effects ----------------------------------------------------------------------

  useEffect(() => {
    const d = debouncer;
    const inflightSet = inflight.current;
    return () => {
      d.cancelAll();
      for (const ac of inflightSet) ac.abort();
    };
  }, [debouncer]);

  // First record (or a blank one), then the load event.
  useEffect(() => {
    const st = get();
    if (st.initialized || !opts.ready) return;
    if (opts.unbound) {
      set({ initialized: true, rec: newRec(), index: 0 });
      void dispatch(buildEvent("load")).then((r) => r && applyResponse(r));
      return;
    }
    if (opts.count > 0) {
      const row = opts.access.rowAt(0);
      if (!row) return;
      set({ initialized: true });
      openRow(0, row);
      void dispatch(buildEvent("load", {}, recFromRow(row))).then((r) => r && applyResponse(r));
    } else if (opts.canAdd && opts.table) {
      set({ initialized: true });
      startNew();
      void dispatch(buildEvent("load", {}, newRec())).then((r) => r && applyResponse(r));
    } else {
      set({ initialized: true });
    }
  });

  // A pending move/delete completes when the wanted row arrives; an emptied source opens a blank record.
  useEffect(() => {
    const st = get();
    if (!st.wantOpen || !opts.ready) return;
    if (opts.count === 0) {
      if (opts.canAdd && opts.table && st.skipId === null) startNew();
      else if (st.skipId !== null && opts.canAdd && opts.table) {
        // Wait for the refetch to confirm that the source is empty.
      } else set({ wantOpen: false, rec: null });
      return;
    }
    const row = opts.access.rowAt(Math.min(st.index, opts.count - 1));
    if (row && row.id !== st.skipId) openRow(Math.min(st.index, opts.count - 1), row);
  });

  // Live updates: follow the record when the source changes underneath a clean buffer.
  useEffect(() => {
    const st = get();
    const rec = st.rec;
    if (!rec || rec.id === null || rec.isNew || isDirty(rec) || st.busy || st.wantOpen) return;
    const at = opts.access.indexOfId(rec.id);
    if (at < 0) return;
    if (at !== st.index) set({ index: at });
    const row = opts.access.rowAt(at);
    if (row && row._version > (rec.version ?? 0)) set({ rec: recFromRow(row) });
  });

  // Report dirtiness and register for flushing with the master form.
  const dirty = useSessionState(store, (st) => (st.rec ? isDirty(st.rec) : false));
  const parent = opts.parent;
  useEffect(() => {
    parent?.setDirty(dirty);
  }, [dirty, parent]);
  useEffect(() => {
    if (!parent) return;
    return parent.register(() => leaveRef.current());
  }, [parent]);

  return {
    store, eq, moveTo, goTo, save, leave, cancelEdit, deleteRecord, commitField, notifyChange, click, answerConfirm, refreshCalc,
    dismissMessage: () => set({ message: null }),
    keepMine, discardMine, calcFor,
  };
}


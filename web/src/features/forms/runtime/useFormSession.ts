import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { createStore, type StoreApi } from "zustand/vanilla";
import { ApiError } from "@/lib/api";
import { keys } from "@/lib/events";
import { useErrorStore } from "@/lib/errorStore";
import type { Table } from "@/lib/schema";
import { classifyFailure, type Row } from "@/features/datasheet/rowsApi";
import { isReadOnlyColumn, valuesEqual } from "@/features/datasheet/values";
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
  afterSave,
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
import { rebase, takeTheirs } from "./merge3";
import { asNewRecord, deletedNotice, deleterName, isNotFound, rowExists } from "./gone";

/** How often a save is retried on its own after merging a concurrent change without conflicts. */
export const MAX_AUTO_RETRIES = 3;

export interface SessionState {
  index: number;
  rec: RecState | null;
  overrides: Overrides;
  calc: CalcState;
  calcBase: CalcState;
  message: string | null;
  conflict: Row | null;
  /** The fields both users changed differently (the rest was merged). */
  conflictFields: string[];
  /** The open record was deleted by someone else while it had unsaved edits. */
  deleted: boolean;
  /** Inline notice after the open record was deleted and the form moved on. */
  notice: string | null;
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
  conflictFields: [],
  deleted: false,
  notice: null,
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
  /** Text typed into a box but not committed yet; undefined clears it. */
  noteTyping: (colId: string, value: unknown) => void;
  notifyChange: (control: FormControl, colId: string, value: unknown) => void;
  click: (control: FormControl) => Promise<void>;
  answerConfirm: (answer: boolean) => Promise<void>;
  refreshCalc: () => Promise<void>;
  dismissMessage: () => void;
  keepMine: () => void;
  discardMine: () => void;
  /** After the open record was deleted: keep the edits as a new record and save it. */
  saveAsNew: () => Promise<boolean>;
  /** After the open record was deleted: drop the edits and move to the nearest record. */
  discardDeleted: () => void;
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
  // Text being typed into a box that has not committed yet (commit happens on blur), keyed by
  // column, for the record it was typed into. If the record is deleted meanwhile, the typing
  // counts as an unsaved edit instead of being lost.
  const typing = useRef<{ recId: number | null; values: Map<string, unknown> }>({ recId: null, values: new Map() });

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
      if (mine === epoch.current) {
        const cur = get().rec;
        if (isNotFound(e) && cur && cur.id !== null && !cur.isNew && o.current.table) markGone();
        else showError(e);
      }
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
    set({ index, rec: recFromRow(row), message: null, conflict: null, conflictFields: [], deleted: false, fieldErrors: {}, wantOpen: false, skipId: null, calc: emptyCalc, calcBase: emptyCalc });
    void dispatch(buildEvent("current")).then((resp) => resp && applyResponse(resp));
  }

  function startNew(): void {
    abortInFlight();
    const { link, count } = o.current;
    const defaults = link ? { [link.columnId]: link.value } : {};
    set({ index: count, rec: newRec(defaults), message: null, conflict: null, conflictFields: [], deleted: false, fieldErrors: {}, wantOpen: false, skipId: null, calc: emptyCalc, calcBase: emptyCalc });
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
    if (get().notice !== null) set({ notice: null });
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

  async function save(attempt = 0): Promise<boolean> {
    const st = get();
    const rec = st.rec;
    if (!rec || !isDirty(rec)) return true;
    // The record is gone: it can only be saved as a new record ("Save as new record").
    if (st.deleted) return false;
    // A conflict is open: the user decides first.
    if (st.conflict) return false;
    const { form, canEdit, canAdd, table } = o.current;
    if (!table || (rec.isNew ? !canAdd : !canEdit)) return false;
    if (st.busy) return false;
    abortInFlight();
    set({ busy: true, message: null, conflict: null, conflictFields: [], fieldErrors: {}, notice: null });
    let resp: FormEventResponse;
    const sent = saveFields(rec);
    try {
      resp = await formSave(env.ws, form.id, {
        ...(rec.id !== null ? { record_id: rec.id } : {}),
        ...(rec.version !== null ? { record_version: rec.version } : {}),
        is_new: rec.isNew,
        fields: sent,
      });
    } catch (e) {
      set({ busy: false });
      const f = classifyFailure(e);
      if (f.kind === "conflict") {
        // Merge field by field: only fields both users changed need a decision.
        const cur = get().rec;
        if (!cur || cur.id !== rec.id) return false;
        const merged = rebase(cur, f.current, eq);
        set({ rec: merged.rec });
        if (merged.conflicts.length === 0 && attempt < MAX_AUTO_RETRIES) return isDirty(merged.rec) ? save(attempt + 1) : true;
        set({ conflict: f.current, conflictFields: merged.conflicts });
      } else if (!rec.isNew && isNotFound(e)) {
        markGone();
      } else if (f.kind === "fields") {
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
    // Fields the user changed while the save was running stay as unsaved edits.
    set((s) => {
      const next = afterSave(s.rec, sent, saved, eq);
      return { rec: { ...next, display: { ...(s.rec?.display ?? {}), ...(saved.display ?? {}), ...next.display } } };
    });
    set((s) => ({ calcBase: s.calc }));
    invalidateSource();
    o.current.parent?.changed();
    return true;
  }

  function allControls(): FormControl[] {
    return layoutControls(o.current.form.layout);
  }

  function cancelEdit(): void {
    if (get().deleted) {
      discardDeleted();
      return;
    }
    abortInFlight();
    set((s) => ({ rec: s.rec ? revert(s.rec) : s.rec, calc: s.calcBase, message: null, conflict: null, conflictFields: [], fieldErrors: {} }));
  }

  /** Keeps the user's values for the conflicting fields; everything else is already merged. */
  function keepMine(): void {
    if (!get().conflict) return;
    set({ conflict: null, conflictFields: [] });
  }

  /** Takes the other user's values for the conflicting fields; the user's other edits stay. */
  function discardMine(): void {
    const { conflict, conflictFields, rec } = get();
    if (!conflict || !rec) return;
    const next = conflictFields.length > 0 ? takeTheirs(rec, conflictFields) : recFromRow(conflict);
    const fresh = conflictFields.length > 0 ? { ...next, display: { ...rec.display, ...(conflict.display ?? {}) } } : next;
    set({ rec: fresh, conflict: null, conflictFields: [], fieldErrors: {} });
    invalidateSource();
  }

  // ---- the open record was deleted by someone else ---------------------------------------------

  /**
   * Called when the open record turns out to be gone (live update, NOT_FOUND on save or on an event).
   * A clean buffer moves to the nearest record with a notice; unsaved edits stay on screen behind a banner.
   */
  function markGone(): void {
    const st = get();
    let rec = st.rec;
    if (!rec || rec.id === null || rec.isNew || st.deleted) return;
    const { table } = o.current;
    abortInFlight();
    const id = rec.id;
    if (typing.current.recId === id) {
      for (const [colId, value] of typing.current.values) {
        if (!eq(colId, value, bufferOf(rec)[colId])) rec = setField(rec, colId, value, eq);
      }
    }
    typing.current = { recId: null, values: new Map() };
    if (isDirty(rec)) {
      set({ rec, deleted: true, busy: false, conflict: null, conflictFields: [], message: null, fieldErrors: {}, confirm: null });
    } else {
      const generic = deletedNotice(null);
      set({ rec: null, wantOpen: true, skipId: id, notice: generic, busy: false, conflict: null, conflictFields: [], deleted: false, calc: emptyCalc, calcBase: emptyCalc });
      if (table) {
        void deleterName(env.ws, table.id, id).then((name) => {
          if (name) set((s) => (s.notice === generic ? { notice: deletedNotice(name) } : {}));
        });
      }
    }
    invalidateSource();
    o.current.parent?.changed();
  }

  function discardDeleted(): void {
    const { rec, deleted } = get();
    if (!deleted || !rec || rec.id === null) return;
    set({ rec: null, wantOpen: true, skipId: rec.id, deleted: false, notice: null, fieldErrors: {}, calc: emptyCalc, calcBase: emptyCalc });
    invalidateSource();
  }

  async function saveAsNew(): Promise<boolean> {
    const st = get();
    const { rec, deleted } = st;
    const { canAdd, table, link, count } = o.current;
    if (!deleted || !rec || !canAdd || !table || st.busy) return false;
    const cols = known();
    const byId = new Map((table.columns ?? []).map((c) => [c.id, c]));
    const writable = (id: string) => {
      const c = byId.get(id);
      return !!cols?.(id) && !!c && !isReadOnlyColumn(c);
    };
    const defaults = link ? { [link.columnId]: link.value } : {};
    set({ rec: asNewRecord(rec, writable, defaults), deleted: false, index: count, conflict: null, conflictFields: [], fieldErrors: {}, message: null });
    return save();
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
      if (isNotFound(e)) markGone();
      else if (f.kind === "conflict") {
        const cur = get().rec;
        if (!cur) return;
        const merged = rebase(cur, f.current, eq);
        set({ rec: merged.rec });
        if (merged.theirsChanged || merged.conflicts.length > 0) set({ conflict: f.current, conflictFields: merged.conflicts });
      } else if (f.kind === "error") showError(f.error);
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

  /** A text box reports what is typed but not committed yet; undefined clears it. */
  function noteTyping(colId: string, value: unknown): void {
    const recId = get().rec?.id ?? null;
    if (typing.current.recId !== recId) typing.current = { recId, values: new Map() };
    if (value === undefined) typing.current.values.delete(colId);
    else typing.current.values.set(colId, value);
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
    const want = Math.min(st.index, opts.count - 1);
    // Pages are loaded around `index`: bring it inside the list so the wanted row can arrive.
    if (want !== st.index) set({ index: want });
    const row = opts.access.rowAt(want);
    if (row && row.id !== st.skipId) openRow(want, row);
  });

  // Live updates: follow the open record when the source changes underneath it (inserts and deletes
  // of other records move it, a newer version is merged field by field, a vanished record is handled).
  const goneChecked = useRef("");
  useEffect(() => {
    const st = get();
    const rec = st.rec;
    if (!rec || rec.id === null || rec.isNew || st.busy || st.wantOpen || st.deleted) return;
    const at = opts.access.indexOfId(rec.id);
    if (at < 0) {
      // Not in the loaded pages: confirm with the server before treating it as deleted.
      const table = opts.table;
      const here = opts.access.rowAt(Math.min(st.index, Math.max(0, opts.count - 1)));
      if (!table || !opts.ready || (opts.count > 0 && !here)) return;
      const key = `${rec.id}|${opts.count}|${here?.id ?? ""}:${here?._version ?? ""}`;
      if (goneChecked.current === key) return;
      goneChecked.current = key;
      const id = rec.id;
      void rowExists(env.ws, table.id, id).then((exists) => {
        const cur = get().rec;
        if (!exists && cur && cur.id === id && !cur.isNew) markGone();
      });
      return;
    }
    goneChecked.current = "";
    if (at !== st.index) set({ index: at });
    const row = opts.access.rowAt(at);
    if (!row || row._version <= (rec.version ?? 0)) return;
    if (!isDirty(rec)) {
      set({ rec: recFromRow(row) });
      return;
    }
    if (st.conflict) return;
    const merged = rebase(rec, row, eq);
    set({ rec: merged.rec });
    if (merged.conflicts.length > 0) set({ conflict: row, conflictFields: merged.conflicts });
    else if (merged.theirsChanged) void refreshCalc();
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
    store, eq, moveTo, goTo, save: () => save(), leave, cancelEdit, deleteRecord, commitField, noteTyping, notifyChange, click, answerConfirm, refreshCalc,
    dismissMessage: () => set({ message: null, notice: null }),
    keepMine, discardMine, saveAsNew, discardDeleted, calcFor,
  };
}


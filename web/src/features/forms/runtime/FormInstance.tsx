import { EmptyState, SkeletonLines } from "@/components/EditorParts";
import { useCallback, useContext, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnPrimary, infoBox } from "@/components/styles";
import { useStatusBar } from "@/lib/statusbar";
import { findTable, type Column } from "@/lib/schema";
import { RowHistoryPanel } from "@/features/history/RowHistoryPanel";
import { useSavedQuery } from "@/features/queries/savedQueries";
import { type Form, type FormControl, type FormSection } from "../formsApi";
import { ControlView, StackedContext, domId, type ControlEnv } from "./Controls";
import { accuracyColumn } from "@/lib/geo/locate";
import { useIsCompact } from "@/lib/useMediaQuery";
import { useFormEnv, type ParentLink } from "./env";
import {
  applyCalc,
  bufferOf,
  emptyCalc,
  isDirty,
  labelFor,
  layoutControls,
  recFromRow,
  recordLabel,
  stackedOrder,
  tabOrder,
  type CalcState,
  type Fields,
} from "./model";
import { DELETED_BANNER } from "./gone";
import { createSessionStore, useFormSession, useSessionState } from "./useFormSession";
import { linkFilter, pagesFor, useQueryRows, useTableCount, useTablePages, type Link, type PageAccess } from "./sources";
import { ConfirmDialog, Toolbar, SubformHost } from "./FormInstanceParts";
import { dismissActiveInput } from "@/lib/touchErgonomics";

const FOCUSABLE = "input:not([disabled]), select:not([disabled]), button:not([disabled]), textarea:not([disabled])";
const DETAIL_PAD = 0;

export interface Registry {
  flushers: Set<() => Promise<boolean>>;
  dirty: Map<symbol, boolean>;
  onDirty: () => void;
  changed: { current: () => void };
}

function Band({
  section,
  prefix,
  fields,
  display,
  recordId,
  env,
  renderOnly,
}: {
  section: FormSection;
  prefix: string;
  fields: Fields;
  display: Record<string, string>;
  recordId: number | null;
  env: ControlEnv;
  renderOnly?: (c: FormControl) => boolean;
}) {
  const stacked = useContext(StackedContext);
  const controls = useMemo(() => section.controls ?? [], [section.controls]);
  const ordered = useMemo(() => {
    const list = tabOrder(controls).filter((c) => !renderOnly || renderOnly(c));
    // Stacked (phone) layout reads top to bottom, left to right, as designed.
    return stacked ? stackedOrder(list) : list;
  }, [controls, renderOnly, stacked]);
  return (
    <>
      {ordered.map((c) => {
        const label = c.type === "textbox" || c.type === "combobox" ? labelFor(c, controls) : undefined;
        return (
          <ControlView
            key={c.id}
            control={c}
            siblings={controls}
            domPrefix={prefix}
            fields={fields}
            display={display}
            recordId={recordId}
            env={env}
            labelledBy={label ? domId(prefix, label) : undefined}
          />
        );
      })}
    </>
  );
}

interface Props {
  form: Form;
  link?: Link | null;
  /** The master record is not saved yet: no records can be listed or added. */
  blocked?: boolean;
  top?: boolean;
  tabKey?: string;
  parent?: ParentLink | null;
  label: string;
}

/** One running form: its record source, current record, views and toolbar. */
export function FormInstance({ form, link = null, blocked = false, top = false, tabKey = "", parent = null, label }: Props) {
  const env = useFormEnv();
  const uid = useId().replace(/[^a-zA-Z0-9]/g, "");
  const rootRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const setRecordNav = useStatusBar((s) => s.setRecordNav);
  const layout = form.layout;
  const view = top ? form.default_view : "continuous";
  const stacked = useIsCompact();

  // ---- record source ----------------------------------------------------------------
  const kind = form.source_kind;
  const queryId = kind === "query" && form.source_id ? form.source_id : null;
  const saved = useSavedQuery(env.ws, queryId);
  const tableFromQuery = saved.data?.editable && saved.data.table_id ? findTable(env.schema, saved.data.table_id) : undefined;
  const sourceTable = kind === "table" ? findTable(env.schema, form.source_id ?? "") : tableFromQuery;
  const tableSourceId = kind === "table" && !blocked && sourceTable ? sourceTable.id : null;
  const filter = linkFilter(link);
  const tc = useTableCount(env.ws, tableSourceId, filter);
  const qr = useQueryRows(env.ws, blocked ? null : queryId, sourceTable, link);
  const count = blocked ? 0 : kind === "query" ? qr.rows.length : kind === "none" ? 0 : tc.count;
  const ready = blocked || kind === "none" || (kind === "query" ? qr.ready : tc.ready);
  const writeTable = sourceTable && (kind === "table" || qr.hasIdentity) ? sourceTable : undefined;

  const canEdit = env.roleCanEdit && form.allow_edits && !!writeTable && !blocked;
  const canAdd = env.roleCanEdit && form.allow_additions && !!writeTable && !blocked;
  const canDelete = env.roleCanEdit && form.allow_deletions && !!writeTable && !blocked;

  const store = useState(createSessionStore)[0];
  const index = useSessionState(store, (s) => s.index);
  const bandCount = count + (canAdd ? 1 : 0);
  const virtualizer = useVirtualizer({
    count: view === "single" ? 0 : bandCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => (view === "datasheet" ? 30 : Math.max(24, layout.sections.detail.height)),
    overscan: 6,
  });
  const items = virtualizer.getVirtualItems();
  const first = view === "single" ? index : (items[0]?.index ?? 0);
  const last = view === "single" ? index : (items[items.length - 1]?.index ?? 0);
  const pages = useMemo(() => (tableSourceId ? pagesFor(first, last, count, index) : []), [tableSourceId, first, last, count, index]);
  const tablePages = useTablePages(env.ws, tableSourceId, filter, pages);
  const access: PageAccess = useMemo(
    () =>
      kind === "query"
        ? { rowAt: (i) => qr.rows[i], indexOfId: (id) => qr.rows.findIndex((r) => r.id === id) }
        : tablePages,
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [kind, qr.rows, tablePages.rowAt, tablePages.indexOfId],
  );

  // ---- subform registry -----------------------------------------------------------------
  const [childDirtyCount, setChildDirtyCount] = useState(0);
  const registry = useState<Registry>(() => ({
    flushers: new Set(),
    dirty: new Map(),
    onDirty: () => undefined,
    changed: { current: () => undefined },
  }))[0];
  useEffect(() => {
    registry.onDirty = () => setChildDirtyCount([...registry.dirty.values()].filter(Boolean).length);
  }, [registry]);
  const flushChildren = useCallback(async () => {
    for (const f of [...registry.flushers]) if (!(await f())) return false;
    return true;
  }, [registry]);

  const session = useFormSession(store, {
    form,
    table: writeTable,
    unbound: kind === "none",
    count,
    ready,
    access,
    canEdit,
    canAdd,
    canDelete,
    link,
    parent,
    flushChildren,
    refetch: () => undefined,
  });
  useEffect(() => {
    registry.changed.current = () => void session.refreshCalc();
  });

  const rec = useSessionState(store, (s) => s.rec);
  const overrides = useSessionState(store, (s) => s.overrides);
  const calc = useSessionState(store, (s) => s.calc);
  const message = useSessionState(store, (s) => s.message);
  const conflict = useSessionState(store, (s) => s.conflict);
  const conflictFields = useSessionState(store, (s) => s.conflictFields);
  const deleted = useSessionState(store, (s) => s.deleted);
  const notice = useSessionState(store, (s) => s.notice);
  const fieldErrors = useSessionState(store, (s) => s.fieldErrors);
  const confirm = useSessionState(store, (s) => s.confirm);
  const busy = useSessionState(store, (s) => s.busy);
  const focus = useSessionState(store, (s) => s.focus);
  const dirty = rec ? isDirty(rec) : false;
  const [deleting, setDeleting] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);

  // ---- tab, status bar --------------------------------------------------------------------
  useEffect(() => {
    if (!top) return;
    useTabs.getState().setDirty(env.ws, tabKey, dirty || childDirtyCount > 0);
    return () => useTabs.getState().setDirty(env.ws, tabKey, false);
  }, [top, env.ws, tabKey, dirty, childDirtyCount]);

  const navText = deleted ? "Deleted record" : recordLabel(index, count, !!rec?.isNew);
  useEffect(() => {
    if (!top) return;
    setRecordNav(blocked ? null : navText);
    return () => setRecordNav(null);
  }, [top, navText, blocked, setRecordNav]);

  // ---- derived helpers ----------------------------------------------------------------------
  const calcIds = useMemo(() => {
    return layoutControls(layout).filter((c) => c.calc).map((c) => c.id);
  }, [layout]);

  const columnOf = useCallback((id: string): Column | undefined => writeTable?.columns?.find((c) => c.id === id) ?? sourceTable?.columns?.find((c) => c.id === id), [writeTable, sourceTable]);
  const accuracyCol = useMemo(() => accuracyColumn(writeTable?.columns ?? []), [writeTable]);
  const columnName = useCallback(
    (id: string): string | undefined => (id === "id" ? "ID" : (columnOf(id)?.display_name ?? (kind === "query" ? id : undefined))),
    [columnOf, kind],
  );

  // Calculated values of rows that are not current.
  const [rowCalc, setRowCalc] = useState<Record<string, CalcState>>({});
  const requested = useRef(new Set<string>());
  useEffect(() => {
    if (calcIds.length === 0 || view === "single") return;
    for (const v of items) {
      if (v.index === index || v.index >= count) continue;
      const row = access.rowAt(v.index);
      if (!row) continue;
      const key = `${row.id}:${row._version}`;
      if (requested.current.has(key)) continue;
      requested.current.add(key);
      void session.calcFor(recFromRow(row)).then((resp) => {
        if (resp) setRowCalc((m) => ({ ...m, [key]: applyCalc(emptyCalc, resp, calcIds) }));
        else requested.current.delete(key);
      });
    }
  });

  // Focus requested by ui.focus.
  useEffect(() => {
    if (!focus || !rootRef.current) return;
    const scope = rootRef.current.querySelector('[data-current="true"]') ?? rootRef.current;
    const el = scope.querySelector(`[data-control-name="${CSS.escape(focus.name)}"]`);
    (el?.matches(FOCUSABLE) ? el : el?.querySelector(FOCUSABLE))?.dispatchEvent(new Event("focus-request"));
    (el?.matches(FOCUSABLE) ? (el as HTMLElement) : el?.querySelector<HTMLElement>(FOCUSABLE))?.focus();
  }, [focus]);

  // Keep the current record visible in continuous views.
  useEffect(() => {
    if (view !== "single" && rec) virtualizer.scrollToIndex(index, { align: "auto" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [index, view]);

  const recEditable = rec ? (rec.isNew ? canAdd : canEdit) : false;

  function envFor(i: number): ControlEnv {
    const isCurrent = i === index && rec !== null;
    const target = () => session.goTo(i);
    const toCurrent = async (): Promise<boolean> => (store.getState().index === i && store.getState().rec ? true : target());
    const rowKey = (() => {
      const row = access.rowAt(i);
      return row ? `${row.id}:${row._version}` : "";
    })();
    return {
      ws: env.ws,
      columnOf,
      columnName,
      accuracyCol,
      editable: isCurrent ? recEditable : canEdit || (i >= count && canAdd),
      overrides,
      calc: isCurrent ? calc : (rowCalc[rowKey] ?? emptyCalc),
      fieldErrors: isCurrent ? fieldErrors : {},
      renderSubform: (control, fields) => (
        <SubformHost
          control={control}
          fields={fields}
          recordId={isCurrent ? (rec?.id ?? null) : (access.rowAt(i)?.id ?? null)}
          parentGone={isCurrent && deleted}
          registry={registry}
        />
      ),
      actions: {
        commit: async (c, colId, v, d) => ((await toCurrent()) ? session.commitField(c, colId, v, d) : false),
        change: (c, colId, v) => {
          if (isCurrent) session.notifyChange(c, colId, v);
        },
        typing: (colId, v) => {
          if (isCurrent) session.noteTyping(colId, v);
        },
        click: (c) => void toCurrent().then((ok) => (ok ? session.click(c) : undefined)),
        enterRow: () => {
          if (!isCurrent && !store.getState().busy) void target();
        },
        save: () => session.save(),
      },
    };
  }

  function bandData(i: number): { fields: Fields; display: Record<string, string>; id: number | null } {
    if (i === index && rec) return { fields: bufferOf(rec), display: rec.display, id: rec.id };
    const row = access.rowAt(i);
    if (row) return { fields: row.fields, display: row.display ?? {}, id: row.id };
    return { fields: {}, display: {}, id: null };
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (!e.currentTarget.contains(e.target as Node) || e.nativeEvent.isComposing) return;
    const mod = e.metaKey || e.ctrlKey;
    if (e.key === "Enter" && e.shiftKey && !mod) {
      e.preventDefault();
      e.stopPropagation();
      void session.save();
    } else if (e.key === "Escape" && !e.defaultPrevented && (dirty || message || conflict || notice)) {
      e.preventDefault();
      e.stopPropagation();
      session.cancelEdit();
    } else if (mod && e.shiftKey && e.key.toLowerCase() === "n") {
      e.preventDefault();
      e.stopPropagation();
      void session.moveTo("new");
    } else if (e.altKey && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
      e.preventDefault();
      e.stopPropagation();
      void session.moveTo(e.key === "ArrowDown" ? "next" : "prev");
    }
  }

  // ---- rendering ------------------------------------------------------------------------------
  const sections = layout.sections;
  const header = sections.header;
  const footer = sections.footer;
  const cur = bandData(index);
  const curEnv = envFor(index);
  const toolbar = (
    <Toolbar
      top={top}
      label={label}
      navText={navText}
      count={count}
      index={index}
      rec={rec}
      busy={busy}
      dirty={dirty}
      canAdd={canAdd}
      canDelete={canDelete && !!rec && !rec.isNew && !deleted}
      canSave={dirty && !deleted && (rec?.isNew ? canAdd : canEdit)}
      onNav={(t) => void session.moveTo(t)}
      onSave={() => void session.save()}
      onUndo={() => session.cancelEdit()}
      onDelete={() => setDeleting(true)}
      canHistory={top && !!writeTable && !!rec && !rec.isNew && rec.id !== null && !deleted}
      onHistory={() => setHistoryOpen(true)}
    />
  );

  let body: ReactNode;
  if (blocked) {
    body = <EmptyState kind="form" compact title={`Save this record first to add ${label}.`} />;
  } else if (!ready) {
    body = <SkeletonLines rows={4} label="Loading records…" />;
  } else if (!rec && view === "single") {
    body = <EmptyState kind="form" compact title="No records" hint={canAdd ? "Use New record to add one." : "There is nothing to show yet."} />;
  } else if (view === "single" && stacked) {
    const band = (name: string, section: FormSection) =>
      (section.controls?.length ?? 0) > 0 && (
        <div className="flex flex-col gap-2">
          <Band section={section} prefix={`${uid}${name}`} fields={cur.fields} display={cur.display} recordId={cur.id} env={curEnv} />
        </div>
      );
    body = (
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden touch-pan-y" data-current="true">
        <div className="flex flex-col gap-3 p-3">
          {band("h", header)}
          {band("d", sections.detail)}
          {band("f", footer)}
        </div>
      </div>
    );
  } else if (view === "single") {
    body = (
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto touch-pan-y">
        <div style={{ width: layout.width }} className="relative" data-current="true">
          <div style={{ position: "relative", height: header.height, width: layout.width }}>
            <Band section={header} prefix={`${uid}h`} fields={cur.fields} display={cur.display} recordId={cur.id} env={curEnv} />
          </div>
          <div style={{ position: "relative", height: sections.detail.height, width: layout.width }}>
            <Band section={sections.detail} prefix={`${uid}d`} fields={cur.fields} display={cur.display} recordId={cur.id} env={curEnv} />
          </div>
          <div style={{ position: "relative", height: footer.height, width: layout.width }}>
            <Band section={footer} prefix={`${uid}f`} fields={cur.fields} display={cur.display} recordId={cur.id} env={curEnv} />
          </div>
        </div>
      </div>
    );
  } else if (view === "continuous") {
    const hh = DETAIL_PAD + header.height;
    body = (
      <>
        {hh > 0 && (
          <div style={{ position: "relative", height: header.height, width: layout.width, flexShrink: 0 }}>
            <Band section={header} prefix={`${uid}h`} fields={cur.fields} display={cur.display} recordId={cur.id} env={curEnv} />
          </div>
        )}
        <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto touch-pan-y">
          <div role="group" aria-label={`${label} records`} style={{ height: virtualizer.getTotalSize(), width: layout.width, position: "relative" }}>
            {items.map((v) => {
              const d = bandData(v.index);
              const isCur = v.index === index && rec !== null;
              return (
                <div
                  key={v.key}
                  role="group"
                  aria-label={v.index >= count ? "New record" : `Record ${v.index + 1}`}
                  aria-current={isCur ? "true" : undefined}
                  data-current={isCur ? "true" : undefined}
                  style={{ position: "absolute", top: v.start, height: v.size, width: layout.width }}
                  className={`border-b border-border ${isCur ? "border-l-4 border-l-accent bg-accent-soft" : ""}`}
                >
                  <Band section={sections.detail} prefix={`${uid}r${v.index}`} fields={d.fields} display={d.display} recordId={d.id} env={envFor(v.index)} />
                </div>
              );
            })}
          </div>
        </div>
        {footer.height > 0 && (
          <div style={{ position: "relative", height: footer.height, width: layout.width, flexShrink: 0 }}>
            <Band section={footer} prefix={`${uid}f`} fields={cur.fields} display={cur.display} recordId={cur.id} env={curEnv} />
          </div>
        )}
      </>
    );
  } else {
    const detailControls = sections.detail.controls ?? [];
    const headerControls = header.controls ?? [];
    const cols = tabOrder(detailControls.filter((c) => c.type === "textbox" || c.type === "combobox" || c.type === "checkbox")).sort(
      (a, b) => a.x - b.x || a.tab_index - b.tab_index,
    );
    const width = cols.reduce((n, c) => n + Math.max(60, c.w) + 4, 0);
    const headerFor = (c: FormControl) => {
      const l = labelFor(c, detailControls) ?? labelFor(c, headerControls);
      return l?.caption || c.caption || (c.bind ? columnName(c.bind.column_id) : undefined) || c.name;
    };
    body = (
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-auto touch-pan-y">
        <div style={{ width }} className="sticky top-0 z-10 flex border-b border-border bg-surface-2 text-xs font-semibold text-fg-muted">
          {cols.map((c) => (
            <div key={c.id} style={{ width: Math.max(60, c.w) + 4 }} className="truncate px-2 py-1">
              {headerFor(c)}
            </div>
          ))}
        </div>
        <div role="group" aria-label={`${label} records`} style={{ height: virtualizer.getTotalSize(), width, position: "relative" }}>
          {items.map((v) => {
            const d = bandData(v.index);
            const isCur = v.index === index && rec !== null;
            const rowEnv = envFor(v.index);
            return (
              <div
                key={v.key}
                role="group"
                aria-label={v.index >= count ? "New record" : `Record ${v.index + 1}`}
                aria-current={isCur ? "true" : undefined}
                data-current={isCur ? "true" : undefined}
                style={{ position: "absolute", top: v.start, height: v.size, width }}
                className={`flex border-b border-border ${isCur ? "border-l-4 border-l-accent bg-accent-soft" : ""}`}
              >
                {cols.map((c) => (
                  <div key={c.id} style={{ width: Math.max(60, c.w) + 4, height: v.size }} className="p-0.5">
                    <ControlView control={c} siblings={cols} domPrefix={`${uid}g${v.index}`} fields={d.fields} display={d.display} recordId={d.id} env={rowEnv} fill />
                  </div>
                ))}
              </div>
            );
          })}
        </div>
      </div>
    );
  }

  const rootCls = top ? "flex min-h-0 flex-1 flex-col" : "flex h-full min-h-0 flex-col";
  return (
    <StackedContext.Provider value={stacked && view === "single"}>
    <div
      ref={rootRef}
      role="group"
      aria-label={label}
      onKeyDown={onKeyDown}
      onPointerDown={(e) => dismissActiveInput(e.target)}
      className={`${rootCls} touch-pan-y`}
    >
      {toolbar}
      {(message || conflict) && (
        <div role="alert" className={`${alertBox} mx-2 mt-1 flex flex-wrap items-center gap-2`}>
          {message && <span>{message}</span>}
          {conflict && (
            <>
              <span>
                This record was changed by someone else while you were editing.
                {conflictFields.length > 0 &&
                  ` Both of you changed ${conflictFields.length === 1 ? "this field" : "these fields"}: ${conflictFields.map((f) => columnName(f) ?? f).join(", ")}.`}
              </span>
              <button type="button" className={btn} onClick={() => session.keepMine()}>
                Keep my changes
              </button>
              <button type="button" className={btn} onClick={() => session.discardMine()}>
                Use their version
              </button>
            </>
          )}
          {message && !conflict && (
            <button type="button" className={btn} onClick={() => session.dismissMessage()}>
              Dismiss
            </button>
          )}
        </div>
      )}
      {deleted && (
        <div role="alert" className={`${alertBox} mx-2 mt-1 flex flex-wrap items-center gap-2`}>
          <span>{DELETED_BANNER}</span>
          {canAdd && (
            <button type="button" className={btnPrimary} disabled={busy} onClick={() => void session.saveAsNew()}>
              Save as new record
            </button>
          )}
          <button type="button" className={btn} disabled={busy} onClick={() => session.discardDeleted()}>
            Discard
          </button>
        </div>
      )}
      {notice && (
        <div role="status" className={`${infoBox} mx-2 mt-1 flex flex-wrap items-center gap-2`}>
          <span>{notice}</span>
          <button type="button" className={btn} onClick={() => session.dismissMessage()}>
            Dismiss
          </button>
        </div>
      )}
      {body}
      {historyOpen && writeTable && rec && rec.id !== null && (
        <RowHistoryPanel ws={env.ws} tableId={writeTable.id} rowId={rec.id} onClose={() => setHistoryOpen(false)} />
      )}
      <ConfirmDialog
        open={confirm !== null}
        title="Please confirm"
        message={confirm?.message ?? ""}
        yes="Yes"
        no="No"
        onAnswer={(a) => void session.answerConfirm(a)}
      />
      <ConfirmDialog
        open={deleting}
        title="Delete record"
        message="Delete this record? This cannot be undone."
        yes="Delete"
        no="Cancel"
        onAnswer={(a) => {
          setDeleting(false);
          if (a) void session.deleteRecord();
        }}
      />
    </div>
    </StackedContext.Provider>
  );
}


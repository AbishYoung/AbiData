import { useId, useState, type FormEvent, type ReactNode } from "react";
import { alertBox, btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import type { Schema } from "@/lib/schema";
import { Modal, selectClass } from "./Modal";
import type { Join, JoinKind, NewSource, QField, SourceInfo } from "./model";
import { blankExprField, blankField } from "./model";
import type { Saved } from "./savedQueries";

function Actions({ children }: { children: ReactNode }) {
  return <div className="mt-4 flex justify-end gap-2">{children}</div>;
}

/** Picks a table or saved query to place on the canvas. */
export function AddSourceDialog({
  schema,
  queries,
  excludeQueryId,
  onAdd,
  onClose,
}: {
  schema: Schema | undefined;
  queries: readonly Saved[];
  excludeQueryId: string | null;
  onAdd: (s: NewSource) => void;
  onClose: () => void;
}) {
  const id = useId();
  const tables = [...(schema?.tables ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name));
  const others = queries.filter((q) => q.id !== excludeQueryId).sort((a, b) => a.name.localeCompare(b.name));
  const [value, setValue] = useState("");

  function submit(e: FormEvent): void {
    e.preventDefault();
    if (value.startsWith("t:")) {
      const t = tables.find((x) => x.id === value.slice(2));
      if (t) onAdd({ table_id: t.id, baseName: t.physical_name });
    } else if (value.startsWith("q:")) {
      const q = others.find((x) => x.id === value.slice(2));
      if (q) onAdd({ query_id: q.id, baseName: q.name });
    }
  }

  return (
    <Modal title="Add table or query" description="The chosen object appears as a box on the canvas." onClose={onClose}>
      <form onSubmit={submit}>
        <label className={labelClass} htmlFor={id}>
          Table or query
        </label>
        <select id={id} className={selectClass} value={value} onChange={(e) => setValue(e.target.value)}>
          <option value="">Choose…</option>
          <optgroup label="Tables">
            {tables.map((t) => (
              <option key={t.id} value={`t:${t.id}`}>
                {t.display_name}
              </option>
            ))}
          </optgroup>
          {others.length > 0 && (
            <optgroup label="Saved queries">
              {others.map((q) => (
                <option key={q.id} value={`q:${q.id}`}>
                  {q.name}
                </option>
              ))}
            </optgroup>
          )}
        </select>
        <Actions>
          <button type="button" className={btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={value === ""}>
            Add
          </button>
        </Actions>
      </form>
    </Modal>
  );
}

function SourceColumnSelects({
  prefix,
  infos,
  alias,
  column,
  onAlias,
  onColumn,
}: {
  prefix: string;
  infos: readonly SourceInfo[];
  alias: string;
  column: string;
  onAlias: (a: string) => void;
  onColumn: (c: string) => void;
}) {
  const id = useId();
  const cols = infos.find((i) => i.alias === alias)?.columns ?? [];
  return (
    <div className="grid grid-cols-2 gap-2">
      <div>
        <label className={labelClass} htmlFor={`${id}-s`}>
          {prefix} source
        </label>
        <select
          id={`${id}-s`}
          className={selectClass}
          value={alias}
          onChange={(e) => {
            onAlias(e.target.value);
            onColumn("");
          }}
        >
          <option value="">Choose…</option>
          {infos.map((i) => (
            <option key={i.alias} value={i.alias}>
              {i.title} ({i.alias})
            </option>
          ))}
        </select>
      </div>
      <div>
        <label className={labelClass} htmlFor={`${id}-c`}>
          {prefix} column
        </label>
        <select id={`${id}-c`} className={selectClass} value={column} onChange={(e) => onColumn(e.target.value)}>
          <option value="">Choose…</option>
          {cols.map((c) => (
            <option key={c.id} value={c.id}>
              {c.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

/** Keyboard alternative to dragging a column onto another: pick both ends and the join kind. */
export function AddJoinDialog({ infos, onAdd, onClose }: { infos: readonly SourceInfo[]; onAdd: (j: Join) => void; onClose: () => void }) {
  const kindId = useId();
  const [la, setLa] = useState("");
  const [lc, setLc] = useState("");
  const [ra, setRa] = useState("");
  const [rc, setRc] = useState("");
  const [kind, setKind] = useState<JoinKind>("inner");
  const [error, setError] = useState<string | null>(null);

  function submit(e: FormEvent): void {
    e.preventDefault();
    if (!la || !lc || !ra || !rc) {
      setError("Choose a source and a column on both sides.");
      return;
    }
    if (la === ra) {
      setError("A join connects two different boxes. Add the table again to join it to itself.");
      return;
    }
    onAdd({ left_alias: la, left_column_id: lc, right_alias: ra, right_column_id: rc, kind });
  }

  return (
    <Modal title="Add join" description="Rows are matched where the two columns are equal." onClose={onClose}>
      <form onSubmit={submit} className="space-y-3">
        <SourceColumnSelects prefix="Left" infos={infos} alias={la} column={lc} onAlias={setLa} onColumn={setLc} />
        <SourceColumnSelects prefix="Right" infos={infos} alias={ra} column={rc} onAlias={setRa} onColumn={setRc} />
        <div>
          <label className={labelClass} htmlFor={kindId}>
            Join kind
          </label>
          <select id={kindId} className={selectClass} value={kind} onChange={(e) => setKind(e.target.value === "left" ? "left" : "inner")}>
            <option value="inner">Inner: only rows that match on both sides</option>
            <option value="left">Left: all left rows, matching right rows if any</option>
          </select>
        </div>
        {error && (
          <p role="alert" className={alertBox}>
            {error}
          </p>
        )}
        <Actions>
          <button type="button" className={btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary}>
            Add join
          </button>
        </Actions>
      </form>
    </Modal>
  );
}

/** Chooses inner/left for an existing join, or deletes it. */
export function EditJoinDialog({
  summary,
  kind,
  onKind,
  onDelete,
  onClose,
}: {
  summary: string;
  kind: JoinKind;
  onKind: (k: JoinKind) => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  const id = useId();
  return (
    <Modal title="Edit join" description={summary} onClose={onClose}>
      <label className={labelClass} htmlFor={id}>
        Join kind
      </label>
      <select id={id} className={selectClass} value={kind} onChange={(e) => onKind(e.target.value === "left" ? "left" : "inner")}>
        <option value="inner">Inner: only rows that match on both sides</option>
        <option value="left">Left: all left rows, matching right rows if any</option>
      </select>
      <Actions>
        <button type="button" className={btn} onClick={onDelete}>
          Delete join
        </button>
        <button type="button" className={btnPrimary} onClick={onClose}>
          Done
        </button>
      </Actions>
    </Modal>
  );
}

/** Adds a column of a source, or a SQL expression, as an output field. */
export function AddFieldDialog({ infos, onAdd, onClose }: { infos: readonly SourceInfo[]; onAdd: (f: QField) => void; onClose: () => void }) {
  const exprId = useId();
  const [alias, setAlias] = useState("");
  const [column, setColumn] = useState("");
  const [expr, setExpr] = useState("");

  function submit(e: FormEvent): void {
    e.preventDefault();
    if (expr.trim() !== "") onAdd(blankExprField(expr.trim()));
    else if (alias && column) onAdd(blankField(alias, column));
  }

  return (
    <Modal title="Add field" description="Choose a column, or write an expression instead." onClose={onClose}>
      <form onSubmit={submit} className="space-y-3">
        <SourceColumnSelects prefix="Field" infos={infos} alias={alias} column={column} onAlias={setAlias} onColumn={setColumn} />
        <div>
          <label className={labelClass} htmlFor={exprId}>
            Or expression (SQL)
          </label>
          <input id={exprId} className={`${input} font-mono`} value={expr} onChange={(e) => setExpr(e.target.value)} placeholder="price * qty" />
        </div>
        <Actions>
          <button type="button" className={btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={expr.trim() === "" && !(alias && column)}>
            Add field
          </button>
        </Actions>
      </form>
    </Modal>
  );
}

/** Names the query for Save / Save as. */
export function SaveDialog({
  title,
  initialName,
  initialDescription,
  onSave,
  onClose,
}: {
  title: string;
  initialName: string;
  initialDescription: string;
  onSave: (name: string, description: string) => Promise<string | null>;
  onClose: () => void;
}) {
  const nameId = useId();
  const descId = useId();
  const [name, setName] = useState(initialName);
  const [description, setDescription] = useState(initialDescription);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    setBusy(true);
    const err = await onSave(name.trim(), description.trim());
    setBusy(false);
    if (err) setError(err);
  }

  return (
    <Modal title={title} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className={labelClass} htmlFor={nameId}>
          Query name
        </label>
        <input id={nameId} className={input} value={name} onChange={(e) => setName(e.target.value)} />
        <label className={`${labelClass} mt-3`} htmlFor={descId}>
          Description (optional)
        </label>
        <input id={descId} className={input} value={description} onChange={(e) => setDescription(e.target.value)} />
        {error && (
          <p role="alert" className={`${alertBox} mt-3`}>
            {error}
          </p>
        )}
        <Actions>
          <button type="button" className={btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={name.trim() === "" || busy}>
            Save
          </button>
        </Actions>
      </form>
    </Modal>
  );
}

export function ConvertDialog({ onConfirm, onClose }: { onConfirm: () => Promise<string | null>; onClose: () => void }) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  return (
    <Modal
      title="Convert to SQL"
      description="The query becomes a SQL query and can no longer be edited visually. This cannot be undone."
      onClose={onClose}
    >
      {error && (
        <p role="alert" className={alertBox}>
          {error}
        </p>
      )}
      <Actions>
        <button type="button" className={btn} onClick={onClose}>
          Cancel
        </button>
        <button
          type="button"
          className={btnPrimary}
          disabled={busy}
          onClick={() => {
            setBusy(true);
            void onConfirm().then((err) => {
              setBusy(false);
              if (err) setError(err);
            });
          }}
        >
          Convert to SQL
        </button>
      </Actions>
    </Modal>
  );
}

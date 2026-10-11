import { useId, useState, type FormEvent } from "react";
import { Dialog } from "radix-ui";
import { queryClient } from "@/app/queryClient";
import { useTabs } from "@/app/tabs";
import {
  alertBox,
  btn,
  btnPrimary,
  input,
  label as labelClass,
  dialogDescription,
  dialogFooter,
  dialogOverlay,
  dialogPanel,
  dialogTitle,
} from "@/components/styles";
import {
  deleteForm,
  formKeys,
  getForm,
  updateForm,
  type Form,
} from "@/features/forms/formsApi";
import {
  applyExternalSave,
  draftKey,
  useDesignerDrafts,
} from "@/features/forms/designer/designerStore";
import {
  inputFromState,
  stateFromForm,
} from "@/features/forms/designer/layoutModel";
import { ApiError } from "@/lib/api";

const overlay = dialogOverlay;
const content = `${dialogPanel} w-[min(26rem,calc(100vw-2rem))] p-5`;

function message(e: unknown): string {
  if (e instanceof ApiError)
    return e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return "Something went wrong while contacting the server.";
}

interface Props {
  ws: string;
  form: Form;
  onClose: () => void;
}

/**
 * Renames a form. The list endpoint omits the module source, so the full form is fetched first and
 * saved back unchanged apart from the name (a PUT with an empty module would erase the handlers).
 */
export function RenameFormDialog({ ws, form, onClose }: Props) {
  const fieldId = useId();
  const renameTab = useTabs((s) => s.rename);
  const [name, setName] = useState(form.name);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const trimmed = name.trim();

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    if (trimmed === "" || trimmed === form.name) return;
    setBusy(true);
    try {
      const full = await getForm(ws, form.id);
      const saved = await updateForm(ws, form.id, full.version, {
        ...inputFromState(stateFromForm(full)),
        name: trimmed,
      });
      queryClient.setQueryData(formKeys.one(ws, saved.id), saved);
      void queryClient.invalidateQueries({ queryKey: formKeys.all(ws) });
      renameTab(ws, `form:${form.id}`, saved.name);
      applyExternalSave(ws, saved);
      onClose();
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className={overlay} />
        <Dialog.Content className={content}>
          <Dialog.Title className={dialogTitle}>Rename form</Dialog.Title>
          <Dialog.Description className="sr-only">
            Enter a new name for the form.
          </Dialog.Description>
          <form onSubmit={(e) => void submit(e)} className="mt-3">
            <label className={labelClass} htmlFor={fieldId}>
              Form name
            </label>
            <input
              id={fieldId}
              className={input}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            {error && (
              <p role="alert" className={`${alertBox} mt-3`}>
                {error}
              </p>
            )}
            <div className={dialogFooter}>
              <button type="button" className={btn} onClick={onClose}>
                Cancel
              </button>
              <button
                type="submit"
                className={btnPrimary}
                disabled={busy || trimmed === "" || trimmed === form.name}
              >
                Rename
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/** Deletes a form together with its module. */
export function DeleteFormDialog({ ws, form, onClose }: Props) {
  const closeTab = useTabs((s) => s.close);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function remove(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      await deleteForm(ws, form.id, form.version);
      useDesignerDrafts.getState().remove(draftKey(ws, `form:${form.id}`));
      closeTab(ws, `form:${form.id}`);
      void queryClient.invalidateQueries({ queryKey: formKeys.all(ws) });
      onClose();
    } catch (err) {
      setError(message(err));
      setBusy(false);
    }
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className={overlay} />
        <Dialog.Content className={content}>
          <Dialog.Title className={dialogTitle}>
            Delete form “{form.name}”
          </Dialog.Title>
          <Dialog.Description className={dialogDescription}>
            The form and its module are permanently deleted. Tables and their
            data are not affected.
          </Dialog.Description>
          {error && (
            <p role="alert" className={`${alertBox} mt-3`}>
              {error}
            </p>
          )}
          <div className={dialogFooter}>
            <button type="button" className={btn} onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className={btnPrimary}
              disabled={busy}
              onClick={() => void remove()}
            >
              Delete form
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

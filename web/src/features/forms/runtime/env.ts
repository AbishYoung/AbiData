import { createContext, useContext } from "react";
import type { Schema } from "@/lib/schema";
import type { Notice } from "./model";

/** What every form instance (the top-level form and its subforms) shares. */
export interface FormEnv {
  ws: string;
  schema: Schema;
  /** True when the workspace role may edit data (owner, builder, editor). */
  roleCanEdit: boolean;
  toast: (n: Notice) => void;
  alert: (n: Notice) => void;
  /** Opens another form by name in a tab (ui.open_form). */
  openForm: (name: string) => void;
  closeTab: () => void;
}

export const FormEnvContext = createContext<FormEnv | null>(null);

export function useFormEnv(): FormEnv {
  const env = useContext(FormEnvContext);
  if (!env) throw new Error("useFormEnv must be used inside a form runtime");
  return env;
}

/** How a subform talks to the form that embeds it. */
export interface ParentLink {
  /** Registers the subform's "save if dirty" so the master can flush it before moving on. */
  register: (flush: () => Promise<boolean>) => () => void;
  setDirty: (dirty: boolean) => void;
  /** A child record was saved or deleted: the master recalculates. */
  changed: () => void;
}

export const ParentContext = createContext<ParentLink | null>(null);

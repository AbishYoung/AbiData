import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertDialog } from "radix-ui";
import { api } from "@/lib/api";
import { useSchema } from "@/lib/schema";
import { useTabs } from "@/app/tabs";
import { ToastRegion, useToasts } from "@/components/Toasts";
import { alertBox, btnPrimary } from "@/components/styles";
import { formKeys, getForm, listForms } from "../formsApi";
import { FormEnvContext, type FormEnv } from "./env";
import { FormInstance } from "./FormInstance";
import type { Notice } from "./model";

const EDIT_ROLES: ReadonlySet<string> = new Set(["owner", "builder", "editor"]);

/** Runs a saved form: record navigation, controls, D10 events, save/delete. */
export function FormRuntime({ ws, formId, tabKey }: { ws: string; formId: string; tabKey: string }) {
  const qc = useQueryClient();
  const form = useQuery({ queryKey: formKeys.one(ws, formId), queryFn: () => getForm(ws, formId) });
  const schema = useSchema(ws);
  const workspace = useQuery({
    queryKey: ["workspace", ws],
    queryFn: () => api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), { label: "Opening workspace" }),
  });
  const { toasts, push, dismiss } = useToasts();
  const [alerts, setAlerts] = useState<readonly Notice[]>([]);

  const env: FormEnv | null = useMemo(() => {
    if (!schema.data) return null;
    return {
      ws,
      schema: schema.data,
      roleCanEdit: EDIT_ROLES.has(workspace.data?.role ?? ""),
      toast: (n) => push(n.level, n.text),
      alert: (n) => setAlerts((a) => [...a, n]),
      openForm: (name) => {
        void qc
          .fetchQuery({ queryKey: formKeys.list(ws), queryFn: () => listForms(ws) })
          .then((all) => {
            const f = all.find((x) => x.name.toLowerCase() === name.toLowerCase());
            if (f) useTabs.getState().open(ws, { key: `form:${f.id}`, kind: "form", objectId: f.id, view: "form", title: f.name });
            else push("warning", `The form “${name}” does not exist.`);
          })
          .catch(() => push("error", `The form “${name}” could not be opened.`));
      },
      closeTab: () => useTabs.getState().close(ws, tabKey),
    };
  }, [ws, schema.data, workspace.data?.role, push, qc, tabKey]);

  if (form.isError || schema.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4`}>
        {(form.error ?? schema.error)?.message}
      </div>
    );
  }
  if (!form.data || !env || !workspace.data) return <p className="p-4 text-fg-muted">Loading form…</p>;

  const alert = alerts[0];
  return (
    <FormEnvContext.Provider value={env}>
      <FormInstance key={form.data.id} form={form.data} top tabKey={tabKey} label={form.data.name} />
      <ToastRegion toasts={toasts} onDismiss={dismiss} />
      <AlertDialog.Root open={alert !== undefined}>
        <AlertDialog.Portal>
          <AlertDialog.Overlay className="fixed inset-0 z-50 bg-black/50" />
          <AlertDialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-5 text-fg shadow-xl">
            <AlertDialog.Title className="text-base font-semibold">{alert?.title ?? (alert?.level === "error" ? "Error" : "Notice")}</AlertDialog.Title>
            <AlertDialog.Description className="mt-2 text-sm">{alert?.text}</AlertDialog.Description>
            <div className="mt-4 flex justify-end">
              <AlertDialog.Action asChild>
                <button type="button" className={btnPrimary} onClick={() => setAlerts((a) => a.slice(1))}>
                  OK
                </button>
              </AlertDialog.Action>
            </div>
          </AlertDialog.Content>
        </AlertDialog.Portal>
      </AlertDialog.Root>
    </FormEnvContext.Provider>
  );
}

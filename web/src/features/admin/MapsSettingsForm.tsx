import { useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Save } from "lucide-react";
import { ApiError } from "@/lib/api";
import { ME_KEY } from "@/lib/session";
import { alertBox, btnPrimary, input, warningBox } from "@/components/styles";
import { adminKeys, putMaps, type MapsSettings } from "./adminApi";
import { Field, useUnloadGuard } from "./ui";


/** Client-side mirror of the server's checks (the server has the final word). */
export function tileUrlProblem(raw: string): string | undefined {
  const t = raw.trim();
  if (t === "") return undefined;
  if (!/\{z\}/.test(t) || !/\{x\}/.test(t) || !/\{y\}/.test(t)) return "The URL must contain {z}, {x} and {y}.";
  let url: URL;
  try {
    url = new URL(t.replace("{s}", "a").replace(/\{[zxyr]\}/g, "0"));
  } catch {
    return "Enter a full URL starting with https://.";
  }
  const local = url.hostname === "localhost" || url.hostname === "127.0.0.1" || url.hostname === "[::1]" || url.hostname.endsWith(".localhost");
  if (url.protocol !== "https:" && !(url.protocol === "http:" && local)) return "The URL must use https (http only for localhost).";
  return undefined;
}

/** Admin > Settings > Maps: the optional base map tile server. */
export function MapsSettingsForm({ initial }: { initial: MapsSettings }) {
  const qc = useQueryClient();
  const [saved, setSaved] = useState(false);
  const [topError, setTopError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isDirty },
  } = useForm<MapsSettings>({ defaultValues: initial });
  useUnloadGuard(isDirty);
  const save = useMutation({
    meta: { inline: true },
    mutationFn: (v: MapsSettings) => putMaps({ tile_url: v.tile_url.trim(), attribution: v.attribution.trim() }),
    onSuccess: async (m) => {
      reset(m);
      qc.setQueryData(adminKeys.maps, m);
      await Promise.all([qc.invalidateQueries({ queryKey: adminKeys.settings }), qc.invalidateQueries({ queryKey: ME_KEY })]);
      setSaved(true);
      setTopError(null);
    },
    onError: (e) => {
      setSaved(false);
      setTopError(e instanceof ApiError ? e.message : "The map settings could not be saved.");
    },
  });
  const url = watch("tile_url");
  return (
    <form
      aria-labelledby="maps-heading"
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        setSaved(false);
        void handleSubmit((v) => save.mutate(v))();
      }}
      className="flex max-w-xl flex-col gap-3"
    >
      <h3 id="maps-heading" className="m-0 text-sm font-semibold">
        Maps
      </h3>
      <p className="m-0 text-sm text-fg-muted">
        Maps for location and area fields draw on a plain latitude/longitude grid by default, so they work offline and contact no one. Set a tile server to show a
        street or satellite map underneath.
      </p>
      {url.trim() !== "" && (
        <p className={warningBox}>
          With a tile server set, every user&rsquo;s browser requests map images for the areas they view from that server. It learns those areas and the users&rsquo;
          IP addresses. Use a provider your organisation is allowed to share locations with, or a self-hosted tile server.
        </p>
      )}
      {topError && (
        <div role="alert" className={alertBox}>
          {topError}
        </div>
      )}
      <Field
        id="maps-tile-url"
        label="Tile URL template"
        hint="For example https://tile.openstreetmap.org/{z}/{x}/{y}.png. Use {s} as the first part of the host for a, b, c subdomains. Leave empty to turn tiles off."
        error={errors.tile_url?.message}
      >
        {(aria) => <input className={`${input} font-mono`} autoComplete="off" spellCheck={false} {...aria} {...register("tile_url", { validate: (v) => tileUrlProblem(v) ?? true })} />}
      </Field>
      <Field id="maps-attribution" label="Attribution" hint="Shown on every map, as the provider requires, for example © OpenStreetMap contributors." error={errors.attribution?.message}>
        {(aria) => <input className={input} autoComplete="off" {...aria} {...register("attribution", { maxLength: { value: 300, message: "Use at most 300 characters." } })} />}
      </Field>
      <p className="m-0 text-xs text-fg-muted">The server allows images from the tile server only while this is set. Users see the change after they reload the page.</p>
      <div className="flex items-center gap-2">
        <button type="submit" className={btnPrimary} disabled={save.isPending || !isDirty}>
          <Save size={14} aria-hidden="true" />
          Save map settings
        </button>
        <span role="status" className="text-sm text-fg-muted">
          {saved && !isDirty ? "Saved" : isDirty ? "Unsaved changes" : ""}
        </span>
      </div>
    </form>
  );
}

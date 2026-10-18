import type { ComponentProps } from "react";
import { suspended } from "@/lib/suspended";

// Leaflet (about 145 kB) and the map editors load on first use (ADR 0015, review R7): the entry
// chunk carries none of it.

const editors = () => import("./GeoEditors");

export const GeoValueDialog = suspended<ComponentProps<typeof import("./GeoEditors").GeoValueDialog>>(() => editors().then((m) => ({ default: m.GeoValueDialog })));
export const AreaDialog = suspended<ComponentProps<typeof import("./GeoEditors").AreaDialog>>(() => editors().then((m) => ({ default: m.AreaDialog })));

/** Starts loading the map editors (on hover or focus of something that opens one). */
export function prefetchMaps(): void {
  void editors();
}

/** The datasheet's location filter dialog. */
export const GeoFilterDialog = suspended<ComponentProps<typeof import("@/features/datasheet/GeoFilterDialog").GeoFilterDialog>>(() =>
  import("@/features/datasheet/GeoFilterDialog").then((m) => ({ default: m.GeoFilterDialog })),
);

/** A form's Map control; a plain box holds its place while Leaflet loads. */
export const MapControl = suspended<ComponentProps<typeof import("@/features/forms/runtime/MapControl").MapControl>>(
  () => import("@/features/forms/runtime/MapControl").then((m) => ({ default: m.MapControl })),
  <div className="h-full min-h-40 w-full animate-pulse rounded border border-border bg-surface-3" aria-hidden="true" />,
);

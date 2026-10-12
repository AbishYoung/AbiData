import type { Column } from "@/lib/schema";

export type LocateResult = { ok: true; lat: number; lon: number; accuracy: number } | { ok: false; message: string };

/** Asks the browser for the device position (GPS on phones): high accuracy, 20 s timeout, no cached fix. */
export function locate(timeoutMs = 20_000): Promise<LocateResult> {
  if (typeof window !== "undefined" && window.isSecureContext === false) {
    return Promise.resolve({ ok: false, message: "Your location is only available over a secure (HTTPS) connection or on localhost. Enter the coordinates instead." });
  }
  if (typeof navigator === "undefined" || !("geolocation" in navigator)) {
    return Promise.resolve({ ok: false, message: "This browser cannot share your location. Enter the coordinates instead." });
  }
  return new Promise((resolve) => {
    navigator.geolocation.getCurrentPosition(
      (pos) => resolve({ ok: true, lat: pos.coords.latitude, lon: pos.coords.longitude, accuracy: pos.coords.accuracy }),
      (err) => {
        switch (err.code) {
          case err.PERMISSION_DENIED:
            resolve({ ok: false, message: "Location access was denied. Allow location for this site in your browser settings, or enter the coordinates." });
            break;
          case err.TIMEOUT:
            resolve({ ok: false, message: "Finding your location took too long. Try again outdoors, or enter the coordinates." });
            break;
          default:
            resolve({ ok: false, message: "Your location is not available right now. Check that location services are on, or enter the coordinates." });
        }
      },
      { enableHighAccuracy: true, timeout: timeoutMs, maximumAge: 0 },
    );
  });
}

/** A number field to receive the GPS accuracy: the first integer or number field whose name contains "accuracy". */
export function accuracyColumn(columns: readonly Column[]): Column | undefined {
  return columns.find(
    (c) => (c.field_type === "number" || c.field_type === "integer") && (c.display_name.toLowerCase().includes("accuracy") || c.physical_name.toLowerCase().includes("accuracy")),
  );
}

/** The accuracy value to store for a column: whole metres for an integer field, one decimal otherwise. */
export function accuracyValue(col: Column, metres: number): number {
  return col.field_type === "integer" ? Math.round(metres) : Math.round(metres * 10) / 10;
}

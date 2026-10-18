import { useId, useState } from "react";
import { LocateFixed } from "lucide-react";
import { btn, btnPrimary, fieldError } from "@/components/styles";
import { locate } from "@/lib/geo/locate";

/** "Use my location" with its status line; large and prominent on phones. */
export function LocateButton({ onLocated, prominent }: { onLocated: (lat: number, lon: number, accuracy: number) => void; prominent: boolean }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const errId = useId();
  async function run() {
    setBusy(true);
    setError(null);
    const r = await locate();
    setBusy(false);
    if (r.ok) onLocated(r.lat, r.lon, r.accuracy);
    else setError(r.message);
  }
  return (
    <div className={prominent ? "w-full" : ""}>
      <button
        type="button"
        className={prominent ? `${btnPrimary} !h-12 w-full text-base` : btn}
        onClick={() => void run()}
        disabled={busy}
        aria-describedby={error ? errId : undefined}
      >
        <LocateFixed size={prominent ? 20 : 14} aria-hidden="true" className={busy ? "animate-pulse" : ""} />
        {busy ? "Finding your location…" : "Use my location"}
      </button>
      {error && (
        <p id={errId} role="alert" className={fieldError}>
          {error}
        </p>
      )}
    </div>
  );
}

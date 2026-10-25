import { useEffect, useRef, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { Minus, Plus } from "lucide-react";
import { iconBtn } from "@/components/styles";
import { haptic } from "@/lib/touchErgonomics";

/** A held button repeats after this long (ms), then speeds up to the floor. */
const HOLD_DELAY = 400;
const REPEAT_START = 160;
const REPEAT_FLOOR = 40;

/** The text after adding `delta` to a whole-number draft; an empty or unreadable draft counts as 0. */
export function stepText(text: string, delta: number): string {
  const n = Number.parseInt(text.trim().replace(/,/g, ""), 10);
  const base = Number.isFinite(n) ? n : 0;
  return String(Math.max(Number.MIN_SAFE_INTEGER, Math.min(Number.MAX_SAFE_INTEGER, base + delta)));
}

/**
 * Large `-` and `+` buttons for whole-number fields, so a value can be nudged with one thumb without the soft
 * keyboard (ADR 0022). Holding a button repeats and accelerates. They are ordinary buttons, so keyboard
 * activation steps once. `children` is the number input between them.
 */
export function Stepper({ text, onText, label, children }: { text: string; onText: (t: string) => void; label: string; children: ReactNode }) {
  const textRef = useRef(text);
  const onTextRef = useRef(onText);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    textRef.current = text;
    onTextRef.current = onText;
  });

  function stop() {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
  }
  useEffect(() => stop, []);

  function step(delta: number) {
    const next = stepText(textRef.current, delta);
    textRef.current = next;
    onTextRef.current(next);
    haptic(6);
  }
  function start(e: ReactPointerEvent<HTMLButtonElement>, delta: number) {
    if (e.button !== 0) return;
    stop();
    step(delta);
    let wait = REPEAT_START;
    const tick = () => {
      step(delta);
      wait = Math.max(REPEAT_FLOOR, wait * 0.85);
      timer.current = setTimeout(tick, wait);
    };
    timer.current = setTimeout(tick, HOLD_DELAY);
  }
  // A pointer press already stepped on pointerdown; a keyboard "click" has no pointer (detail 0).
  function keyboardStep(e: ReactMouseEvent, delta: number) {
    if (e.detail === 0) step(delta);
  }
  const btn = `${iconBtn} !h-12 !w-12 shrink-0 border border-border bg-surface`;
  return (
    <div className="flex items-center gap-2">
      <button type="button" className={btn} aria-label={`Decrease ${label}`} onPointerDown={(e) => start(e, -1)} onPointerUp={stop} onPointerCancel={stop} onPointerLeave={stop} onClick={(e) => keyboardStep(e, -1)}>
        <Minus size={20} aria-hidden="true" />
      </button>
      <div className="min-w-0 flex-1">{children}</div>
      <button type="button" className={btn} aria-label={`Increase ${label}`} onPointerDown={(e) => start(e, 1)} onPointerUp={stop} onPointerCancel={stop} onPointerLeave={stop} onClick={(e) => keyboardStep(e, 1)}>
        <Plus size={20} aria-hidden="true" />
      </button>
    </div>
  );
}

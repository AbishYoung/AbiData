import { lazy, Suspense, type ComponentType, type ReactNode } from "react";

/**
 * A lazily loaded component that renders inside its own Suspense boundary, so callers use it like
 * the eager one (ADR 0015, review R7). `fallback` holds its place while the chunk loads.
 */
export function suspended<P extends object>(load: () => Promise<{ default: ComponentType<P> }>, fallback: ReactNode = null) {
  const Lazy = lazy(load);
  return function Suspended(props: P) {
    return (
      <Suspense fallback={fallback}>
        <Lazy {...props} />
      </Suspense>
    );
  };
}

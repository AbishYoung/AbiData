import type { ReactNode } from "react";

export function AuthLayout({ title, children }: { title: string; children: ReactNode }) {
  return (
    <main className="flex min-h-full items-center justify-center p-4">
      <div className="w-full max-w-sm rounded-lg border border-border bg-surface p-6 shadow-sm">
        <p className="text-sm font-semibold text-accent">AbiData</p>
        <h1 className="mb-4 mt-1 text-xl font-semibold text-fg">{title}</h1>
        {children}
      </div>
    </main>
  );
}

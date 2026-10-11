import type { ReactNode } from "react";
import { Database, History, ShieldCheck, Table2 } from "lucide-react";
import { LogoMark } from "@/components/brand/Logo";

const FEATURES = [
  { icon: Table2, text: "Tables, queries, forms and reports in one workspace" },
  { icon: History, text: "Row history and live updates for the whole team" },
  {
    icon: ShieldCheck,
    text: "Self-hosted, with roles, backups and an audit trail",
  },
];

/** Decorative data-grid motif: a datasheet card with a smaller card behind it. All aria-hidden. */
function GridMotif() {
  const rows = [
    [34, 22, 14],
    [26, 30, 12],
    [38, 16, 20],
    [30, 24, 10],
    [22, 34, 16],
  ];
  return (
    <svg
      viewBox="0 0 360 230"
      className="h-auto w-full max-w-md"
      aria-hidden="true"
      focusable="false"
    >
      <rect
        x="40"
        y="10"
        width="300"
        height="150"
        rx="12"
        fill="#fff"
        fillOpacity="0.06"
        stroke="#fff"
        strokeOpacity="0.25"
      />
      <rect
        x="10"
        y="40"
        width="320"
        height="180"
        rx="14"
        fill="#fff"
        fillOpacity="0.1"
        stroke="#fff"
        strokeOpacity="0.4"
      />
      <path d="M10 70h320" stroke="#fff" strokeOpacity="0.4" />
      <rect
        x="26"
        y="50"
        width="60"
        height="9"
        rx="4.5"
        fill="#fff"
        fillOpacity="0.85"
      />
      <rect
        x="130"
        y="50"
        width="48"
        height="9"
        rx="4.5"
        fill="#fff"
        fillOpacity="0.55"
      />
      <rect
        x="240"
        y="50"
        width="40"
        height="9"
        rx="4.5"
        fill="#fff"
        fillOpacity="0.55"
      />
      {[118, 218].map((x) => (
        <path key={x} d={`M${x} 70v150`} stroke="#fff" strokeOpacity="0.2" />
      ))}
      {rows.map(([a, b, c], i) => {
        const y = 84 + i * 28;
        return (
          <g key={i}>
            <path d={`M10 ${y + 18}h320`} stroke="#fff" strokeOpacity="0.15" />
            <rect
              x="26"
              y={y}
              width={(a ?? 0) * 2}
              height="8"
              rx="4"
              fill="#fff"
              fillOpacity="0.5"
            />
            <rect
              x="130"
              y={y}
              width={(b ?? 0) * 2}
              height="8"
              rx="4"
              fill="#fff"
              fillOpacity="0.38"
            />
            <rect
              x="230"
              y={y}
              width={(c ?? 0) * 2.5}
              height="8"
              rx="4"
              fill="#fff"
              fillOpacity="0.28"
            />
          </g>
        );
      })}
      <rect
        x="10"
        y="110"
        width="320"
        height="28"
        fill="#fff"
        fillOpacity="0.1"
      />
      <rect
        x="10"
        y="110"
        width="3"
        height="28"
        fill="#fff"
        fillOpacity="0.8"
      />
    </svg>
  );
}

/** Split sign-in layout: a brand panel on the left (a compact header on narrow screens) and the form card on the right. */
export function AuthLayout({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
}) {
  return (
    <div className="grid min-h-full lg:grid-cols-[minmax(0,5fr)_minmax(0,6fr)]">
      <div className="bg-brand relative isolate overflow-hidden text-white">
        <div
          aria-hidden="true"
          className="absolute inset-0 -z-10 bg-black/40"
        />
        <div className="flex flex-col gap-10 px-6 py-5 lg:min-h-full lg:justify-between lg:p-12">
          <div className="flex items-center gap-3">
            <span className="rounded-lg shadow-md ring-1 ring-white/40">
              <LogoMark size={32} />
            </span>
            <span className="text-lg font-semibold tracking-tight">
              AbiData
            </span>
          </div>
          <div className="hidden lg:block">
            <GridMotif />
            <p className="mt-8 max-w-sm text-3xl font-semibold leading-tight tracking-tight">
              Your data, organised and in reach.
            </p>
            <p className="mt-3 max-w-sm text-sm text-white/85">
              A calm, keyboard-first workspace for building and running database
              applications.
            </p>
            <ul className="mt-6 space-y-3 text-sm">
              {FEATURES.map(({ icon: Icon, text }) => (
                <li key={text} className="flex items-start gap-3">
                  <span className="mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-white/15">
                    <Icon size={14} aria-hidden="true" />
                  </span>
                  <span className="text-white/90">{text}</span>
                </li>
              ))}
            </ul>
          </div>
          <p className="hidden items-center gap-2 text-xs text-white/75 lg:flex">
            <Database size={12} aria-hidden="true" /> Self-hosted. Your data
            stays with you.
          </p>
        </div>
      </div>
      <main className="flex items-center justify-center bg-bg px-4 py-8 sm:p-8">
        <div className="w-full max-w-md rounded-xl border border-border bg-surface p-6 shadow-md sm:p-8">
          <h1 className="text-2xl font-semibold tracking-tight text-fg">
            {title}
          </h1>
          {subtitle && (
            <p className="mb-6 mt-1.5 text-sm text-fg-muted">{subtitle}</p>
          )}
          {!subtitle && <div className="mb-6" />}
          {children}
        </div>
      </main>
    </div>
  );
}

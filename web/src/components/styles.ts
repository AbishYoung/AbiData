// Shared class strings for the design system (see DESIGN.md next to this file).
// Existing export names are kept; prefer these over ad-hoc classes so the app
// stays consistent.

const base =
  "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium whitespace-nowrap select-none " +
  "transition-[background-color,border-color,color,box-shadow] duration-150 " +
  "disabled:opacity-55 disabled:cursor-not-allowed disabled:shadow-none cursor-pointer [&_svg]:shrink-0";
const size = "h-8 px-3";

/** Secondary button: the default for most actions. */
export const btn = `${base} ${size} border border-border bg-surface text-fg shadow-xs hover:bg-surface-2 hover:border-border-strong/60 active:bg-surface-3`;
/** The one main action of a view or dialog. */
export const btnPrimary = `${base} ${size} bg-accent text-accent-fg shadow-sm hover:bg-accent-hover active:brightness-95`;
/** Low-emphasis action (toolbars, inline). */
export const btnGhost = `${base} ${size} text-fg-muted hover:bg-surface-2 hover:text-fg active:bg-surface-3`;
/** Destructive confirmation. */
export const btnDanger = `${base} ${size} bg-danger text-danger-fg shadow-sm hover:brightness-110 active:brightness-95`;
/** Smaller variants for dense toolbars and table rows. */
export const btnSm = `${base} h-7 px-2.5 text-xs border border-border bg-surface text-fg shadow-xs hover:bg-surface-2`;
export const btnGhostSm = `${base} h-7 px-2 text-xs text-fg-muted hover:bg-surface-2 hover:text-fg`;
/** Square icon-only button; always give it an aria-label. */
export const iconBtn = `${base} h-8 w-8 p-0 text-fg-muted hover:bg-surface-2 hover:text-fg active:bg-surface-3`;
export const iconBtnSm = `${base} h-6 w-6 p-0 rounded text-fg-muted hover:bg-surface-3 hover:text-fg`;

export const input =
  "w-full h-8 rounded-md border border-border-strong/70 bg-surface px-3 text-sm text-fg shadow-xs placeholder:text-fg-subtle " +
  "transition-[border-color,box-shadow] duration-150 hover:border-border-strong " +
  "focus:border-accent focus:outline-none focus:ring-3 focus:ring-accent/20 focus-visible:outline-none " +
  "disabled:bg-surface-2 disabled:text-fg-muted disabled:cursor-not-allowed aria-[invalid=true]:border-danger aria-[invalid=true]:ring-danger/20";
/** Multi-line input: same look, natural height. */
export const textarea = input.replace("h-8 ", "min-h-20 py-2 ");
export const select = `${input} pr-8 appearance-auto`;
export const label = "mb-1.5 block text-sm font-medium text-fg";
export const hint = "mt-1 text-xs text-fg-muted";
export const fieldError = "mt-1 text-xs font-medium text-danger";

export const alertBox = "rounded-lg border border-danger/40 bg-danger-soft px-3 py-2 text-sm text-fg";
export const warningBox = "rounded-lg border border-warning/40 bg-warning-soft px-3 py-2 text-sm text-fg";
export const infoBox = "rounded-lg border border-accent/30 bg-accent-soft px-3 py-2 text-sm text-fg";
export const successBox = "rounded-lg border border-success/40 bg-success-soft px-3 py-2 text-sm text-fg";

/** Raised container for grouped content. */
export const card = "rounded-xl border border-border bg-surface shadow-sm";
export const cardHeader = "flex items-center justify-between gap-3 border-b border-border px-4 py-3";
export const cardBody = "p-4";
/** Section heading inside a page or card. */
export const sectionTitle = "text-sm font-semibold text-fg";
export const pageTitle = "text-xl font-semibold tracking-tight text-fg";
/** Small uppercase label for groups (nav sections, table captions). */
export const eyebrow = "text-xs font-semibold uppercase tracking-wider text-fg-subtle";

/** Pills for status and roles. */
const pill = "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap";
export const badge = `${pill} bg-surface-3 text-fg-muted`;
export const badgeAccent = `${pill} bg-accent-soft text-accent`;
export const badgeSuccess = `${pill} bg-success-soft text-success`;
export const badgeWarning = `${pill} bg-warning-soft text-warning`;
export const badgeDanger = `${pill} bg-danger-soft text-danger`;

/** Keyboard key hint. */
export const kbd = "inline-flex h-5 min-w-5 items-center justify-center rounded border border-border bg-surface-2 px-1 font-mono text-xs text-fg-muted";

/** Data tables outside the datasheet (lists in settings, admin, runs). */
export const table = "w-full border-collapse text-sm";
export const th = "border-b border-border bg-surface-2 px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-fg-subtle";
export const td = "border-b border-border px-3 py-2 align-middle";
export const trHover = "hover:bg-surface-2/60";

/** Menus and popovers (Radix content). */
export const menu = "min-w-44 rounded-lg border border-border bg-surface p-1 shadow-lg";
export const menuItem =
  "flex cursor-pointer select-none items-center gap-2 rounded-md px-2 py-1.5 text-sm text-fg outline-none " +
  "data-[highlighted]:bg-surface-2 data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50";
export const menuSeparator = "my-1 h-px bg-border";

/** Dialog surfaces (Radix). */
export const overlay = "fixed inset-0 z-40 bg-scrim/40 backdrop-blur-[2px]";
export const dialog = "fixed left-1/2 top-1/2 z-50 max-h-[85vh] w-[min(92vw,32rem)] -translate-x-1/2 -translate-y-1/2 overflow-auto rounded-xl border border-border bg-surface shadow-lg";

/** Dialog chrome (DESIGN.md "Dialogs"). Pair `dialogOverlay` with `dialogPanel` and give the content `p-5`. */
export const dialogOverlay = "ui-overlay fixed inset-0 z-40 bg-scrim/45 backdrop-blur-[2px]";
/** Centered dialog surface; set the width with a `w-[min(26rem,calc(100vw-2rem))]` class. */
export const dialogPanel =
  "ui-dialog fixed left-1/2 top-1/2 z-50 max-h-[calc(100dvh-2rem)] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl border border-border bg-surface text-fg shadow-lg";
export const dialogTitle = "m-0 text-base font-semibold tracking-tight text-fg";
export const dialogDescription = "mt-1 text-sm text-fg-muted";
/** Footer strip for dialog content padded with `p-5`. */
export const dialogFooter =
  "-mx-5 -mb-5 mt-5 flex flex-wrap justify-end gap-2 rounded-b-xl border-t border-border bg-surface-2/50 px-5 py-3 compact:sticky compact:bottom-[-1.25rem] compact:z-10 compact:pb-[max(0.75rem,env(safe-area-inset-bottom))]";
/** Right-anchored side sheet (docs, history). */
export const sheetPanel =
  "ui-sheet fixed inset-y-0 right-0 z-50 flex w-[min(92vw,28rem)] flex-col border-l border-border bg-surface text-fg shadow-lg";
/** Small tinted square for a semantic dialog icon. */
export const iconChip = "inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg";

/** The document toolbar at the top of an editor tab (DESIGN.md "Document toolbar"). */
export const docToolbar = "flex min-h-11 flex-wrap items-center gap-1.5 border-b border-border bg-surface px-3 py-1.5";
/** Slim footer bar under an editor (counts, status). */
export const docFooter = "flex min-h-8 flex-wrap items-center gap-x-4 gap-y-1 border-t border-border bg-surface-2 px-3 py-1 text-xs text-fg-muted";

import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { EmptyState } from "@/components/ui";
import { DeerIllustration, FoxIllustration, OwlIllustration } from "./index";

const css = (globalThis as unknown as { process: { getBuiltinModule: (id: string) => unknown } }).process.getBuiltinModule("node:fs") as {
  readFileSync: (p: string, enc: string) => string;
};

describe("wildlife illustrations", () => {
  it.each([
    ["deer", DeerIllustration],
    ["owl", OwlIllustration],
    ["fox", FoxIllustration],
  ] as const)("%s is a decorative inline SVG using only currentColor", (name, Art) => {
    const { container } = render(<Art size={80} />);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg?.getAttribute("aria-hidden")).toBe("true");
    expect(svg?.getAttribute("data-wildlife")).toBe(name);
    expect(svg?.getAttribute("width")).toBe("80");
    expect(svg?.querySelectorAll("path").length).toBeGreaterThan(2);
    expect(container.innerHTML).not.toMatch(/https?:|href=|#[0-9a-f]{3,6}\b|rgb\(/i);
    expect(svg?.getAttribute("class")).toContain("wl-anim");
  });

  it("can be rendered without animation", () => {
    const { container } = render(<OwlIllustration animated={false} />);
    expect(container.querySelector("svg")?.getAttribute("class")).not.toContain("wl-anim");
  });

  it("keeps every animation inside prefers-reduced-motion: no-preference", () => {
    const sheet = css.readFileSync("src/styles/wildlife.css", "utf8");
    const outside = sheet.replace(/@media \(prefers-reduced-motion: no-preference\) \{[\s\S]*?\n\}\n/, "");
    expect(outside).not.toMatch(/animation\s*:/);
    expect(sheet).toContain("prefers-reduced-motion: no-preference");
  });
});

describe("EmptyState wildlife", () => {
  it("shows the stag, owl and fox for tables, queries and jobs, and the kind icon otherwise", () => {
    expect(render(<EmptyState kind="table" title="t" />).container.querySelector('[data-wildlife="deer"]')).not.toBeNull();
    expect(render(<EmptyState kind="query" title="q" />).container.querySelector('[data-wildlife="owl"]')).not.toBeNull();
    expect(render(<EmptyState kind="job" title="j" />).container.querySelector('[data-wildlife="fox"]')).not.toBeNull();
    expect(render(<EmptyState kind="form" title="f" />).container.querySelector("[data-wildlife]")).toBeNull();
    expect(render(<EmptyState kind="table" art={false} title="t" />).container.querySelector("[data-wildlife]")).toBeNull();
  });
});

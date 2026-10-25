import { afterEach, describe, expect, it } from "vitest";
import { act, render } from "@testing-library/react";
import { useActivityStore } from "@/lib/activity";
import { ActivityBeam } from "./ActivityBeam";
import { LiveBeacon } from "./LiveBeacon";

const fs = (globalThis as unknown as { process: { getBuiltinModule: (id: string) => unknown } }).process.getBuiltinModule("node:fs") as {
  readFileSync: (p: string, enc: string) => string;
};

afterEach(() => act(() => useActivityStore.setState({ activities: [], operations: 0 })));

describe("ActivityBeam", () => {
  it("shows only while requests or operations are running, and is decorative", () => {
    const { container } = render(<ActivityBeam />);
    expect(container.querySelector(".laser-beam")).toBeNull();
    act(() => useActivityStore.setState({ operations: 1 }));
    const beam = container.querySelector(".laser-beam");
    expect(beam?.getAttribute("aria-hidden")).toBe("true");
    act(() => useActivityStore.setState({ operations: 0 }));
    expect(container.querySelector(".laser-beam")).toBeNull();
  });
});

describe("LiveBeacon", () => {
  it("carries its state as a data attribute and hides from assistive technology", () => {
    const { container } = render(<LiveBeacon state="reconnecting" />);
    const el = container.querySelector(".beacon");
    expect(el?.getAttribute("data-state")).toBe("reconnecting");
    expect(el?.getAttribute("aria-hidden")).toBe("true");
  });
});

describe("motion.css", () => {
  const sheet = fs.readFileSync("src/styles/motion.css", "utf8");
  it("applies every animation only under prefers-reduced-motion: no-preference", () => {
    const at = sheet.indexOf("@media (prefers-reduced-motion: no-preference)");
    expect(at).toBeGreaterThan(0);
    const end = sheet.indexOf("\n}\n", at);
    const outside = sheet.slice(0, at) + sheet.slice(end);
    expect(outside).not.toMatch(/animation\s*:/);
  });
});

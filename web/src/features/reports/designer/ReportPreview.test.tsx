import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReportInput } from "../reportsApi";
import { ReportPreview, draftKey } from "./ReportPreview";

const runs: ReportInput[] = [];
vi.mock("../ReportTab", () => ({
  DraftRun: ({ input }: { input: ReportInput }) => {
    runs.push(input);
    return <div data-testid="draft-run">{input.name}</div>;
  },
}));

const input = (name: string): ReportInput => ({
  name,
  source_kind: "table",
  source_id: "T1",
  layout: { width: 100, bands: { report_header: { height: 0 }, page_header: { height: 0 }, detail: { height: 20 }, page_footer: { height: 0 }, report_footer: { height: 0 } } } as ReportInput["layout"],
  page_setup: { paper: "letter", orientation: "portrait", margin_top: 0, margin_right: 0, margin_bottom: 0, margin_left: 0 },
});

afterEach(() => {
  cleanup();
  runs.length = 0;
});

describe("ReportPreview (unsaved drafts)", () => {
  it("runs the draft without asking to save", () => {
    render(<ReportPreview ws="w" input={input("Draft")} problems={[]} />);
    expect(screen.getByTestId("draft-run")).toHaveTextContent("Draft");
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  });

  it("lists problems instead of running a design that cannot be saved", () => {
    render(<ReportPreview ws="w" input={input("")} problems={["Enter a report name.", "Choose a record source."]} />);
    expect(screen.getByRole("status")).toHaveTextContent("Enter a report name.");
    expect(screen.getByRole("status")).toHaveTextContent("Choose a record source.");
    expect(screen.queryByTestId("draft-run")).toBeNull();
    expect(runs).toHaveLength(0);
  });

  it("reruns when the design changes and not otherwise", () => {
    const { rerender } = render(<ReportPreview ws="w" input={input("A")} problems={[]} />);
    rerender(<ReportPreview ws="w" input={{ ...input("A") }} problems={[]} />);
    expect(draftKey(input("A"))).toBe(draftKey(input("A")));
    expect(draftKey(input("A"))).not.toBe(draftKey(input("B")));
    rerender(<ReportPreview ws="w" input={input("B")} problems={[]} />);
    expect(screen.getByTestId("draft-run")).toHaveTextContent("B");
  });
});

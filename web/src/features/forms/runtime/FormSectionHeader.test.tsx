import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FormSectionHeader, type FormSectionMeta } from "./FormSectionHeader";

afterEach(cleanup);

describe("FormSectionHeader", () => {
  const meta: FormSectionMeta = {
    id: "general",
    title: "General Information",
    collapsible: true,
    defaultExpanded: true,
    badgeCount: 4,
    badgeLabel: "fields",
  };

  it("renders accessible button with ARIA attributes and title", () => {
    const onToggle = vi.fn();
    render(<FormSectionHeader section={meta} isExpanded={true} onToggle={onToggle} />);

    const button = screen.getByRole("button", { name: /General Information/i });
    expect(button).toBeInTheDocument();
    expect(button).toHaveAttribute("id", "section-general-header");
    expect(button).toHaveAttribute("aria-controls", "section-general-content");
    expect(button).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("4 fields")).toBeInTheDocument();
  });

  it("reflects collapsed state in aria-expanded", () => {
    const onToggle = vi.fn();
    render(<FormSectionHeader section={meta} isExpanded={false} onToggle={onToggle} />);

    const button = screen.getByRole("button", { name: /General Information/i });
    expect(button).toHaveAttribute("aria-expanded", "false");
  });

  it("toggles expand/collapse on click", () => {
    const onToggle = vi.fn();
    render(<FormSectionHeader section={meta} isExpanded={false} onToggle={onToggle} />);

    const button = screen.getByRole("button", { name: /General Information/i });
    fireEvent.click(button);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it("toggles expand/collapse on Enter and Space keys", () => {
    const onToggle = vi.fn();
    render(<FormSectionHeader section={meta} isExpanded={false} onToggle={onToggle} />);

    const button = screen.getByRole("button", { name: /General Information/i });
    fireEvent.keyDown(button, { key: "Enter" });
    expect(onToggle).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(button, { key: " " });
    expect(onToggle).toHaveBeenCalledTimes(2);
  });

  it("renders non-collapsible section without toggle", () => {
    const onToggle = vi.fn();
    const nonCollapsibleMeta: FormSectionMeta = {
      id: "summary",
      title: "Summary Details",
      collapsible: false,
    };
    render(<FormSectionHeader section={nonCollapsibleMeta} isExpanded={true} onToggle={onToggle} />);

    const button = screen.getByRole("button", { name: /Summary Details/i });
    expect(button).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(button);
    expect(onToggle).not.toHaveBeenCalled();
    fireEvent.keyDown(button, { key: "Enter" });
    expect(onToggle).not.toHaveBeenCalled();
  });
});

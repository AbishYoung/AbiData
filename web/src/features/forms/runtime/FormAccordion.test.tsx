import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FormAccordion, FormAccordionItem, type FormSectionMeta } from "./FormAccordion";

afterEach(cleanup);

describe("FormAccordion", () => {
  const sections: FormSectionMeta[] = [
    { id: "sec1", title: "General Info", collapsible: true, defaultExpanded: true, badgeCount: 3 },
    { id: "sec2", title: "Shipping Address", collapsible: true, defaultExpanded: false, badgeCount: 5 },
    { id: "sec3", title: "Billing Details", collapsible: true, defaultExpanded: false, badgeCount: 2 },
  ];

  it("renders headers and regions with correct ARIA attributes", () => {
    render(
      <FormAccordion
        items={sections.map((s) => ({
          section: s,
          content: <div>Content for {s.title}</div>,
        }))}
      />,
    );

    const btn1 = screen.getByRole("button", { name: /General Info/i });
    const btn2 = screen.getByRole("button", { name: /Shipping Address/i });

    expect(btn1).toHaveAttribute("id", "section-sec1-header");
    expect(btn1).toHaveAttribute("aria-controls", "section-sec1-content");
    expect(btn1).toHaveAttribute("aria-expanded", "true");

    expect(btn2).toHaveAttribute("id", "section-sec2-header");
    expect(btn2).toHaveAttribute("aria-controls", "section-sec2-content");
    expect(btn2).toHaveAttribute("aria-expanded", "false");

    const regions = screen.getAllByRole("region");
    expect(regions).toHaveLength(3);
    expect(regions[0]).toHaveAttribute("id", "section-sec1-content");
    expect(regions[0]).toHaveAttribute("aria-labelledby", "section-sec1-header");
    expect(regions[1]).toHaveAttribute("id", "section-sec2-content");
    expect(regions[1]).toHaveAttribute("aria-labelledby", "section-sec2-header");
  });

  it("toggles section expand/collapse on click", () => {
    render(
      <FormAccordion
        items={sections.map((s) => ({
          section: s,
          content: <div>Content for {s.title}</div>,
        }))}
      />,
    );

    const btn2 = screen.getByRole("button", { name: /Shipping Address/i });
    expect(btn2).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(btn2);
    expect(btn2).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(btn2);
    expect(btn2).toHaveAttribute("aria-expanded", "false");
  });

  it("supports keyboard navigation between headers (ArrowDown, ArrowUp, Home, End)", () => {
    render(
      <FormAccordion
        items={sections.map((s) => ({
          section: s,
          content: <div>Content for {s.title}</div>,
        }))}
      />,
    );

    const btn1 = screen.getByRole("button", { name: /General Info/i });
    const btn2 = screen.getByRole("button", { name: /Shipping Address/i });
    const btn3 = screen.getByRole("button", { name: /Billing Details/i });

    btn1.focus();
    expect(document.activeElement).toBe(btn1);

    // ArrowDown moves to next header
    fireEvent.keyDown(btn1, { key: "ArrowDown" });
    expect(document.activeElement).toBe(btn2);

    // ArrowDown moves to third header
    fireEvent.keyDown(btn2, { key: "ArrowDown" });
    expect(document.activeElement).toBe(btn3);

    // ArrowDown wraps to first header
    fireEvent.keyDown(btn3, { key: "ArrowDown" });
    expect(document.activeElement).toBe(btn1);

    // ArrowUp wraps to last header
    fireEvent.keyDown(btn1, { key: "ArrowUp" });
    expect(document.activeElement).toBe(btn3);

    // ArrowUp moves to second header
    fireEvent.keyDown(btn3, { key: "ArrowUp" });
    expect(document.activeElement).toBe(btn2);

    // Home jumps to first header
    fireEvent.keyDown(btn2, { key: "Home" });
    expect(document.activeElement).toBe(btn1);

    // End jumps to last header
    fireEvent.keyDown(btn1, { key: "End" });
    expect(document.activeElement).toBe(btn3);
  });

  it("supports Enter and Space key toggle on headers", () => {
    render(
      <FormAccordion
        items={sections.map((s) => ({
          section: s,
          content: <div>Content for {s.title}</div>,
        }))}
      />,
    );

    const btn2 = screen.getByRole("button", { name: /Shipping Address/i });
    expect(btn2).toHaveAttribute("aria-expanded", "false");

    fireEvent.keyDown(btn2, { key: "Enter" });
    expect(btn2).toHaveAttribute("aria-expanded", "true");

    fireEvent.keyDown(btn2, { key: " " });
    expect(btn2).toHaveAttribute("aria-expanded", "false");
  });

  it("triggers auto-expansion callback when an error is flagged inside a section", () => {
    const onAutoExpand = vi.fn();
    const onToggle = vi.fn();
    const targetSection = sections[1] as FormSectionMeta;

    const { rerender } = render(
      <FormAccordionItem
        section={targetSection}
        isExpanded={false}
        onToggle={onToggle}
        onAutoExpand={onAutoExpand}
        hasError={false}
      >
        <div>Content</div>
      </FormAccordionItem>,
    );

    expect(onToggle).not.toHaveBeenCalled();
    expect(onAutoExpand).not.toHaveBeenCalled();

    // Flag error inside section
    rerender(
      <FormAccordionItem
        section={targetSection}
        isExpanded={false}
        onToggle={onToggle}
        onAutoExpand={onAutoExpand}
        hasError={true}
      >
        <div>Content</div>
      </FormAccordionItem>,
    );

    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onAutoExpand).toHaveBeenCalledWith("sec2");
  });

  it("supports controlled expandedSections state and onToggleSection callback", () => {
    const onToggleSection = vi.fn();
    const expanded = { sec1: false, sec2: true, sec3: false };

    render(
      <FormAccordion
        expandedSections={expanded}
        onToggleSection={onToggleSection}
        items={sections.map((s) => ({
          section: s,
          content: <div>Content for {s.title}</div>,
        }))}
      />,
    );

    const btn1 = screen.getByRole("button", { name: /General Info/i });
    const btn2 = screen.getByRole("button", { name: /Shipping Address/i });

    expect(btn1).toHaveAttribute("aria-expanded", "false");
    expect(btn2).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(btn1);
    expect(onToggleSection).toHaveBeenCalledWith("sec1");
  });
});

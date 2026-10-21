import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RecordSheet } from "./RecordSheet";
import { col, row } from "./testutil";
import { useConfirmStore } from "@/components/ui/confirm";
import { newBuffer } from "./buffer";

// Ensure PointerEvent is defined in jsdom environment
if (typeof window.PointerEvent === "undefined") {
  class MockPointerEvent extends MouseEvent {
    pointerId: number;
    pointerType: string;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 1;
      this.pointerType = params.pointerType ?? "touch";
    }
  }
  // @ts-expect-error mock assignment
  window.PointerEvent = MockPointerEvent;
  // @ts-expect-error mock assignment
  globalThis.PointerEvent = MockPointerEvent;
}

function dispatchPointer(
  target: EventTarget,
  type: string,
  clientX: number,
  clientY: number,
  pointerId = 1,
  button = 0,
) {
  const event = new PointerEvent(type, {
    bubbles: true,
    cancelable: true,
    clientX,
    clientY,
    pointerId,
    button,
  });
  target.dispatchEvent(event);
  return event;
}

const columns = [
  col("c_name", "short_text", { display_name: "Customer Name" }),
  col("c_city", "short_text", { display_name: "City" }),
];

describe("RecordSheet mobile touch navigation & ergonomics", () => {
  beforeEach(() => {
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 400 });
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("renders record fields, header controls, and pagination indicator dots", () => {
    const onGo = vi.fn();
    const onClose = vi.fn();
    const onSave = vi.fn().mockResolvedValue("saved");

    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={1}
        total={5}
        isNew={false}
        row={row(2, 1, { c_name: "Hanari Carnes", c_city: "Rio de Janeiro" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={onSave}
        onGo={onGo}
        onClose={onClose}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    expect(screen.getByText("Record 2 of 5")).toBeInTheDocument();
    expect(screen.getByDisplayValue("Hanari Carnes")).toBeInTheDocument();

    // Check pagination dots are rendered
    const tablist = screen.getByRole("tablist", { name: "Record pagination" });
    expect(tablist).toBeInTheDocument();
    const dots = screen.getAllByRole("tab");
    expect(dots).toHaveLength(5);
    expect(dots[1]).toHaveAttribute("aria-selected", "true");
  });

  it("clicking pagination dots navigates to the selected record via onGo", async () => {
    const user = userEvent.setup();
    const onGo = vi.fn();

    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={0}
        total={3}
        isNew={false}
        row={row(1, 1, { c_name: "Customer 1", c_city: "City 1" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={onGo}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    const dots = screen.getAllByRole("tab");
    const dot = dots[2];
    if (!dot) throw new Error("Expected dot at index 2");
    await user.click(dot);
    expect(onGo).toHaveBeenCalledWith(2);
  });

  it("swipes left to step to the next record", () => {
    const onGo = vi.fn();

    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={1}
        total={5}
        isNew={false}
        row={row(2, 1, { c_name: "Customer 2", c_city: "City 2" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={onGo}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    const form = document.querySelector("form.swipe-card") as HTMLFormElement;
    expect(form).toBeInTheDocument();

    // Swipe left (finger moves right-to-left: from 300px to 150px)
    dispatchPointer(form, "pointerdown", 300, 100);
    dispatchPointer(window, "pointermove", 150, 100);
    dispatchPointer(window, "pointerup", 150, 100);

    // Navigates to next record (index + 1 = 2)
    expect(onGo).toHaveBeenCalledWith(2);
  });

  it("swipes right to step to the previous record", () => {
    const onGo = vi.fn();

    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={2}
        total={5}
        isNew={false}
        row={row(3, 1, { c_name: "Customer 3", c_city: "City 3" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={onGo}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    const form = document.querySelector("form.swipe-card") as HTMLFormElement;
    expect(form).toBeInTheDocument();

    // Swipe right (finger moves left-to-right: from 100px to 250px)
    dispatchPointer(form, "pointerdown", 100, 100);
    dispatchPointer(window, "pointermove", 250, 100);
    dispatchPointer(window, "pointerup", 250, 100);

    // Navigates to previous record (index - 1 = 1)
    expect(onGo).toHaveBeenCalledWith(1);
  });

  it("snaps back and does not navigate when swiping right on first record (boundary)", () => {
    const onGo = vi.fn();

    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={0}
        total={5}
        isNew={false}
        row={row(1, 1, { c_name: "Customer 1", c_city: "City 1" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={onGo}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    const form = document.querySelector("form.swipe-card") as HTMLFormElement;

    // Swipe right on record 0: boundary
    dispatchPointer(form, "pointerdown", 100, 100);
    dispatchPointer(window, "pointermove", 250, 100);
    dispatchPointer(window, "pointerup", 250, 100);

    expect(onGo).not.toHaveBeenCalled();
  });

  it("guards unsaved changes on swipe with confirmation dialog", async () => {
    const user = userEvent.setup();
    const onGo = vi.fn();
    const askSpy = vi.spyOn(useConfirmStore.getState(), "ask").mockResolvedValue(false);

    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={1}
        total={5}
        isNew={false}
        row={row(2, 1, { c_name: "Customer 2", c_city: "City 2" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={onGo}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    // Make an edit so record is dirty
    const input = screen.getByDisplayValue("Customer 2");
    await user.clear(input);
    await user.type(input, "Changed Name");

    const form = document.querySelector("form.swipe-card") as HTMLFormElement;

    // Attempt swipe left
    dispatchPointer(form, "pointerdown", 300, 100);
    dispatchPointer(window, "pointermove", 150, 100);
    dispatchPointer(window, "pointerup", 150, 100);

    // Confirmation was requested
    expect(askSpy).toHaveBeenCalledTimes(1);
    // User cancelled (ask resolved false), so onGo was NOT called
    expect(onGo).not.toHaveBeenCalled();

    // Now if user confirms
    askSpy.mockResolvedValueOnce(true);
    dispatchPointer(form, "pointerdown", 300, 100);
    dispatchPointer(window, "pointermove", 150, 100);
    dispatchPointer(window, "pointerup", 150, 100);

    // Wait microtask for async confirmation
    await vi.waitFor(() => {
      expect(onGo).toHaveBeenCalledWith(2);
    });
  });

  it("guards unsaved changes when buffer has changes", async () => {
    const onGo = vi.fn();
    const askSpy = vi.spyOn(useConfirmStore.getState(), "ask").mockResolvedValue(false);

    const buf = newBuffer(2, row(2, 1, { c_name: "Customer 2" }));
    buf.changes = { c_name: "Edited in Buffer" };

    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={1}
        total={5}
        isNew={false}
        row={row(2, 1, { c_name: "Customer 2", c_city: "City 2" })}
        canEdit={true}
        buffer={buf}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={onGo}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    const form = document.querySelector("form.swipe-card") as HTMLFormElement;

    dispatchPointer(form, "pointerdown", 300, 100);
    dispatchPointer(window, "pointermove", 150, 100);
    dispatchPointer(window, "pointerup", 150, 100);

    expect(askSpy).toHaveBeenCalledTimes(1);
    expect(onGo).not.toHaveBeenCalled();
  });

  it("dismisses virtual keyboard (blurs active input) when tapping non-interactive background", async () => {
    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={0}
        total={1}
        isNew={false}
        row={row(1, 1, { c_name: "Customer 1", c_city: "City 1" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={vi.fn()}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    const input = screen.getByDisplayValue("Customer 1") as HTMLInputElement;
    input.focus();
    expect(document.activeElement).toBe(input);

    const content = document.querySelector("[data-presentation='fullscreen']") as HTMLElement;
    expect(content).toBeInTheDocument();

    // Tap on the backdrop / container background
    fireEvent.pointerDown(content);

    // Input should be blurred
    expect(document.activeElement).not.toBe(input);
  });

  it("does NOT dismiss keyboard when tapping interactive elements (buttons or other inputs)", async () => {
    render(
      <RecordSheet
        tableName="Customers"
        columns={columns}
        index={0}
        total={1}
        isNew={false}
        row={row(1, 1, { c_name: "Customer 1", c_city: "City 1" })}
        canEdit={true}
        buffer={undefined}
        lookupFor={() => undefined}
        onSave={vi.fn().mockResolvedValue("saved")}
        onGo={vi.fn()}
        onClose={vi.fn()}
        onDelete={vi.fn()}
        onHistory={vi.fn()}
      />,
    );

    const input = screen.getByDisplayValue("Customer 1") as HTMLInputElement;
    input.focus();
    expect(document.activeElement).toBe(input);

    const closeBtn = screen.getByRole("button", { name: "Close record" });

    // Pointerdown on the close button
    fireEvent.pointerDown(closeBtn);

    // Should NOT blur the input prematurely before click is processed
    expect(document.activeElement).toBe(input);
  });
});

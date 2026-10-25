import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RecordSheet } from "./RecordSheet";
import { dragOffset, shouldDismiss } from "./useDragToDismiss";
import { col, row } from "./testutil";
import { useConfirmStore } from "@/components/ui/confirm";

describe("drag maths", () => {
  it("follows the finger down and damps upward drags", () => {
    expect(dragOffset(120)).toBe(120);
    expect(dragOffset(-100)).toBeCloseTo(-20);
  });
  it("dismisses past 25% of the height or on a fast downward release", () => {
    expect(shouldDismiss(100, 800, 0.1)).toBe(false);
    expect(shouldDismiss(201, 800, 0.1)).toBe(true);
    expect(shouldDismiss(40, 800, 0.6)).toBe(true);
    expect(shouldDismiss(-300, 800, 2)).toBe(false);
  });
});

const columns = [col("c_name", "short_text", { display_name: "Customer Name" })];

function sheet(onClose: () => void) {
  render(
    <RecordSheet
      tableName="Customers"
      columns={columns}
      index={0}
      total={1}
      isNew={false}
      row={row(1, 1, { c_name: "Alpha" })}
      canEdit
      buffer={undefined}
      lookupFor={() => undefined}
      onSave={vi.fn().mockResolvedValue("saved")}
      onGo={vi.fn()}
      onClose={onClose}
      onDelete={vi.fn()}
      onHistory={vi.fn()}
    />,
  );
}

function dragHeader(dy: number) {
  const bar = screen.getByTestId("sheet-grab-bar").parentElement as HTMLElement;
  // A slow drag (a second long), so only the distance decides.
  let now = 1000;
  vi.spyOn(performance, "now").mockImplementation(() => now);
  fireEvent.pointerDown(bar, { pointerId: 1, pointerType: "touch", button: 0, clientY: 50 });
  now += 900;
  fireEvent.pointerMove(bar, { pointerId: 1, pointerType: "touch", clientY: 50 + dy });
  now += 100;
  fireEvent.pointerUp(bar, { pointerId: 1, pointerType: "touch", clientY: 50 + dy });
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("RecordSheet drag to dismiss", () => {
  it("closes after a long downward drag on the grab bar", async () => {
    const onClose = vi.fn();
    sheet(onClose);
    dragHeader(400);
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
  it("springs back on a short drag", async () => {
    const onClose = vi.fn();
    sheet(onClose);
    dragHeader(30);
    await new Promise((r) => setTimeout(r, 20));
    expect(onClose).not.toHaveBeenCalled();
  });
  it("asks before discarding unsaved edits and stays open when declined", async () => {
    const onClose = vi.fn();
    const ask = vi.spyOn(useConfirmStore.getState(), "ask").mockResolvedValue(false);
    sheet(onClose);
    fireEvent.change(screen.getByDisplayValue("Alpha"), { target: { value: "Beta" } });
    dragHeader(400);
    await waitFor(() => expect(ask).toHaveBeenCalled());
    expect(onClose).not.toHaveBeenCalled();
  });
  it("keeps the Close button as the non-gesture path", () => {
    sheet(vi.fn());
    expect(screen.getByRole("button", { name: "Close record" })).toBeInTheDocument();
  });
});

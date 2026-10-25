import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ToastRegion, useToasts } from "./Toasts";

function Host({ sticky = false }: { sticky?: boolean }) {
  const { toasts, push, dismiss, pause, resume } = useToasts();
  return (
    <>
      <button type="button" onClick={() => push("success", "Saved", { sticky })}>
        push
      </button>
      <ToastRegion toasts={toasts} onDismiss={dismiss} onPause={pause} onResume={resume} />
    </>
  );
}

describe("toasts", () => {
  beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("shows a progress hairline that drains for auto-dismissing toasts only", async () => {
    const { unmount } = render(<Host />);
    await userEvent.setup({ advanceTimers: vi.advanceTimersByTime }).click(screen.getByText("push"));
    expect(screen.getByTestId("toast-progress")).toBeInTheDocument();
    unmount();
    render(<Host sticky />);
    await userEvent.setup({ advanceTimers: vi.advanceTimersByTime }).click(screen.getByText("push"));
    expect(screen.queryByTestId("toast-progress")).toBeNull();
  });

  it("holds the auto-dismiss while hovered and resumes afterwards", async () => {
    render(<Host />);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    await user.click(screen.getByText("push"));
    const text = screen.getByText("Saved");
    await user.hover(text);
    act(() => void vi.advanceTimersByTime(20000));
    expect(screen.getByText("Saved")).toBeInTheDocument();
    await user.unhover(text);
    act(() => void vi.advanceTimersByTime(7000));
    expect(screen.queryByText("Saved")).toBeNull();
  });
});

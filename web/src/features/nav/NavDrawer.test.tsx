import { afterEach, describe, expect, it } from "vitest";
import { useRef, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Dialog } from "radix-ui";
import { NavDrawer } from "./NavDrawer";

afterEach(cleanup);

/** A nav item that owns a dialog, like a rename action in the navigation pane. */
function Item() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Rename Products
      </button>
      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog.Portal>
          <Dialog.Content aria-describedby={undefined}>
            <Dialog.Title>Rename table</Dialog.Title>
            <button type="button" onClick={() => setOpen(false)}>
              Cancel
            </button>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}

function Harness() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button ref={ref} type="button" onClick={() => setOpen(true)}>
        Open navigation
      </button>
      <NavDrawer open={open} onOpenChange={setOpen} returnFocusRef={ref}>
        <nav aria-label="Navigation pane">
          <Item />
        </nav>
      </NavDrawer>
    </>
  );
}

describe("NavDrawer", () => {
  it("closes when an action in it opens a dialog, and the dialog survives", async () => {
    render(<Harness />);
    expect(screen.queryByRole("navigation", { name: "Navigation pane" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    expect(await screen.findByRole("navigation", { name: "Navigation pane" })).toBeInTheDocument();
    expect(screen.getByRole("dialog", { name: "Navigation" })).toBeInTheDocument();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Rename Products" }));
    });
    // The drawer is gone; the dialog it launched is still open.
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Navigation" })).toBeNull());
    expect(screen.queryByRole("navigation", { name: "Navigation pane" })).toBeNull();
    expect(screen.getByRole("dialog", { name: "Rename table" })).toBeInTheDocument();

    // Closing the dialog leaves the drawer closed.
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    });
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Rename table" })).toBeNull());
    expect(screen.queryByRole("dialog", { name: "Navigation" })).toBeNull();
  });

  it("keeps the drawer open when nothing opens a dialog", async () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    expect(await screen.findByRole("dialog", { name: "Navigation" })).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.getByRole("dialog", { name: "Navigation" })).toBeInTheDocument();
  });
});

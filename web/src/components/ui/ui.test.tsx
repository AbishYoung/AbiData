import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Badge, Button, Card, ConfirmDialog, ConfirmHost, Dialog, EmptyState, Field, IconButton, Input, Select, Sheet, Skeleton, Textarea, Toolbar, ToolbarSeparator, askToConfirm } from "./index";

afterEach(cleanup);

describe("Button", () => {
  it("is a plain button by default and takes a variant", () => {
    render(
      <form onSubmit={() => {}}>
        <Button>Cancel</Button>
        <Button variant="danger" type="submit">
          Delete
        </Button>
      </form>,
    );
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveAttribute("type", "button");
    const del = screen.getByRole("button", { name: "Delete" });
    expect(del).toHaveAttribute("type", "submit");
    expect(del.className).toContain("bg-danger");
  });
});

describe("IconButton", () => {
  it("names the button with its label and shows the same text as a tooltip on focus", async () => {
    render(
      <IconButton label="Refresh" shortcut="Mod+R">
        <svg />
      </IconButton>,
    );
    const b = screen.getByRole("button", { name: "Refresh" });
    expect(b).toHaveAttribute("type", "button");
    act(() => b.focus());
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Refresh");
  });
});

describe("Field", () => {
  it("labels the control and describes it with the hint", () => {
    render(
      <Field label="Name" hint="Shown in the navigation pane.">
        <Input />
      </Field>,
    );
    const input = screen.getByLabelText("Name");
    expect(input).toHaveAccessibleDescription("Shown in the navigation pane.");
    expect(input).not.toHaveAttribute("aria-invalid");
  });

  it("marks the control invalid and adds the error to its description", () => {
    render(
      <Field label="Rows" hint="At most 100." error="Enter a whole number." required>
        <Select>
          <option>1</option>
        </Select>
      </Field>,
    );
    const select = screen.getByRole("combobox", { name: "Rows" });
    expect(select).toHaveAttribute("aria-invalid", "true");
    expect(select).toBeRequired();
    expect(select).toHaveAccessibleDescription("At most 100. Enter a whole number.");
  });

  it("keeps a control's own id and description", () => {
    render(
      <>
        <p id="extra">Extra</p>
        <Field label="Notes" id="notes">
          <Textarea aria-describedby="extra" />
        </Field>
      </>,
    );
    const t = screen.getByLabelText("Notes");
    expect(t).toHaveAttribute("id", "notes");
    expect(t).toHaveAccessibleDescription("Extra");
  });

  it("leaves a control outside a field alone", () => {
    render(<Input aria-label="Search" />);
    expect(screen.getByRole("textbox", { name: "Search" })).not.toHaveAttribute("aria-describedby");
  });
});

describe("Dialog", () => {
  it("is a named modal dialog with a close button and a footer", async () => {
    const onClose = vi.fn();
    render(
      <Dialog title="Rename table" description="The new name shows everywhere." onClose={onClose} footer={<Button>Save</Button>}>
        <p>Body</p>
      </Dialog>,
    );
    const d = screen.getByRole("dialog", { name: "Rename table" });
    expect(d).toHaveAccessibleDescription("The new name shows everywhere.");
    expect(screen.getByRole("button", { name: "Save" })).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("focuses the first field when it opens, and Close comes last", () => {
    render(
      <Dialog title="Rename" onClose={() => {}} footer={<Button>Save</Button>}>
        <Field label="Name">
          <Input />
        </Field>
      </Dialog>,
    );
    expect(screen.getByLabelText("Name")).toHaveFocus();
    const buttons = screen.getAllByRole("button");
    expect(buttons.map((b) => b.textContent || b.getAttribute("aria-label"))).toEqual(["Save", "Close"]);
  });

  it("gives focus back to the button that opened it", async () => {
    function Opener() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <Button onClick={() => setOpen(true)}>New app</Button>
          {open && <Dialog title="New public app" onClose={() => setOpen(false)} />}
        </>
      );
    }
    render(<Opener />);
    await userEvent.click(screen.getByRole("button", { name: "New app" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.getByRole("button", { name: "New app" })).toHaveFocus());
  });

  it("closes on Escape", async () => {
    const onClose = vi.fn();
    render(<Dialog title="T" onClose={onClose} />);
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });
});

describe("Sheet", () => {
  it("is a named dialog that opts out of the compact bottom sheet", () => {
    render(
      <Sheet title="History" onClose={() => {}}>
        <p>Entries</p>
      </Sheet>,
    );
    expect(screen.getByRole("dialog", { name: "History" })).toHaveAttribute("data-presentation", "side");
  });
});

describe("ConfirmDialog", () => {
  it("is an alert dialog that starts on Cancel and confirms only on the confirm button", async () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(<ConfirmDialog title="Delete 2 records?" message="This cannot be undone." confirmLabel="Delete records" danger onConfirm={onConfirm} onCancel={onCancel} />);
    const d = screen.getByRole("alertdialog", { name: "Delete 2 records?" });
    expect(d).toHaveAccessibleDescription("This cannot be undone.");
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    await userEvent.click(screen.getByRole("button", { name: "Delete records" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onCancel).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("cannot be dismissed while busy", async () => {
    const onCancel = vi.fn();
    render(<ConfirmDialog title="Delete?" message="Gone." confirmLabel="Delete" busy onConfirm={() => {}} onCancel={onCancel} />);
    await userEvent.keyboard("{Escape}");
    expect(onCancel).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
  });
});

describe("askToConfirm", () => {
  it("resolves true on confirm and false on cancel", async () => {
    render(<ConfirmHost />);
    let p = askToConfirm({ title: "Close tab?", message: "Unsaved changes are lost.", confirmLabel: "Discard and close", danger: true });
    await userEvent.click(await screen.findByRole("button", { name: "Discard and close" }));
    await expect(p).resolves.toBe(true);
    expect(screen.queryByRole("alertdialog")).toBeNull();

    p = askToConfirm({ title: "Close tab?", message: "Unsaved changes are lost.", confirmLabel: "Discard and close" });
    await userEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await expect(p).resolves.toBe(false);
  });

  it("answers an unanswered request with false when a newer one replaces it", async () => {
    render(<ConfirmHost />);
    let first: Promise<boolean> = Promise.resolve(true);
    act(() => {
      first = askToConfirm({ title: "One", message: "m", confirmLabel: "OK" });
      void askToConfirm({ title: "Two", message: "m", confirmLabel: "OK" });
    });
    await expect(first).resolves.toBe(false);
    expect(screen.getByRole("alertdialog", { name: "Two" })).toBeInTheDocument();
  });
});

describe("Card, Badge, EmptyState, Skeleton, Toolbar", () => {
  it("renders a titled card section with actions", () => {
    render(
      <Card title="Members" actions={<Button>Invite</Button>} aria-label="Members">
        <p>List</p>
      </Card>,
    );
    expect(screen.getByRole("region", { name: "Members" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Members" })).toBeInTheDocument();
  });

  it("gives a badge its tone and keeps the text", () => {
    render(<Badge tone="success">Live</Badge>);
    expect(screen.getByText("Live").className).toContain("bg-success-soft");
  });

  it("renders an empty state with a title and an action", () => {
    render(<EmptyState title="No records yet" hint="Add the first one." action={<Button variant="primary">New record</Button>} />);
    expect(screen.getByText("No records yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New record" })).toBeInTheDocument();
  });

  it("announces a skeleton as a status", () => {
    render(<Skeleton label="Loading members…" rows={2} />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading members…");
  });

  it("is a named toolbar only when labelled; separators are hidden", () => {
    const { container } = render(
      <Toolbar label="Record actions">
        <Button>Save</Button>
        <ToolbarSeparator />
        <Button>Delete</Button>
      </Toolbar>,
    );
    expect(screen.getByRole("toolbar", { name: "Record actions" })).toBeInTheDocument();
    expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull();
    cleanup();
    render(
      <Toolbar>
        <Button>Save</Button>
      </Toolbar>,
    );
    expect(screen.queryByRole("toolbar")).toBeNull();
  });
});

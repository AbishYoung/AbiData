import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useTabs } from "@/app/tabs";
import { useMobileView } from "@/lib/mobileView";
import { MobileBottomBar, neighbourTab } from "./MobileBottomBar";

const tab = (id: string, title: string) => ({ key: `table:${id}`, kind: "table" as const, objectId: id, view: "datasheet" as const, title });

beforeEach(() => {
  useTabs.setState({ byWs: {}, openSeq: 0 });
  useMobileView.setState({ mode: "cards" });
});
afterEach(cleanup);

describe("neighbourTab", () => {
  it("stops at both ends", () => {
    expect(neighbourTab(["a", "b", "c"], "b", 1)).toBe("c");
    expect(neighbourTab(["a", "b", "c"], "b", -1)).toBe("a");
    expect(neighbourTab(["a", "b", "c"], "c", 1)).toBeNull();
    expect(neighbourTab(["a"], null, 1)).toBeNull();
  });
});

describe("MobileBottomBar", () => {
  it("opens the menu and toggles the view on a table", async () => {
    const onMenu = vi.fn();
    useTabs.getState().open("w", tab("1", "Customers"));
    render(<MobileBottomBar ws="w" onMenu={onMenu} />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Menu" }));
    expect(onMenu).toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Show records as grid" }));
    expect(useMobileView.getState().mode).toBe("grid");
    expect(screen.getByRole("button", { name: "Show records as cards" })).toBeInTheDocument();
  });

  it("swipes to the next and previous tab", () => {
    useTabs.getState().open("w", tab("1", "One"));
    useTabs.getState().open("w", tab("2", "Two"));
    useTabs.getState().activate("w", "table:1");
    render(<MobileBottomBar ws="w" onMenu={vi.fn()} />);
    const nav = screen.getByRole("navigation", { name: "Quick actions" });
    fireEvent.pointerDown(nav, { pointerType: "touch", clientX: 300, clientY: 10 });
    fireEvent.pointerUp(nav, { pointerType: "touch", clientX: 200, clientY: 12 });
    expect(useTabs.getState().byWs["w"]?.active).toBe("table:2");
    fireEvent.pointerDown(nav, { pointerType: "touch", clientX: 100, clientY: 10 });
    fireEvent.pointerUp(nav, { pointerType: "touch", clientX: 220, clientY: 10 });
    expect(useTabs.getState().byWs["w"]?.active).toBe("table:1");
  });
});

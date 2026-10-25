import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { Switch } from "./Switch";
import { Stepper, stepText } from "./Stepper";
import { pinchDirection } from "@/lib/usePinchZoom";

afterEach(cleanup);

describe("Switch", () => {
  it("is a labelled switch that toggles with the keyboard and a tap", async () => {
    const onChange = vi.fn();
    render(
      <label>
        Active
        <Switch checked={false} onChange={onChange} />
      </label>,
    );
    const sw = screen.getByRole("switch", { name: "Active" });
    await userEvent.setup().click(sw);
    expect(onChange).toHaveBeenCalledTimes(1);
    sw.focus();
    await userEvent.setup().keyboard(" ");
    expect(onChange).toHaveBeenCalledTimes(2);
  });
});

describe("stepText", () => {
  it("adds to whole numbers and treats blanks as zero", () => {
    expect(stepText("142", 1)).toBe("143");
    expect(stepText("", -1)).toBe("-1");
    expect(stepText("1,000", 1)).toBe("1001");
    expect(stepText("abc", 1)).toBe("1");
  });
});

function Host() {
  const [t, setT] = useState("5");
  return (
    <Stepper text={t} onText={setT} label="Count">
      <input aria-label="Count value" value={t} onChange={(e) => setT(e.target.value)} />
    </Stepper>
  );
}

describe("Stepper", () => {
  it("steps with a press and with the keyboard", async () => {
    render(<Host />);
    fireEvent.pointerDown(screen.getByRole("button", { name: "Increase Count" }), { button: 0 });
    fireEvent.pointerUp(screen.getByRole("button", { name: "Increase Count" }));
    expect(screen.getByLabelText("Count value")).toHaveValue("6");
    fireEvent.click(screen.getByRole("button", { name: "Decrease Count" }), { detail: 0 });
    expect(screen.getByLabelText("Count value")).toHaveValue("5");
  });
  it("repeats while held", () => {
    vi.useFakeTimers();
    render(<Host />);
    const plus = screen.getByRole("button", { name: "Increase Count" });
    fireEvent.pointerDown(plus, { button: 0 });
    act(() => {
      vi.advanceTimersByTime(400 + 160 + 140);
    });
    fireEvent.pointerUp(plus);
    const v = Number((screen.getByLabelText("Count value") as HTMLInputElement).value);
    vi.useRealTimers();
    expect(v).toBeGreaterThanOrEqual(7);
  });
});

describe("pinchDirection", () => {
  it("needs a clear change in finger distance", () => {
    expect(pinchDirection(100, 110)).toBe(0);
    expect(pinchDirection(100, 130)).toBe(1);
    expect(pinchDirection(100, 70)).toBe(-1);
    expect(pinchDirection(0, 70)).toBe(0);
  });
});

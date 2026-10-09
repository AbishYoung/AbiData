import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@/lib/api";
import { LoginForm } from "./LoginForm";

afterEach(cleanup);

describe("LoginForm", () => {
  it("renders labelled inputs and submits with Enter", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<LoginForm onSubmit={onSubmit} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Email"), "a@example.com");
    await user.type(screen.getByLabelText("Password"), "secret{Enter}");
    expect(onSubmit).toHaveBeenCalledWith("a@example.com", "secret");
  });

  it("shows the server error text in an alert", async () => {
    const onSubmit = vi.fn().mockRejectedValue(
      new ApiError({
        status: 429,
        code: "RATE_LIMITED",
        title: "Slow down",
        message: "Too many sign-in attempts.",
        recovery: "Wait a minute.",
      }),
    );
    render(<LoginForm onSubmit={onSubmit} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Email"), "a@example.com");
    await user.type(screen.getByLabelText("Password"), "x{Enter}");
    expect(await screen.findByRole("alert")).toHaveTextContent("Too many sign-in attempts.");
  });
});

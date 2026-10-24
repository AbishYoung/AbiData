import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { DialectBadge, DialectMismatch, dialectLabel } from "./dialect";

afterEach(cleanup);

describe("SQL dialect hints", () => {
  it("names the install's dialect", () => {
    render(<DialectBadge dialect="postgres" />);
    expect(screen.getByTestId("sql-dialect")).toHaveTextContent("PostgreSQL SQL");
    cleanup();
    render(<DialectBadge dialect="sqlite" />);
    expect(screen.getByTestId("sql-dialect")).toHaveTextContent("SQLite SQL");
  });

  it("shows nothing until the dialect is known", () => {
    const { container } = render(<DialectBadge dialect={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("warns about a saved query written in the other dialect only", () => {
    render(<DialectMismatch dialect="postgres" saved="sqlite" />);
    expect(screen.getByRole("status")).toHaveTextContent("written in SQLite SQL, but this install runs PostgreSQL");
    cleanup();
    const same = render(<DialectMismatch dialect="sqlite" saved="sqlite" />);
    expect(same.container).toBeEmptyDOMElement();
    cleanup();
    const unsaved = render(<DialectMismatch dialect="sqlite" saved={undefined} />);
    expect(unsaved.container).toBeEmptyDOMElement();
  });

  it("labels known and unknown dialects", () => {
    expect(dialectLabel("postgres")).toBe("PostgreSQL");
    expect(dialectLabel("sqlite")).toBe("SQLite");
    expect(dialectLabel("mysql")).toBe("mysql");
    expect(dialectLabel(undefined)).toBe("");
  });
});

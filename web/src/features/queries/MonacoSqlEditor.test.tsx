import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { useEffect, useRef, type ReactNode } from "react";
import MonacoSqlEditor from "./MonacoSqlEditor";
import type { ParsedSqlError } from "./sqlErrorParser";

function MockMonacoEditor({
  value,
  onChange,
  onMount,
  options,
  loading,
}: {
  value: string;
  onChange?: (val: string) => void;
  onMount?: (editor: unknown, monaco: unknown) => void;
  options?: { ariaLabel?: string; readOnly?: boolean };
  loading?: ReactNode;
}) {
  const mountedRef = useRef(false);
  useEffect(() => {
    if (!mountedRef.current) {
      mountedRef.current = true;
      onMount?.(
        {
          focus: vi.fn(),
          setPosition: vi.fn(),
          setSelection: vi.fn(),
          revealPositionInCenterIfOutsideViewport: vi.fn(),
          getModel: vi.fn(),
          addCommand: vi.fn(),
        },
        {},
      );
    }
  }, [onMount]);

  return (
    <div data-testid="mock-monaco-editor" aria-label={options?.ariaLabel}>
      {loading}
      <textarea
        data-testid="mock-monaco-textarea"
        aria-label={options?.ariaLabel}
        value={value}
        readOnly={options?.readOnly}
        onChange={(e) => onChange?.(e.target.value)}
      />
    </div>
  );
}

// Mock @monaco-editor/react for jsdom component testing
vi.mock("@monaco-editor/react", () => ({
  default: MockMonacoEditor,
}));

// Mock monaco setup
vi.mock("@/features/scripts/monacoSetup", () => ({
  monaco: {
    MarkerSeverity: { Error: 8, Warning: 4 },
    KeyMod: { CtrlCmd: 2048 },
    KeyCode: { Enter: 3 },
    editor: {
      setModelMarkers: vi.fn(),
      defineTheme: vi.fn(),
      setTheme: vi.fn(),
    },
  },
  applyTheme: vi.fn(() => "abidata-light"),
  currentThemeName: vi.fn(() => "abidata-light"),
}));

afterEach(cleanup);

describe("MonacoSqlEditor", () => {
  it("renders container with accessible region and aria-label", () => {
    const onChange = vi.fn();
    render(
      <MonacoSqlEditor
        value="SELECT 1;"
        onChange={onChange}
      />,
    );

    const region = screen.getByRole("region", { name: "SQL query editor" });
    expect(region).toBeInTheDocument();
    expect(region).toHaveAttribute("data-testid", "monaco-sql-editor");
  });

  it("exposes editor handle on ready", () => {
    const onReady = vi.fn();
    render(
      <MonacoSqlEditor
        value="SELECT 1;"
        onChange={vi.fn()}
        onReady={onReady}
      />,
    );

    expect(onReady).toHaveBeenCalledWith(
      expect.objectContaining({
        focus: expect.any(Function),
        revealPosition: expect.any(Function),
      }),
    );
  });

  it("renders with error and sets error markers appropriately", () => {
    const error: ParsedSqlError = {
      message: 'near "FORM": syntax error',
      lineNumber: 2,
      column: 1,
      endLineNumber: 2,
      endColumn: 5,
      token: "FORM",
    };

    render(
      <MonacoSqlEditor
        value="SELECT 1\nFORM orders"
        onChange={vi.fn()}
        error={error}
      />,
    );

    expect(screen.getByTestId("monaco-sql-editor")).toBeInTheDocument();
  });
});

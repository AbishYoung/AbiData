import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { isAllowedUrl, MarkdownBody } from "./MarkdownBody";

afterEach(cleanup);

describe("isAllowedUrl", () => {
  it("permits http and https URLs", () => {
    expect(isAllowedUrl("http://example.com")).toBe(true);
    expect(isAllowedUrl("https://example.com/path?q=1")).toBe(true);
  });

  it("permits relative paths and fragments", () => {
    expect(isAllowedUrl("/api/v1/w/demo/files/123/content")).toBe(true);
    expect(isAllowedUrl("#heading")).toBe(true);
  });

  it("neutralizes dangerous protocols", () => {
    expect(isAllowedUrl("javascript:alert(1)")).toBe(false);
    expect(isAllowedUrl("javascript:/*--></title></style>alert(1)")).toBe(false);
    expect(isAllowedUrl("data:text/html,<script>alert(1)</script>")).toBe(false);
    expect(isAllowedUrl("vbscript:msgbox(1)")).toBe(false);
  });
});

describe("MarkdownBody", () => {
  it("renders bold and italics formatting", () => {
    render(<MarkdownBody content="This is **bold text** and this is *italic text*." />);
    const boldEl = screen.getByText("bold text");
    expect(boldEl.tagName).toBe("STRONG");
    expect(boldEl).toHaveClass("font-semibold");

    const italicEl = screen.getByText("italic text");
    expect(italicEl.tagName).toBe("EM");
    expect(italicEl).toHaveClass("italic");
  });

  it("renders inline code with custom styling", () => {
    render(<MarkdownBody content="Run `SELECT 1` in the console." />);
    const codeEl = screen.getByText("SELECT 1");
    expect(codeEl.tagName).toBe("CODE");
    expect(codeEl).toHaveClass("font-mono", "text-xs", "bg-surface-3", "px-1.5", "py-0.5", "rounded", "text-accent");
  });

  it("renders fenced SQL blocks with keyword highlighting and copy button", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    if (!navigator.clipboard) {
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        configurable: true,
      });
    } else {
      vi.spyOn(navigator.clipboard, "writeText").mockImplementation(writeText);
    }

    const sqlContent = "```sql\nSELECT id, name FROM users WHERE active = 1;\n```";
    render(<MarkdownBody content={sqlContent} />);

    // Monospace container and language label
    expect(screen.getByText("sql")).toBeInTheDocument();

    // SQL Keywords highlighted
    expect(screen.getByText("SELECT")).toHaveClass("text-accent", "font-semibold");
    expect(screen.getByText("FROM")).toHaveClass("text-accent", "font-semibold");
    expect(screen.getByText("WHERE")).toHaveClass("text-accent", "font-semibold");

    // Copy button
    const copyBtn = screen.getByRole("button", { name: "Copy code" });
    expect(copyBtn).toBeInTheDocument();

    await user.click(copyBtn);
    expect(writeText).toHaveBeenCalledWith("SELECT id, name FROM users WHERE active = 1;");

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Copied code" })).toBeInTheDocument();
      expect(screen.getByText("Copied!")).toBeInTheDocument();
    });
  });

  it("renders bullet and numbered lists", () => {
    const listContent = "- First item\n- Second item\n\n1. Numbered one\n2. Numbered two";
    render(<MarkdownBody content={listContent} />);

    expect(screen.getByText("First item").closest("li")).toBeInTheDocument();
    expect(screen.getByText("Second item").closest("li")).toBeInTheDocument();
    expect(screen.getByText("Numbered one").closest("li")).toBeInTheDocument();
    expect(screen.getByText("Numbered two").closest("li")).toBeInTheDocument();
  });

  it("renders blockquotes with accent border", () => {
    const quoteContent = "> Note that this database migration is irreversible.";
    render(<MarkdownBody content={quoteContent} />);

    const quoteEl = screen.getByText("Note that this database migration is irreversible.").closest("blockquote");
    expect(quoteEl).toBeInTheDocument();
    expect(quoteEl).toHaveClass("border-l-2", "border-accent", "italic");
  });

  it("renders accessible hyperlinks with target blank and noopener noreferrer", () => {
    render(<MarkdownBody content="Check out [AbiData](https://abidata.example.com) for details." />);

    const link = screen.getByRole("link", { name: "AbiData" });
    expect(link).toHaveAttribute("href", "https://abidata.example.com");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("neutralizes malicious javascript: URLs in links", () => {
    render(<MarkdownBody content="Click [here](javascript:alert(1)) to hack." />);

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    const textSpan = screen.getByText("here");
    expect(textSpan.tagName).toBe("SPAN");
    expect(textSpan).not.toHaveAttribute("href");
  });

  it("escapes raw HTML tags without evaluating or injecting them into DOM", () => {
    const rawHtml = "Check <script>alert('xss')</script> and <img src=x onerror=alert(1)> in text.";
    const { container } = render(<MarkdownBody content={rawHtml} />);

    expect(container.querySelector("script")).toBeNull();
    // Raw script and tag text is rendered as safe literal text
    expect(screen.getByText(/<script>alert\('xss'\)<\/script>/)).toBeInTheDocument();
  });

  it("renders inline image preview cards", () => {
    const imgMarkdown = "![Architecture Diagram](https://example.com/arch.png)";
    render(<MarkdownBody content={imgMarkdown} />);

    const img = screen.getByAltText("Architecture Diagram");
    expect(img).toBeInTheDocument();
    expect(img).toHaveAttribute("src", "https://example.com/arch.png");
    expect(img).toHaveClass("max-h-72", "rounded-lg", "border", "border-border", "object-cover");
  });
});

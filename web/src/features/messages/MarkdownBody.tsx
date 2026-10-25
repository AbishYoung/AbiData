/* eslint-disable no-restricted-syntax -- documented exception (DESIGN.md): the full-screen photo viewer's dark overlay with white controls. */
import { useState, type ReactNode } from "react";
import { Check, Copy, X } from "lucide-react";
import { Dialog } from "radix-ui";

export interface MarkdownBodyProps {
  content: string;
  workspaceId?: string;
  className?: string;
}

export function isAllowedUrl(url: string): boolean {
  const trimmed = url.trim();
  if (trimmed.startsWith("/") || trimmed.startsWith("#")) {
    return true;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

const SQL_KEYWORDS = new Set([
  "SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "INSERT", "INTO", "UPDATE",
  "SET", "DELETE", "CREATE", "TABLE", "DROP", "ALTER", "VIEW", "INDEX", "JOIN",
  "INNER", "LEFT", "RIGHT", "OUTER", "FULL", "CROSS", "ON", "AS", "HAVING",
  "LIMIT", "OFFSET", "UNION", "ALL", "DISTINCT", "VALUES", "NULL", "TRUE",
  "FALSE", "IS", "IN", "LIKE", "ILIKE", "BETWEEN", "EXISTS", "CASE", "WHEN",
  "THEN", "ELSE", "END", "COUNT", "SUM", "AVG", "MIN", "MAX", "DEFAULT",
  "CHECK", "UNIQUE", "ASC", "DESC", "GROUP", "BY", "ORDER",
]);

function highlightSql(code: string): ReactNode[] {
  // Matches SQL comments, strings, words, and other tokens
  const tokenRegex = /(--[^\n]*)|('(?:''|[^'])*')|([a-zA-Z_][a-zA-Z0-9_]*)|(\d+(?:\.\d+)?)|([^\s\w]+|\s+)/g;
  const nodes: ReactNode[] = [];
  let match: RegExpExecArray | null;
  let key = 0;

  while ((match = tokenRegex.exec(code)) !== null) {
    const [full, comment, str, word, num] = match;
    if (comment) {
      nodes.push(
        <span key={key++} className="italic text-fg-subtle">
          {comment}
        </span>,
      );
    } else if (str) {
      nodes.push(
        <span key={key++} className="text-success">
          {str}
        </span>,
      );
    } else if (word && SQL_KEYWORDS.has(word.toUpperCase())) {
      nodes.push(
        <span key={key++} className="font-semibold text-accent">
          {word}
        </span>,
      );
    } else if (num) {
      nodes.push(
        <span key={key++} className="text-warning">
          {num}
        </span>,
      );
    } else {
      nodes.push(full);
    }
  }

  return nodes.length > 0 ? nodes : [code];
}

function CodeBlock({ code, lang }: { code: string; lang: string }) {
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    void navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  const isSql = lang.toLowerCase() === "sql";

  return (
    <div className="relative my-2.5 rounded-lg border border-border bg-surface-2 p-3 font-mono text-xs leading-relaxed shadow-2xs">
      <div className="mb-2 flex items-center justify-between border-b border-border/50 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
        <span>{lang || "code"}</span>
        <button
          type="button"
          onClick={handleCopy}
          aria-label={copied ? "Copied code" : "Copy code"}
          className="inline-flex items-center gap-1.5 rounded bg-surface-3 px-2 py-0.5 text-xs font-medium text-fg-muted transition-colors hover:bg-surface-3/80 hover:text-fg focus-visible:outline-focus"
        >
          {copied ? <Check size={12} className="text-accent" /> : <Copy size={12} />}
          <span>{copied ? "Copied!" : "Copy code"}</span>
        </button>
      </div>
      <pre className="m-0 overflow-x-auto text-fg">
        <code>{isSql ? highlightSql(code) : code}</code>
      </pre>
    </div>
  );
}

function InlineImageCard({ src, alt }: { src: string; alt?: string }) {
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);

  if (!isAllowedUrl(src)) {
    return <span className="text-xs italic text-fg-subtle">[Blocked image]</span>;
  }

  return (
    <span className="my-2 inline-block max-w-full">
      {!loaded && !error && (
        <span
          data-testid="image-skeleton"
          className="inline-block h-44 w-64 max-w-full animate-shimmer rounded-lg border border-border bg-surface-3"
        />
      )}
      {error ? (
        <span className="flex h-32 w-64 items-center justify-center rounded-lg border border-border bg-surface-2 p-3 text-xs text-fg-subtle">
          Failed to load image: {alt || src}
        </span>
      ) : (
        <img
          src={src}
          alt={alt || "Image attachment"}
          onLoad={() => setLoaded(true)}
          onError={() => setError(true)}
          onClick={() => setPreviewOpen(true)}
          className={`max-h-72 w-auto cursor-pointer rounded-lg border border-border object-cover shadow-xs transition-opacity hover:opacity-90 ${
            loaded ? "block" : "hidden"
          }`}
        />
      )}
      {previewOpen && (
        <Dialog.Root open onOpenChange={setPreviewOpen}>
          <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-[65] bg-black/85" />
            <Dialog.Content
              aria-describedby={undefined}
              className="fixed inset-0 z-[65] flex flex-col items-center justify-center p-4 outline-none"
            >
              <Dialog.Title className="sr-only">{alt || "Image preview"}</Dialog.Title>
              <div className="relative flex max-h-full max-w-full flex-col items-center">
                <div className="absolute -top-10 right-0">
                  <Dialog.Close
                    className="inline-flex h-8 w-8 items-center justify-center rounded-full bg-white/20 text-white transition-colors hover:bg-white/30"
                    aria-label="Close preview"
                  >
                    <X size={16} />
                  </Dialog.Close>
                </div>
                <img
                  src={src}
                  alt={alt || "Full preview"}
                  className="max-h-[85vh] max-w-[90vw] rounded object-contain shadow-2xl"
                />
                {alt && <p className="mt-2 text-sm text-white/90">{alt}</p>}
              </div>
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
      )}
    </span>
  );
}

function renderInline(text: string): ReactNode[] {
  // Tokenize images, links, inline code, bold, italics
  const INLINE_REGEX =
    /(!\[([^\]]*)\]\(([^)]+)\))|(\[([^\]]+)\]\(([^)]+)\))|(`([^`\n]+)`)|(\*\*([^*]+)\*\*)|(\*([^*]+)\*)/g;

  const result: ReactNode[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  let key = 0;

  while ((match = INLINE_REGEX.exec(text)) !== null) {
    if (match.index > lastIndex) {
      result.push(text.slice(lastIndex, match.index));
    }

    const [
      ,
      isImage,
      imgAlt = "",
      imgSrc = "",
      isLink,
      linkText = "",
      linkHref = "",
      isCode,
      codeText = "",
      isBold,
      boldText = "",
      isItalic,
      italicText = "",
    ] = match;

    if (isImage) {
      result.push(<InlineImageCard key={key++} src={imgSrc} alt={imgAlt} />);
    } else if (isLink) {
      if (isAllowedUrl(linkHref)) {
        result.push(
          <a
            key={key++}
            href={linkHref}
            target="_blank"
            rel="noopener noreferrer"
            className="text-accent underline underline-offset-2 hover:opacity-80"
          >
            {linkText}
          </a>,
        );
      } else {
        result.push(
          <span key={key++} className="text-fg-muted" data-neutralized-url={linkHref}>
            {linkText}
          </span>,
        );
      }
    } else if (isCode) {
      result.push(
        <code
          key={key++}
          className="font-mono text-xs bg-surface-3 px-1.5 py-0.5 rounded text-accent"
        >
          {codeText}
        </code>,
      );
    } else if (isBold) {
      result.push(
        <strong key={key++} className="font-semibold text-fg">
          {boldText}
        </strong>,
      );
    } else if (isItalic) {
      result.push(
        <em key={key++} className="italic">
          {italicText}
        </em>,
      );
    }

    lastIndex = INLINE_REGEX.lastIndex;
  }

  if (lastIndex < text.length) {
    result.push(text.slice(lastIndex));
  }

  return result.length > 0 ? result : [text];
}

type Block =
  | { type: "code"; lang: string; code: string }
  | { type: "quote"; lines: string[] }
  | { type: "ul"; items: string[] }
  | { type: "ol"; items: string[] }
  | { type: "paragraph"; lines: string[] };

function parseBlocks(raw: string): Block[] {
  const lines = raw.split("\n");
  const blocks: Block[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    if (line === undefined) {
      i++;
      continue;
    }

    // Check fenced code block start
    const codeMatch = /^```(\w*)/.exec(line);
    if (codeMatch) {
      const lang = codeMatch[1] || "";
      const codeLines: string[] = [];
      i++;
      while (i < lines.length) {
        const cur = lines[i];
        if (cur === undefined || cur.startsWith("```")) break;
        codeLines.push(cur);
        i++;
      }
      if (i < lines.length) i++; // consume closing ```
      blocks.push({ type: "code", lang, code: codeLines.join("\n") });
      continue;
    }

    // Check blockquote
    if (line.startsWith(">")) {
      const quoteLines: string[] = [];
      while (i < lines.length) {
        const cur = lines[i];
        if (!cur || !cur.startsWith(">")) break;
        quoteLines.push(cur.replace(/^>\s?/, ""));
        i++;
      }
      blocks.push({ type: "quote", lines: quoteLines });
      continue;
    }

    // Check unordered list
    const ulMatch = /^[*-]\s+(.+)/.exec(line);
    if (ulMatch) {
      const items: string[] = [];
      while (i < lines.length) {
        const cur = lines[i];
        if (!cur) break;
        const itemMatch = /^[*-]\s+(.+)/.exec(cur);
        if (!itemMatch || !itemMatch[1]) break;
        items.push(itemMatch[1]);
        i++;
      }
      blocks.push({ type: "ul", items });
      continue;
    }

    // Check ordered list
    const olMatch = /^\d+\.\s+(.+)/.exec(line);
    if (olMatch) {
      const items: string[] = [];
      while (i < lines.length) {
        const cur = lines[i];
        if (!cur) break;
        const itemMatch = /^\d+\.\s+(.+)/.exec(cur);
        if (!itemMatch || !itemMatch[1]) break;
        items.push(itemMatch[1]);
        i++;
      }
      blocks.push({ type: "ol", items });
      continue;
    }

    // Empty lines
    if (!line.trim()) {
      i++;
      continue;
    }

    // Regular paragraph
    const paraLines: string[] = [];
    while (i < lines.length) {
      const cur = lines[i];
      if (
        !cur ||
        !cur.trim() ||
        cur.startsWith("```") ||
        cur.startsWith(">") ||
        /^[*-]\s+/.test(cur) ||
        /^\d+\.\s+/.test(cur)
      ) {
        break;
      }
      paraLines.push(cur);
      i++;
    }
    blocks.push({ type: "paragraph", lines: paraLines });
  }

  return blocks;
}

export function MarkdownBody({ content, className }: MarkdownBodyProps) {
  if (!content) return null;

  const blocks = parseBlocks(content);

  return (
    <div className={`space-y-2 text-sm leading-relaxed text-fg ${className ?? ""}`}>
      {blocks.map((block, idx) => {
        switch (block.type) {
          case "code":
            return <CodeBlock key={idx} lang={block.lang} code={block.code} />;
          case "quote":
            return (
              <blockquote
                key={idx}
                className="my-2 border-l-2 border-accent pl-3 italic text-fg-muted space-y-1"
              >
                {block.lines.map((l, lIdx) => (
                  <p key={lIdx} className="m-0">
                    {renderInline(l)}
                  </p>
                ))}
              </blockquote>
            );
          case "ul":
            return (
              <ul key={idx} className="my-2 list-disc pl-5 space-y-0.5 text-fg">
                {block.items.map((item, itemIdx) => (
                  <li key={itemIdx}>{renderInline(item)}</li>
                ))}
              </ul>
            );
          case "ol":
            return (
              <ol key={idx} className="my-2 list-decimal pl-5 space-y-0.5 text-fg">
                {block.items.map((item, itemIdx) => (
                  <li key={itemIdx}>{renderInline(item)}</li>
                ))}
              </ol>
            );
          case "paragraph":
            return (
              <p key={idx} className="my-1.5 break-words leading-relaxed">
                {block.lines.flatMap((line, lineIdx) => [
                  ...(lineIdx > 0 ? [<br key={`br-${lineIdx}`} />] : []),
                  ...renderInline(line),
                ])}
              </p>
            );
        }
      })}
    </div>
  );
}

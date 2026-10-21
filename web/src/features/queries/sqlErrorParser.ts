/**
 * Parses database and SQL syntax error messages into 1-based editor coordinates
 * (line, column, endLine, endColumn) for inline Monaco error squiggles and jump-to-error.
 */

export interface ParsedSqlError {
  message: string;
  lineNumber: number;
  column: number;
  endLineNumber: number;
  endColumn: number;
  token?: string;
  offset?: number;
}

/**
 * Converts a 0-based character offset into 1-based { lineNumber, column }.
 */
export function offsetToPosition(sql: string, offset: number): { lineNumber: number; column: number } {
  const clamped = Math.max(0, Math.min(offset, sql.length));
  const lines = sql.slice(0, clamped).split("\n");
  const lineNumber = lines.length;
  const lastLine = lines[lines.length - 1] ?? "";
  const column = lastLine.length + 1;
  return { lineNumber, column };
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function extractErrorMessage(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  if (typeof error === "object" && error !== null && "message" in error && typeof (error as { message: unknown }).message === "string") {
    return (error as { message: string }).message;
  }
  return "";
}

/**
 * Attempts to parse an error message and correlate it to a location in `sql`.
 * Returns null if no specific position or syntax error can be determined.
 */
export function parseSqlError(error: unknown, sql: string): ParsedSqlError | null {
  const raw = extractErrorMessage(error).trim();
  if (!raw) return null;

  // Clean up common wrappers like "SQL error: ..." or trailing periods
  let message = raw;
  if (message.startsWith("SQL error:")) {
    message = message.slice("SQL error:".length).trim();
  }

  // 1. Extract token if present
  // Matches: near "XYZ", near 'XYZ', syntax error at or near "XYZ", unexpected token "XYZ"
  let token: string | undefined;
  const nearMatch =
    message.match(/near\s+["']([^"']+)["']/i) ??
    message.match(/syntax error at or near\s+["']([^"']+)["']/i) ??
    message.match(/unexpected token\s+["']([^"']+)["']/i);
  if (nearMatch) {
    token = nearMatch[1];
  } else {
    // Check for "no such column: XYZ" or "no such table: XYZ"
    const noSuchMatch = message.match(/no such (?:column|table):\s+([a-zA-Z0-9_]+)/i);
    if (noSuchMatch) {
      token = noSuchMatch[1];
    }
  }

  // 2. Extract character offset if present:
  // Matches: "at offset 42", "offset 42", "at character 38"
  let offset: number | undefined;
  const offsetMatch = message.match(/(?:at offset|offset|at character)\s+(\d+)/i);
  if (offsetMatch?.[1] !== undefined) {
    offset = parseInt(offsetMatch[1], 10);
  }

  // 3. Extract line and column if present:
  // Matches: "at line 3, column 5", "line 3, col 5", "at line 3:5"
  let lineCol: { lineNumber: number; column: number } | undefined;
  const lineColMatch =
    message.match(/at line\s+(\d+)(?:,\s*column\s*|,\s*col\s*|:)(\d+)/i) ??
    message.match(/line\s+(\d+)(?:,\s*column\s*|,\s*col\s*|:)(\d+)/i);
  if (lineColMatch?.[1] !== undefined && lineColMatch?.[2] !== undefined) {
    lineCol = {
      lineNumber: parseInt(lineColMatch[1], 10),
      column: parseInt(lineColMatch[2], 10),
    };
  }

  // Check for "incomplete input" or "syntax error at end of input"
  const isIncomplete = /incomplete input|syntax error at end of input/i.test(message);

  // If no syntax indicators were found and no line/col/offset/token/incomplete, return null
  if (!lineCol && offset === undefined && !token && !isIncomplete) {
    return null;
  }

  let lineNumber = 1;
  let column = 1;
  let endLineNumber = 1;
  let endColumn = 1;

  if (lineCol) {
    lineNumber = Math.max(1, lineCol.lineNumber);
    column = Math.max(1, lineCol.column);
    endLineNumber = lineNumber;
    if (token) {
      endColumn = column + token.length;
    } else {
      // Find the length of the word at line and column if possible
      const lines = sql.split("\n");
      const lineText = lines[lineNumber - 1] ?? "";
      const rest = lineText.slice(column - 1);
      const wordMatch = rest.match(/^[a-zA-Z0-9_]+/);
      endColumn = column + (wordMatch ? wordMatch[0].length : 1);
    }
  } else if (offset !== undefined) {
    const pos = offsetToPosition(sql, offset);
    lineNumber = pos.lineNumber;
    column = pos.column;
    endLineNumber = lineNumber;
    if (token) {
      endColumn = column + token.length;
    } else {
      const rest = sql.slice(offset);
      const wordMatch = rest.match(/^[a-zA-Z0-9_]+/);
      endColumn = column + (wordMatch ? wordMatch[0].length : 1);
    }
  } else if (token) {
    // Try to find the token in sql
    const wordRe = new RegExp(`\\b${escapeRegExp(token)}\\b`);
    const wordMatch = wordRe.exec(sql);
    const idx = wordMatch ? wordMatch.index : sql.indexOf(token);

    if (idx !== -1) {
      const pos = offsetToPosition(sql, idx);
      lineNumber = pos.lineNumber;
      column = pos.column;
      endLineNumber = lineNumber;
      endColumn = column + token.length;
      offset = idx;
    } else {
      // Token not found in text (maybe generated or internal), point to start
      lineNumber = 1;
      column = 1;
      endLineNumber = 1;
      endColumn = token.length + 1;
    }
  } else if (isIncomplete) {
    const pos = offsetToPosition(sql, sql.length);
    lineNumber = pos.lineNumber;
    column = Math.max(1, pos.column - 1);
    endLineNumber = lineNumber;
    endColumn = pos.column;
    offset = sql.length;
  }

  return {
    message: raw,
    lineNumber,
    column,
    endLineNumber,
    endColumn,
    token,
    offset,
  };
}

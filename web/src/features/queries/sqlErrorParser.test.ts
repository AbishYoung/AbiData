import { describe, expect, it } from "vitest";
import { offsetToPosition, parseSqlError } from "./sqlErrorParser";

describe("offsetToPosition", () => {
  it("converts 0-based offset to 1-based lineNumber and column", () => {
    const sql = "SELECT id, name\nFROM customers\nWHERE active = 1;";
    // 'S' is offset 0 -> line 1, col 1
    expect(offsetToPosition(sql, 0)).toEqual({ lineNumber: 1, column: 1 });
    // 'F' is offset 16 -> line 2, col 1
    expect(offsetToPosition(sql, 16)).toEqual({ lineNumber: 2, column: 1 });
    // 'W' is offset 31 -> line 3, col 1
    expect(offsetToPosition(sql, 31)).toEqual({ lineNumber: 3, column: 1 });
  });

  it("clamps out-of-range offsets cleanly", () => {
    const sql = "SELECT 1;";
    expect(offsetToPosition(sql, -5)).toEqual({ lineNumber: 1, column: 1 });
    expect(offsetToPosition(sql, 100)).toEqual({ lineNumber: 1, column: 10 });
  });
});

describe("parseSqlError", () => {
  it("parses SQLite error with near token and offset", () => {
    const sql = "SELECT id\nFORM orders\nWHERE id = 1";
    const error = 'near "FORM": syntax error at offset 10';
    const parsed = parseSqlError(error, sql);
    expect(parsed).not.toBeNull();
    expect(parsed?.token).toBe("FORM");
    expect(parsed?.offset).toBe(10);
    expect(parsed?.lineNumber).toBe(2);
    expect(parsed?.column).toBe(1);
    expect(parsed?.endLineNumber).toBe(2);
    expect(parsed?.endColumn).toBe(5);
  });

  it("parses SQLite error with near token but without offset", () => {
    const sql = "SELECT id, name\nFROM customers\nWHER active = 1";
    const error = 'SQL error: near "WHER": syntax error.';
    const parsed = parseSqlError(error, sql);
    expect(parsed).not.toBeNull();
    expect(parsed?.token).toBe("WHER");
    expect(parsed?.lineNumber).toBe(3);
    expect(parsed?.column).toBe(1);
    expect(parsed?.endLineNumber).toBe(3);
    expect(parsed?.endColumn).toBe(5);
  });

  it("parses PostgreSQL character offset error", () => {
    const sql = "SELECT * FROM orders FORM customers";
    const error = 'syntax error at or near "FORM" at character 22';
    const parsed = parseSqlError(error, sql);
    expect(parsed).not.toBeNull();
    expect(parsed?.token).toBe("FORM");
    expect(parsed?.offset).toBe(22);
    expect(parsed?.lineNumber).toBe(1);
    expect(parsed?.column).toBe(23);
    expect(parsed?.endColumn).toBe(27);
  });

  it("parses explicit line and column error messages", () => {
    const sql = "SELECT id\nFROM orders\nWHERE status = 'open'";
    const error = "unexpected token at line 2, col 6";
    const parsed = parseSqlError(error, sql);
    expect(parsed).not.toBeNull();
    expect(parsed?.lineNumber).toBe(2);
    expect(parsed?.column).toBe(6);
    expect(parsed?.endLineNumber).toBe(2);
    expect(parsed?.endColumn).toBe(12); // length of 'orders'
  });

  it("parses missing column error by locating column in SQL", () => {
    const sql = "SELECT id, non_existent_col FROM users";
    const error = "no such column: non_existent_col";
    const parsed = parseSqlError(error, sql);
    expect(parsed).not.toBeNull();
    expect(parsed?.token).toBe("non_existent_col");
    expect(parsed?.lineNumber).toBe(1);
    expect(parsed?.column).toBe(12);
    expect(parsed?.endColumn).toBe(28);
  });

  it("parses missing table error by locating table in SQL", () => {
    const sql = "SELECT * FROM unknown_table WHERE id = 1";
    const error = "no such table: unknown_table";
    const parsed = parseSqlError(error, sql);
    expect(parsed).not.toBeNull();
    expect(parsed?.token).toBe("unknown_table");
    expect(parsed?.lineNumber).toBe(1);
    expect(parsed?.column).toBe(15);
  });

  it("parses incomplete input error", () => {
    const sql = "SELECT * FROM users WHERE";
    const error = "incomplete input";
    const parsed = parseSqlError(error, sql);
    expect(parsed).not.toBeNull();
    expect(parsed?.lineNumber).toBe(1);
    expect(parsed?.column).toBe(25);
    expect(parsed?.endColumn).toBe(26);
  });

  it("returns null for non-syntax or non-positional errors", () => {
    expect(parseSqlError("only read-only statements are allowed", "DELETE FROM users")).toBeNull();
    expect(parseSqlError("The database is locked", "SELECT 1")).toBeNull();
    expect(parseSqlError(null, "SELECT 1")).toBeNull();
  });
});

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

// Expressions authored by builders (validation rules, defaults, calculated
// fields) are embedded into DDL. Before that happens they must pass two checks:
//
//  1. lexical: a single parenthesis-balanced expression with no statement
//     separator, comments, or bind parameters, so it cannot close the
//     surrounding clause or append statements;
//  2. semantic: SQLite prepares `SELECT (expr) FROM "table"` under an authorizer
//     that permits only reads of that one table and an allowlist of builtin
//     deterministic functions.
//
// DDL is additionally executed one statement at a time, never as a script.

// ExprKind selects what an expression may reference.
type ExprKind int

const (
	ExprValidation ExprKind = iota // may read this table's columns
	ExprCalculated                 // may read this table's columns
	ExprDefault                    // literals and functions only; no columns
)

const maxExprLen = 4000

// lexCheck enforces the lexical rules.
func lexCheck(expr string) error { return lexCheckOpt(expr, false) }

// CheckExprAllowParams is CheckExpr but permits :name placeholders (for WHERE
// fragments bound at run time, e.g. db.lookup).
func CheckExprAllowParams(expr string) error { return lexCheckOpt(expr, true) }

func lexCheckOpt(expr string, allowNamed bool) error {
	bad := func(msg string) error { return fault.New(fault.BadRequest, "Invalid expression: "+msg) }
	if strings.TrimSpace(expr) == "" {
		return bad("it is empty.")
	}
	if len(expr) > maxExprLen {
		return bad("it is too long.")
	}
	depth := 0
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		switch c {
		case '`', '[':
			return bad("use double quotes for names (backticks and brackets are not allowed).")
		case '\'', '"':
			closer := c
			j := i + 1
			for ; j < len(expr); j++ {
				if expr[j] == closer {
					if j+1 < len(expr) && expr[j+1] == closer {
						j++ // doubled quote
						continue
					}
					break
				}
			}
			if j >= len(expr) {
				return bad("a quote is not closed.")
			}
			i = j
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return bad("parentheses are not balanced.")
			}
		case ';':
			return bad("';' is not allowed.")
		case '-':
			if i+1 < len(expr) && expr[i+1] == '-' {
				return bad("comments are not allowed.")
			}
		case '/':
			if i+1 < len(expr) && expr[i+1] == '*' {
				return bad("comments are not allowed.")
			}
		case ':':
			if allowNamed && i+1 < len(expr) && (expr[i+1] >= 'A' && expr[i+1] <= 'Z' || expr[i+1] >= 'a' && expr[i+1] <= 'z') {
				continue
			}
			return bad("parameters are not allowed.")
		case '?', '@', '$':
			return bad("parameters are not allowed.")
		case 0:
			return bad("NUL characters are not allowed.")
		}
	}
	if depth != 0 {
		return bad("parentheses are not balanced.")
	}
	return nil
}

// deterministicFuncs are builtins allowed in stored expressions: the
// deterministic entries of the sqlgen function catalog (ADR 0013 §4). SQLite
// itself rejects non-deterministic use (e.g. date('now')) inside CHECK and
// generated columns; the catalog additionally keeps out anything not portable
// to stock sqlite3.
var deterministicFuncs = func() map[string]bool {
	m := map[string]bool{}
	for _, f := range sqlgen.DeterministicFuncs() {
		m[f] = true
	}
	return m
}()

// ValidateExpr checks expr for use in table (physical name). For ExprDefault the
// table may be empty. Returns a BAD_REQUEST fault describing the problem.
func ValidateExpr(ctx context.Context, q dbx.Querier, kind ExprKind, table, expr string) error {
	if err := lexCheck(expr); err != nil {
		return err
	}
	// dialect: sqlite. The semantic check prepares the expression under the
	// SQLite authorizer; ADR 0013 Phase D replaces it with the parser.
	c, ok := sqlitex.ConnOf(q)
	if !ok {
		return fmt.Errorf("catalog: expression check needs a SQLite connection, got %T", q)
	}
	from := ""
	if kind != ExprDefault {
		from = " FROM " + Quote(table)
	}
	var denied string
	auth := func(action sqlite3.AuthorizerActionCode, a3, a4, schema, _ string) sqlite3.AuthorizerReturnCode {
		switch action {
		case sqlite3.AUTH_SELECT:
			return sqlite3.AUTH_OK
		case sqlite3.AUTH_READ:
			if kind != ExprDefault && schema == "main" && strings.EqualFold(a3, table) {
				return sqlite3.AUTH_OK
			}
			denied = "it may only refer to fields of this table"
		case sqlite3.AUTH_FUNCTION:
			if deterministicFuncs[strings.ToLower(a4)] {
				return sqlite3.AUTH_OK
			}
			denied = "function " + a4 + " is not allowed"
		default:
			denied = "it contains an operation that is not allowed"
		}
		return sqlite3.AUTH_DENY
	}
	err := c.WithAuthorizer(auth, func() error {
		_, err := c.Prepare(ctx, "SELECT ("+expr+")"+from)
		return err
	})
	if err != nil {
		if denied != "" {
			return fault.New(fault.BadRequest, "Invalid expression: "+denied+".")
		}
		return &fault.Error{Code: fault.BadRequest, Message: "Invalid expression: " + sqliteMessage(err)}
	}
	if kind == ExprDefault && strings.Contains(strings.ToLower(expr), "select") {
		return fault.New(fault.BadRequest, "Invalid expression: subqueries are not allowed in defaults.")
	}
	return nil
}

func sqliteMessage(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return msg
}

// CheckExpr applies the lexical expression rules (single balanced expression,
// no statement separators, comments or parameters) for SQL fragments authored
// elsewhere, e.g. visual query expressions.
func CheckExpr(expr string) error { return lexCheck(expr) }

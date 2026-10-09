package catalog

import (
	"context"
	"strings"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/fault"
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
func lexCheck(expr string) error {
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
		case '\'', '"', '`', '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			j := i + 1
			for ; j < len(expr); j++ {
				if expr[j] == closer {
					if closer != ']' && j+1 < len(expr) && expr[j+1] == closer {
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
		case '?', ':', '@', '$':
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

// deterministicFuncs are builtins allowed in stored expressions. SQLite itself
// rejects non-deterministic use (e.g. date('now')) inside CHECK and generated
// columns; this list additionally keeps out anything not portable to stock sqlite3.
var deterministicFuncs = func() map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.Fields(`abs char coalesce concat concat_ws format glob hex ifnull iif if instr length
	like likelihood likely lower ltrim max min nullif octet_length printf quote replace round rtrim sign substr
	substring trim typeof unhex unicode unlikely upper zeroblob date time datetime julianday strftime unixepoch timediff
	json json_array json_array_length json_extract json_insert json_object json_patch json_quote json_remove
	json_replace json_set json_type json_valid json_error_position
	acos acosh asin asinh atan atan2 atanh ceil ceiling cos cosh degrees exp floor ln log log10 log2 mod pi pow power
	radians sin sinh sqrt tan tanh trunc`) {
		m[f] = true
	}
	return m
}()

// ValidateExpr checks expr for use in table (physical name). For ExprDefault the
// table may be empty. Returns a BAD_REQUEST fault describing the problem.
func ValidateExpr(ctx context.Context, c *sqlitex.Conn, kind ExprKind, table, expr string) error {
	if err := lexCheck(expr); err != nil {
		return err
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

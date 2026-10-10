package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/fault"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Limits for user SQL (brief: 10 s cap, 50,000-row page cap; plus a byte cap
// so a result cannot exhaust server memory).
const (
	MaxBytes    = 64 << 20
	MaxRows     = 50000
	MaxDuration = 10 * time.Second
	MaxSQLLen   = 100_000
)

// Result is a query result with JSON-safe values.
type Result struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
}

// Params maps parameter names (without prefix) to values.
type Params map[string]any

// Run executes one read-only statement on a reader connection under the
// authorizer for p, with a time and row cap.
func Run(ctx context.Context, l *workspace.Lease, p Policy, sql string, params Params, maxRows int) (Result, error) {
	if maxRows <= 0 || maxRows > MaxRows {
		maxRows = MaxRows
	}
	if strings.TrimSpace(sql) == "" {
		return Result{}, fault.New(fault.BadRequest, "Enter a SQL statement.")
	}
	if len(sql) > MaxSQLLen {
		return Result{}, fault.New(fault.BadRequest, "The SQL statement is too long.")
	}
	p.Write = false
	ctx, cancel := context.WithTimeout(ctx, MaxDuration)
	defer cancel()
	var res Result
	err := l.ReadConn(ctx, func(c *sqlitex.Conn) error {
		var denial Denial
		return wrapAuth(&denial, c.WithAuthorizer(p.Authorizer(&denial), func() error {
			desc, err := c.Describe(ctx, sql)
			if err != nil {
				return err
			}
			if !desc.ReadOnly {
				return fault.New(fault.SQLNotAllowed, "Only statements that read data are allowed here.")
			}
			args, err := bindParams(desc.Params, params)
			if err != nil {
				return err
			}
			rows, err := c.Query(ctx, sql, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			res.Columns = rows.Columns()
			res.Rows = [][]any{}
			size := 0
			for rows.Next() {
				if len(res.Rows) == maxRows || size > MaxBytes {
					res.Truncated = true
					break
				}
				vals := rows.Values()
				for i, v := range vals {
					size += sqlitex.SizeOf(v)
					vals[i] = jsonSafe(v)
				}
				res.Rows = append(res.Rows, vals)
			}
			return rows.Err()
		}))
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
			return Result{}, fault.Newf(fault.WorkspaceBusy, "The query ran longer than %s and was stopped.", MaxDuration)
		}
		return Result{}, err
	}
	return res, nil
}

// wrapAuth turns authorizer denials into SQL_NOT_ALLOWED with the reason, and
// other SQLite errors from user SQL into BAD_REQUEST with SQLite's message.
func wrapAuth(d *Denial, err error) error {
	if err == nil {
		return nil
	}
	var fe *fault.Error
	if errors.As(err, &fe) {
		return err
	}
	var se *sqlite3.Error
	if errors.As(err, &se) {
		if se.Code() == sqlite3.AUTH || d.Reason != "" {
			reason := d.Reason
			if reason == "" {
				reason = "the statement uses an operation that is not permitted"
			}
			return &fault.Error{Code: fault.SQLNotAllowed, Message: "Not allowed: " + reason + ".", Cause: err}
		}
		if se.Code() == sqlite3.ERROR {
			msg := se.Error()
			// Functions that are absent from this build fail before the authorizer
			// runs; denied ones are still reported as not allowed.
			if i := strings.Index(msg, "no such function: "); i >= 0 {
				if name := strings.TrimSpace(msg[i+len("no such function: "):]); deniedFunctions[strings.ToLower(name)] {
					return &fault.Error{Code: fault.SQLNotAllowed, Message: "Not allowed: function " + name + " is not allowed.", Cause: err}
				}
			}
			if i := strings.LastIndex(msg, ": "); i >= 0 {
				msg = msg[i+2:]
			}
			return &fault.Error{Code: fault.BadRequest, Message: "SQL error: " + msg, Cause: err}
		}
	}
	if strings.Contains(err.Error(), "multiple statements") {
		return fault.New(fault.SQLNotAllowed, "Only one statement can run at a time.")
	}
	return err
}

func bindParams(names []string, params Params) ([]any, error) {
	var args []any
	var missing []string
	for _, n := range names {
		if strings.HasPrefix(n, "?") {
			return nil, fault.New(fault.BadRequest, "Use named parameters such as :start_date instead of ?.")
		}
		key := n[1:]
		v, ok := params[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		if num, ok := v.(json.Number); ok {
			if i, err := num.Int64(); err == nil {
				v = i
			} else if f, err := num.Float64(); err == nil {
				v = f
			}
		}
		switch v.(type) {
		case nil, string, int64, float64, bool:
		default:
			return nil, fault.Newf(fault.BadRequest, "Parameter %s must be a text, number, yes/no or empty value.", key)
		}
		args = append(args, sqlitex.Named{Name: n, Value: v})
	}
	if len(missing) > 0 {
		return nil, fault.Newf(fault.BadRequest, "Enter a value for: %s.", strings.Join(missing, ", ")).
			WithDetails(map[string]any{"missing_parameters": missing})
	}
	return args, nil
}

func jsonSafe(v any) any {
	switch x := v.(type) {
	case int64:
		if x > 1<<53 || x < -(1<<53) {
			return fmt.Sprint(x)
		}
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	}
	return v
}

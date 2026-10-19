package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/sqlgen"
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
	Columns []string `json:"columns"`
	// Types names how each column is best displayed: text, integer, number,
	// currency, boolean, date, datetime, geo_point or geo_area (GeoJSON text). Currency values are integers scaled
	// by 10^4, as stored. Types has one entry per column.
	Types     []string `json:"column_types"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
}

// Params maps parameter names (without prefix) to values.
type Params map[string]any

// Run executes one read-only statement on a reader connection under the
// authorizer for p, with a time and row cap.
func Run(ctx context.Context, l *workspace.Lease, p Policy, sql string, params Params, maxRows int) (Result, error) {
	return RunTyped(ctx, l, p, nil, nil, sql, params, maxRows)
}

// RunTyped is Run with column typing: hints (one per column, "" = unknown, as
// produced by Compile) win, then the catalog column a result column is read
// from (when sch is set), then the declared SQLite type, then sampled values.
func RunTyped(ctx context.Context, l *workspace.Lease, p Policy, sch *catalog.Schema, hints []string, sql string, params Params, maxRows int) (Result, error) {
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
	if l.Dialect() != sqlgen.SQLite {
		return runPG(ctx, l, sch, hints, sql, params, maxRows)
	}
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
			res.Types = make([]string, len(res.Columns))
			for i := range res.Types {
				if i < len(hints) && hints[i] != "" {
					res.Types[i] = hints[i]
					continue
				}
				// dialect: sqlite. Column origins type user SQL's columns;
				// generated SQL carries its types in hints.
				if o, ok := rows.(dbx.OriginRows); ok {
					table, col, decl := o.ColumnOrigin(i)
					res.Types[i] = originType(sch, table, col, decl)
				}
				if res.Types[i] == "" && aggregateOfCurrency(sch, res.Columns[i]) {
					res.Types[i] = "currency"
				}
			}
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
			sampleTypes(&res)
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
			return fault.SQLError(fault.SQLiteMessage(se), err)
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
		switch x := v.(type) {
		case nil, string, int64, float64, bool:
		case map[string]any:
			// An area parameter arrives as a GeoJSON object; it binds as its JSON text.
			b, err := json.Marshal(x)
			if err != nil {
				return nil, fault.Newf(fault.BadRequest, "Parameter %s is not valid JSON.", key)
			}
			v = string(b)
		default:
			return nil, fault.Newf(fault.BadRequest, "Parameter %s must be a text, number, yes/no or empty value.", key)
		}
		args = append(args, dbx.Named{Name: n, Value: v})
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

// FieldDisplayType maps a catalog column to a result column type.
func FieldDisplayType(c *catalog.Column) string {
	switch c.Type {
	case catalog.Autonumber, catalog.Integer, catalog.Lookup:
		return "integer"
	case catalog.Number:
		return "number"
	case catalog.Currency:
		return "currency"
	case catalog.YesNo:
		return "boolean"
	case catalog.Date:
		return "date"
	case catalog.DateTime:
		return "datetime"
	case catalog.GeoPoint:
		return "geo_point"
	case catalog.GeoArea:
		return "geo_area"
	case catalog.Calculated:
		switch c.Options.ResultType {
		case "integer", "number", "currency", "date", "datetime", "geo_point", "geo_area":
			return c.Options.ResultType
		case "yes_no":
			return "boolean"
		}
		return ""
	}
	return "text"
}

// originType types a result column from its catalog column or declared type;
// "" means unknown (sampled later).
func originType(sch *catalog.Schema, table, col, decl string) string {
	if sch != nil && table != "" {
		if t, ok := sch.TableByPhysical(table); ok {
			switch strings.ToLower(col) {
			case "id", "_version":
				return "integer"
			}
			if c, ok := t.ColumnByPhysical(col); ok {
				return FieldDisplayType(c)
			}
		}
	}
	switch strings.ToUpper(decl) {
	case "INTEGER", "INT":
		return "integer"
	case "REAL":
		return "number"
	case "TEXT":
		return "text"
	}
	return ""
}

// currencyAggregate matches an unaliased result column such as
// "sum(Total)" or "MAX(o.\"Unit_price\")": SQLite names it by its text.
var currencyAggregate = regexp.MustCompile(`(?i)^\s*(sum|total|avg|min|max)\(\s*(?:distinct\s+)?(?:["\[\x60]?\w+["\]\x60]?\.)?["\[\x60]?(\w+)["\]\x60]?\s*\)\s*$`)

// aggregateOfCurrency reports whether a result column with no catalog origin
// is a plain aggregate of a column that is currency in every table that has
// it. Such values are still scaled by 10^4, so they display as currency.
// Anything more complex (arithmetic, aliases) stays untyped and is sampled.
func aggregateOfCurrency(sch *catalog.Schema, name string) bool {
	m := currencyAggregate.FindStringSubmatch(name)
	if m == nil || sch == nil {
		return false
	}
	found := false
	for i := range sch.Tables {
		if c, ok := sch.Tables[i].ColumnByPhysical(m[2]); ok {
			if FieldDisplayType(c) != "currency" {
				return false
			}
			found = true
		}
	}
	return found
}

var (
	dateSample     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	datetimeSample = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})?$`)
)

// sampleTypes types the columns that are still unknown from their values.
func sampleTypes(res *Result) {
	for i, t := range res.Types {
		if t != "" {
			continue
		}
		kind := ""
		for _, row := range res.Rows {
			if i >= len(row) || row[i] == nil {
				continue
			}
			var k string
			switch v := row[i].(type) {
			case int64:
				k = "integer"
			case float64:
				k = "number"
			case string:
				switch {
				case looksLikeGeo(v) != "":
					k = looksLikeGeo(v)
				case dateSample.MatchString(v):
					k = "date"
				case datetimeSample.MatchString(v):
					k = "datetime"
				default:
					k = "text"
				}
			default:
				k = "text"
			}
			switch {
			case kind == "" || kind == k:
				kind = k
			case (kind == "integer" && k == "number") || (kind == "number" && k == "integer"):
				kind = "number"
			default:
				kind = "text"
			}
			if kind == "text" {
				break
			}
		}
		if kind == "" {
			kind = "text"
		}
		res.Types[i] = kind
	}
}

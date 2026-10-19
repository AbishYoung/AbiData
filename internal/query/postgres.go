package query

// User SQL on PostgreSQL (ADR 0012 §3, Phase 3). SQLite vets user SQL with its
// authorizer; PostgreSQL runs it as the workspace's roles instead: queries run
// in the sandbox as the read role, and checks (describeSQL) prepare and plan
// it as that role inside the caller's transaction. Neither can see the system
// tables, so the builders' read access to _sys_* is not available here yet.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/pgdb"
	"abidata/internal/sqlgen"
	"abidata/internal/workspace"
)

// pgDescribe is describeSQL on PostgreSQL: q is a workspace transaction.
func pgDescribe(ctx context.Context, q dbx.Querier, sql string) ([]string, error) {
	tx, ok := q.(pgdb.Tx)
	if !ok {
		return nil, fmt.Errorf("query: SQL check needs a PostgreSQL transaction, got %T", q)
	}
	if err := pgReadsOnly(sql); err != nil {
		return nil, err
	}
	var schema string
	if err := tx.QueryRow(ctx, sqlPGDescribe.SQL).Scan(&schema); err != nil {
		return nil, err
	}
	cols, _, err := pgdb.DescribeIn(ctx, tx, pgdb.ReadRoleOf(schema), sql)
	if err != nil {
		return nil, PostgresError(err)
	}
	return cols, nil
}

// runPG is RunTyped on PostgreSQL.
func runPG(ctx context.Context, l *workspace.Lease, sch *catalog.Schema, hints []string, sql string, params Params, maxRows int) (Result, error) {
	if err := pgReadsOnly(sql); err != nil {
		return Result{}, err
	}
	names, err := dbx.Params(sql)
	if err != nil {
		return Result{}, fault.SQLError(err.Error(), err)
	}
	args, err := bindParams(names, params)
	if err != nil {
		return Result{}, err
	}
	r, err := l.UserQuery(ctx, sql, args, pgdb.Limits{Timeout: MaxDuration, MaxRows: maxRows})
	if err != nil {
		return Result{}, PostgresError(err)
	}
	res := Result{Columns: r.Columns, Types: make([]string, len(r.Columns)), Rows: [][]any{}, Truncated: r.Truncated}
	origin, err := pgOriginTypes(ctx, l, sch, r.Origins)
	if err != nil {
		return Result{}, err
	}
	for i := range res.Types {
		switch {
		case i < len(hints) && hints[i] != "":
			res.Types[i] = hints[i]
		case origin[i] != "":
			res.Types[i] = origin[i]
		case aggregateOfCurrency(sch, res.Columns[i]):
			res.Types[i] = "currency"
		default:
			res.Types[i] = pgOIDType(r.Origins[i].TypeOID)
		}
	}
	size := 0
	for _, row := range r.Rows {
		if size > MaxBytes {
			res.Truncated = true
			break
		}
		for i, v := range row {
			v = catalog.StorageForm(storedFieldType(res.Types[i]), v)
			if b, ok := v.(string); ok {
				size += len(b)
			}
			size += 16
			row[i] = jsonSafe(v)
		}
		res.Rows = append(res.Rows, row)
	}
	sampleTypes(&res)
	return res, nil
}

// storedFieldType is the field type whose storage form a result column of
// display type t takes: dates and times as text, yes/no as 0/1.
func storedFieldType(t string) catalog.FieldType {
	switch t {
	case "date":
		return catalog.Date
	case "datetime":
		return catalog.DateTime
	case "yes_no":
		return catalog.YesNo
	}
	return catalog.LongText
}

// pgOriginTypes types result columns read straight from a table column, as
// SQLite's column origins do.
func pgOriginTypes(ctx context.Context, l *workspace.Lease, sch *catalog.Schema, origins []pgdb.Origin) ([]string, error) {
	out := make([]string, len(origins))
	var oids []int64
	for _, o := range origins {
		if o.TableOID != 0 {
			oids = append(oids, int64(o.TableOID))
		}
	}
	if len(oids) == 0 || sch == nil {
		return out, nil
	}
	type key struct {
		oid  int64
		attr int64
	}
	cols := map[key][2]string{}
	err := l.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, sqlPGOriginTypes.SQL, oids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var oid, attr int64
			var table, col string
			if err := rows.Scan(&oid, &attr, &table, &col); err != nil {
				return err
			}
			cols[key{oid, attr}] = [2]string{table, col}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for i, o := range origins {
		if tc, ok := cols[key{int64(o.TableOID), int64(o.Attr)}]; ok {
			out[i] = originType(sch, tc[0], tc[1], "")
		}
	}
	return out, nil
}

// pgOIDType types a computed column by its PostgreSQL type.
func pgOIDType(oid uint32) string {
	switch oid {
	case 16: // bool
		return "yes_no"
	case 20, 21, 23: // int8, int2, int4
		return "integer"
	case 700, 701, 1700: // float4, float8, numeric
		return "number"
	case 1082:
		return "date"
	case 1184, 1114:
		return "datetime"
	}
	return ""
}

// pgReadsOnly refuses SQL that does not start as a query, as SQLite's
// read-only check does. User SQL runs as a subquery, where an INSERT or a
// DELETE would only be a syntax error.
func pgReadsOnly(sql string) error {
	if StartsAsQuery(sql) {
		return nil
	}
	return fault.New(fault.SQLNotAllowed, "Only statements that read data are allowed here.")
}

// StartsAsQuery reports whether sql's first keyword, past comments and
// parentheses, starts a query: SELECT, WITH, VALUES or TABLE.
func StartsAsQuery(sql string) bool {
	s := sql
	for {
		s = strings.TrimLeft(s, " \t\r\n(")
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			s = ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			s = ""
		}
		break
	}
	end := strings.IndexFunc(s, func(r rune) bool { return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') })
	if end < 0 {
		end = len(s)
	}
	switch strings.ToUpper(s[:end]) {
	case "SELECT", "WITH", "VALUES", "TABLE", "":
		return true
	}
	return false
}

// PostgresError turns the server's errors on user SQL into what wrapAuth gives
// on SQLite: SQL_NOT_ALLOWED for what the roles forbid, BAD_REQUEST with the
// server's message for mistakes in the SQL.
func PostgresError(err error) error {
	if errors.Is(err, pgdb.ErrMultipleStatements) {
		return fault.New(fault.SQLNotAllowed, "Only one statement can run at a time.")
	}
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err
	}
	switch pe.Code {
	case "42501": // insufficient_privilege
		return &fault.Error{Code: fault.SQLNotAllowed, Message: "Not allowed: system tables and other workspaces cannot be read.", Cause: err}
	case "25006": // read_only_sql_transaction
		return &fault.Error{Code: fault.SQLNotAllowed, Message: "Only statements that read data are allowed here.", Cause: err}
	case "57014": // query_canceled: statement_timeout
		return fault.Newf(fault.WorkspaceBusy, "The query ran longer than %s and was stopped.", MaxDuration)
	}
	if strings.HasPrefix(pe.Code, "42") || strings.HasPrefix(pe.Code, "22") || strings.HasPrefix(pe.Code, "0A") {
		return fault.SQLError(pe.Message, err)
	}
	return err
}

// dialectString is the dialect saved queries record for q's backend.
func dialectString(q dbx.Querier) string {
	if pgdb.DialectOf(q) == sqlgen.SQLite {
		return sqlgen.SQLite.String()
	}
	return sqlgen.Postgres.String()
}

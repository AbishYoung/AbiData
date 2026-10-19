package db

// The db module on PostgreSQL (ADR 0012 Phase 3). SQLite vets a script's SQL
// with its authorizer on the workspace's own connection; PostgreSQL runs it
// as the workspace's sandbox roles on their own logins instead: reads as the
// read role, db.exec as the write role under the workspace's writer lock.
// Those logins cannot join db.transaction's transaction, and user SQL must
// never run on the app's (ADR 0012 §3), so inside a transaction only
// generated SQL runs: records.* and db.lookup and friends without a WHERE.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	lua "github.com/yuin/gopher-lua"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/pgdb"
	"abidata/internal/query"
	"abidata/internal/script"
	"abidata/internal/script/host"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

// pgDefaultTimeout bounds a statement when the run has no deadline.
const pgDefaultTimeout = 10 * time.Second

func onPostgres(d *host.Data) bool { return d.Lease.Dialect() != sqlgen.SQLite }

var errPGInTx = fault.New(fault.SQLNotAllowed,
	"On PostgreSQL, SQL text cannot run inside db.transaction: use records.* inside the transaction, or run the SQL outside it.")

func pgTimeout(ctx context.Context) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		if t := time.Until(dl); t > 0 {
			return t
		}
		return time.Millisecond
	}
	return pgDefaultTimeout
}

// pgStorage converts a value the server returned to the storage form scripts
// see on SQLite: dates and times as text, yes/no as 0/1, JSON as text.
func pgStorage(o pgdb.Origin, v any) any {
	t := catalog.LongText
	if o.TypeOID == pgtype.DateOID {
		t = catalog.Date
	}
	return catalog.StorageForm(t, v)
}

// pgQuery runs a script's read in the sandbox.
func pgQuery(r *script.Run, d *host.Data, sql string, args []any, limit int) pgdb.Result {
	if _, _, ok := d.Tx(); ok {
		r.RaiseFault(errPGInTx)
	}
	if !query.StartsAsQuery(sql) {
		r.RaiseFault(fault.New(fault.SQLNotAllowed, "db.query only reads; use db.exec to change data."))
	}
	res, err := d.Lease.UserQuery(r.Ctx, sql, args, pgdb.Limits{Timeout: pgTimeout(r.Ctx), MaxRows: limit})
	if err != nil {
		r.RaiseFault(query.PostgresError(err))
	}
	return res
}

func pgReadRows(r *script.Run, L *lua.LState, d *host.Data, sql string, args []any, limit int) []*lua.LTable {
	res := pgQuery(r, d, sql, args, limit)
	out := make([]*lua.LTable, 0, len(res.Rows))
	size := 0
	for _, row := range res.Rows {
		t := L.CreateTable(0, len(res.Columns))
		for i, v := range row {
			v = pgStorage(res.Origins[i], v)
			size += sqlitex.SizeOf(v)
			t.RawSetString(res.Columns[i], toLua(r, v))
		}
		if size > MaxBytes {
			r.RaiseLimit(fmt.Sprintf("query result exceeds %d MiB", MaxBytes>>20))
		}
		out = append(out, t)
	}
	return out
}

// pgReadValue is the first column of the first row, or nil.
func pgReadValue(r *script.Run, d *host.Data, sql string, args []any) lua.LValue {
	res := pgQuery(r, d, sql, args, 1)
	if len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
		return nil
	}
	return toLua(r, pgStorage(res.Origins[0], res.Rows[0][0]))
}

func pgExec(r *script.Run, d *host.Data, sql string, args []any) (changes, lastID int64) {
	if _, _, ok := d.Tx(); ok {
		r.RaiseFault(errPGInTx)
	}
	res, err := d.Lease.UserExec(r.Ctx, priority(r), sql, args, pgTimeout(r.Ctx))
	if errors.Is(err, pgdb.ErrNotDML) {
		r.RaiseFault(fault.New(fault.BadRequest, "db.exec runs INSERT, UPDATE or DELETE; use db.query to read."))
	}
	if err != nil {
		// What the roles forbid and mistakes in the SQL as console SQL reports
		// them; constraint failures as the records layer does.
		var fe *fault.Error
		if errors.As(query.PostgresError(err), &fe) {
			r.RaiseFault(fe)
		}
		r.RaiseFault(fault.Classify(err))
	}
	return res.Changes, res.LastID
}

// pgAggregate runs db.lookup and friends. Without a WHERE fragment the SQL is
// generated, so inside a transaction it runs there, as the read role; a
// fragment is the script's own SQL and runs only in the sandbox.
func pgAggregate(r *script.Run, d *host.Data, sql string, generated bool, args []any) lua.LValue {
	tx, tctx, ok := d.Tx()
	if !ok {
		return pgReadValue(r, d, sql, args)
	}
	if !generated {
		r.RaiseFault(errPGInTx)
	}
	pt, ok := tx.(pgdb.Tx)
	if !ok {
		r.RaiseFault(fmt.Errorf("db: transaction is %T, not PostgreSQL", tx))
	}
	n, _ := d.Lease.Postgres()
	v, err := pgdb.QueryValueAs(tctx, pt, n.ReadRole(), sql)
	if err != nil {
		r.RaiseFault(query.PostgresError(err))
	}
	if v.Value == nil {
		return nil
	}
	return toLua(r, pgStorage(v.Origin, v.Value))
}

package reports

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/pgdb"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// pgSource reads a report's rows on PostgreSQL (ADR 0012 Phase 3). The source
// runs in the workspace's sandbox as its read role, as console SQL does; on
// SQLite the authorizer guards it instead.
func pgSource(ctx context.Context, l *workspace.Lease, sql string, args []any, fields []field) ([]*row, error) {
	res, err := l.UserQuery(ctx, sql, args, pgdb.Limits{Timeout: MaxRunTime, MaxRows: MaxRows})
	if err != nil {
		return nil, query.PostgresError(err)
	}
	if res.Truncated {
		return nil, fault.Newf(fault.BadRequest, "The report has more than %d detail rows; add criteria to the source.", MaxRows)
	}
	rows := make([]*row, 0, len(res.Rows))
	size := 0
	for _, vals := range res.Rows {
		size += rowOverhead + fieldOverhead*len(fields)
		r := &row{vals: make(map[string]any, len(fields))}
		for i, f := range fields {
			t := catalog.LongText
			switch {
			case f.col != nil:
				t = f.col.Type
			case res.Origins[i].TypeOID == pgtype.DateOID:
				t = catalog.Date
			}
			v := catalog.StorageForm(t, vals[i])
			size += sqlitex.SizeOf(v)
			if f.col != nil {
				v = records.EncodeValue(*f.col, v)
			}
			r.vals[f.key] = v
		}
		if size > maxSourceBytes {
			return nil, fault.New(fault.BadRequest, "The report's data is too large; add criteria to the source.")
		}
		rows = append(rows, r)
	}
	return rows, nil
}

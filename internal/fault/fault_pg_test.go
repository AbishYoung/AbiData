package fault

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyPostgres(t *testing.T) {
	for _, c := range []struct {
		pe     pgconn.PgError
		code   Code
		detail string
		want   any
	}{
		{pgconn.PgError{Code: "23505", Detail: `Key ("Email", name)=(a@b, x) already exists.`}, DuplicateValue, "columns", "Email, name"},
		{pgconn.PgError{Code: "23505", Detail: `Key ("Email" COLLATE abi_ci)=(a@b) already exists.`}, DuplicateValue, "columns", "Email"},
		{pgconn.PgError{Code: "23503"}, LinkedRecords, "", nil},
		{pgconn.PgError{Code: "23001"}, LinkedRecords, "", nil},
		{pgconn.PgError{Code: "23514", ConstraintName: "ck_01abc"}, ValidationFailed, "constraint", "ck_01abc"},
		{pgconn.PgError{Code: "23502", ColumnName: "Name"}, RequiredField, "column", "Name"},
		{pgconn.PgError{Code: "22001"}, ValidationFailed, "", nil},
		{pgconn.PgError{Code: "55P03"}, WorkspaceBusy, "", nil},
		{pgconn.PgError{Code: "57014"}, WorkspaceBusy, "", nil},
		{pgconn.PgError{Code: "42501"}, SQLNotAllowed, "", nil},
		{pgconn.PgError{Code: "XX000"}, Internal, "", nil},
	} {
		pe := c.pe
		fe := Classify(&pe)
		if fe.Code != c.code || (c.detail != "" && fe.Details[c.detail] != c.want) {
			t.Errorf("%s: %s %v, want %s %s=%v", c.pe.Code, fe.Code, fe.Details, c.code, c.detail, c.want)
		}
	}
}

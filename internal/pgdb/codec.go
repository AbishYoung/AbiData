package pgdb

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"abidata/internal/dbx"
)

// Values in and out (ADR 0013 §3). Callers pass and scan the Go types they
// use with sqlitex. System tables keep SQLite's storage on PostgreSQL (0/1
// flags, text timestamps), while user tables use native types, so a Go bool
// or time.Time is encoded as whatever the parameter's type is, and a *bool
// reads a 0/1 integer or a boolean.

// flexBool is a bool for a bigint (0/1) or boolean parameter.
type flexBool bool

func (b flexBool) Int64Value() (pgtype.Int8, error) {
	v := int64(0)
	if b {
		v = 1
	}
	return pgtype.Int8{Int64: v, Valid: true}, nil
}

func (b flexBool) BoolValue() (pgtype.Bool, error) {
	return pgtype.Bool{Bool: bool(b), Valid: true}, nil
}

// flexTime is a time for a timestamptz, date or text parameter; as text it
// is dbx.TimeLayout, as sqlitex binds it.
type flexTime time.Time

func (t flexTime) TimestamptzValue() (pgtype.Timestamptz, error) {
	return pgtype.Timestamptz{Time: time.Time(t), Valid: true}, nil
}

func (t flexTime) DateValue() (pgtype.Date, error) {
	return pgtype.Date{Time: time.Time(t), Valid: true}, nil
}

func (t flexTime) TextValue() (pgtype.Text, error) {
	return pgtype.Text{String: dbx.FormatTime(time.Time(t)), Valid: true}, nil
}

// encodeArgs wraps the values whose PostgreSQL type depends on the
// parameter's.
func encodeArgs(args []any) []any {
	for i, a := range args {
		switch v := a.(type) {
		case bool:
			args[i] = flexBool(v)
		case time.Time:
			args[i] = flexTime(v)
		}
	}
	return args
}

// boolDest scans a 0/1 integer or a boolean into a bool; NULL is false, as
// sqlitex reads it.
type boolDest struct{ p *bool }

func (d boolDest) ScanInt64(v pgtype.Int8) error {
	*d.p = v.Valid && v.Int64 != 0
	return nil
}

func (d boolDest) ScanBool(v pgtype.Bool) error {
	*d.p = v.Valid && v.Bool
	return nil
}

// scanDests wraps the destinations that need a conversion pgx does not make.
func scanDests(dest []any) []any {
	var out []any
	for i, d := range dest {
		if p, ok := d.(*bool); ok {
			if out == nil {
				out = append([]any(nil), dest...)
			}
			out[i] = boolDest{p}
		}
	}
	if out == nil {
		return dest
	}
	return out
}

package sqlitex

import (
	"context"
	"errors"
	"fmt"

	"abidata/internal/dbx"
)

// The driver-neutral interfaces this package implements.
var (
	_ dbx.Execer     = (*Conn)(nil)
	_ dbx.Tx         = (*Tx)(nil)
	_ dbx.Rows       = (*Rows)(nil)
	_ dbx.OriginRows = (*Rows)(nil)
)

// Tx is a write transaction on the writer connection (BEGIN IMMEDIATE).
type Tx struct {
	*Conn
}

// ErrTxDone is returned when a transaction is used after it finished.
var ErrTxDone = errors.New("sqlitex: transaction already finished")

// Immediate runs fn inside BEGIN IMMEDIATE; fn's error (or panic) rolls back.
func (c *Conn) Immediate(ctx context.Context, fn func(tx *Tx) error) (err error) {
	if c.inTx {
		return errors.New("sqlitex: nested transaction")
	}
	if _, err := c.Exec(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	c.inTx = true
	defer func() {
		c.inTx = false
		if p := recover(); p != nil {
			c.rollback()
			panic(p)
		}
		if err != nil {
			c.rollback()
		}
	}()
	if err = fn(&Tx{Conn: c}); err != nil {
		return err
	}
	// COMMIT must not be interrupted by an already-cancelled request context:
	// once fn succeeded, the outcome is decided here.
	if _, cerr := c.Exec(context.WithoutCancel(ctx), "COMMIT"); cerr != nil {
		return fmt.Errorf("sqlitex: commit: %w", cerr)
	}
	return nil
}

func (c *Conn) rollback() {
	if !c.c.GetAutocommit() {
		_, _ = c.Exec(context.Background(), "ROLLBACK")
	}
}

// Savepoint runs fn inside a named savepoint within the current transaction.
func (tx *Tx) Savepoint(ctx context.Context, fn func() error) (err error) {
	if _, err := tx.Exec(ctx, "SAVEPOINT sx"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, _ = tx.Exec(context.WithoutCancel(ctx), "ROLLBACK TO sx")
		}
		_, _ = tx.Exec(context.WithoutCancel(ctx), "RELEASE sx")
	}()
	return fn()
}

// ConnOf returns the SQLite connection behind a dbx value, for the few
// callers that need a SQLite-only capability (the authorizer, Describe,
// scripts). Such call sites are tagged "dialect: sqlite".
func ConnOf(q any) (*Conn, bool) {
	switch x := q.(type) {
	case *Conn:
		return x, true
	case *Tx:
		return x.Conn, true
	}
	return nil, false
}

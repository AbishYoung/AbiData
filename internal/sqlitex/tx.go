package sqlitex

import (
	"context"
	"errors"
	"fmt"
)

// Querier is the read surface shared by Conn and Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (*Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) *Row
}

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

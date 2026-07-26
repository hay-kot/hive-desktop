package store

import (
	"context"
	"fmt"
)

// Ctx returns a DB bound to the ambient transaction if the context carries
// one, and the receiver otherwise. A store method that participates in
// cross-domain work begins with `db := db.Ctx(ctx)`; one that does not is
// unaffected, which is what makes adoption incremental.
func (db *DB) Ctx(ctx context.Context) *DB {
	tx, ok := txFromContext(ctx)
	if !ok {
		return db
	}
	return &DB{conn: db.conn, tx: tx, queries: db.queries.WithTx(tx), pauseIngest: db.pauseIngest, pauseCommit: db.pauseCommit}
}

// WithinTx runs fn inside a transaction, joining an ambient one if the
// context already carries it rather than opening a second.
//
// Joining is not a nicety here. The DSN sets _txlock=immediate and the pool
// is capped at two connections, so a second BEGIN IMMEDIATE issued while the
// first still holds the write lock returns SQLITE_BUSY *immediately* --
// busy_timeout is ignored, because waiting could never succeed. A nesting
// implementation would deadlock the first time two domains composed.
//
// Only the outermost caller commits or rolls back. An inner fn that fails
// returns its error up to that caller, which is what rolls the whole unit
// back.
func (db *DB) WithinTx(ctx context.Context, fn func(context.Context, *DB) error) error {
	if tx, ok := txFromContext(ctx); ok {
		return fn(ctx, &DB{conn: db.conn, tx: tx, queries: db.queries.WithTx(tx), pauseIngest: db.pauseIngest, pauseCommit: db.pauseCommit})
	}

	txCtx, tx, err := WithTransaction(ctx, db)
	if err != nil {
		return err
	}

	if err := fn(txCtx, db.Ctx(txCtx)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction failed: %w (rollback also failed: %w)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

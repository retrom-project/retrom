package dbexec

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const rollbackTimeout = 5 * time.Second

// errPanicInTransaction is returned when a panic occurs inside Immediate.
var errPanicInTransaction = errors.New("dbexec: panic in immediate transaction")

// Immediate executes work inside a BEGIN IMMEDIATE transaction on an exclusive
// connection obtained from the pool. The work function receives the *sql.Conn
// as an Executor; all reads, constraint checks and writes use the same
// underlying connection.
//
// On success the transaction is committed and the connection returned to the
// pool. On failure, context cancellation or panic the transaction is rolled
// back with a bounded cleanup context. A rollback failure causes the
// underlying connection to be discarded instead of returned to the pool.
//
// The work function must be defined inside internal/repo/ and must not
// originate from an upper-layer parameter.
func Immediate(ctx context.Context, db *sql.DB, work func(Executor) error) (err error) {
	conn, connErr := db.Conn(ctx)
	if connErr != nil {
		return fmt.Errorf("dbexec: acquire connection: %w", connErr)
	}

	if _, execErr := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); execErr != nil {
		_ = conn.Close()
		return fmt.Errorf("dbexec: begin immediate: %w", execErr)
	}

	committed := false
	defer func() {
		if committed {
			_ = conn.Close()
			return
		}
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), rollbackTimeout,
		)
		defer cancel()
		if _, rbErr := conn.ExecContext(cleanupCtx, "ROLLBACK"); rbErr != nil {
			slog.Warn("rollback failed, discarding connection",
				"error", fmt.Sprintf("%T", rbErr))
			discardConnection(conn)
			return
		}
		_ = conn.Close()
	}()

	defer func() {
		if r := recover(); r != nil {
			if err == nil {
				err = fmt.Errorf("%w: %v", errPanicInTransaction, r)
			}
			panic(r)
		}
	}()

	if workErr := work(conn); workErr != nil {
		return workErr
	}

	if _, commitErr := conn.ExecContext(ctx, "COMMIT"); commitErr != nil {
		return fmt.Errorf("dbexec: commit: %w", commitErr)
	}
	committed = true
	return nil
}

// discardConnection marks the underlying driver connection as bad so the pool
// does not reuse a connection that may still be inside a transaction.
func discardConnection(conn *sql.Conn) {
	if rawErr := conn.Raw(func(any) error {
		return driver.ErrBadConn
	}); rawErr != nil && !errors.Is(rawErr, driver.ErrBadConn) {
		slog.Warn("discard connection failed",
			"error", fmt.Sprintf("%T", rawErr))
	}
	_ = conn.Close()
}

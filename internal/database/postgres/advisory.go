package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

// Coordination uses a separate pool: a waiting command cannot occupy a writer
// connection needed by the command holding its identity lock. No transaction
// remains open while a handler performs filesystem or network work.
func (db *handle) WithAdvisoryLock(ctx context.Context, key int64, work func() error) (err error) {
	connection, err := db.coordination.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire command coordination connection: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		// Also run after a cancelled lock acquisition: the server may have granted
		// the lock before the client noticed cancellation.
		_, unlockErr := connection.ExecContext(cleanup, "SELECT pg_advisory_unlock($1)", key)
		if unlockErr != nil {
			_ = connection.Raw(func(any) error { return driver.ErrBadConn })
		}
		err = errors.Join(err, unlockErr, connection.Close())
	}()
	if _, err := connection.ExecContext(ctx, "SET lock_timeout='0'"); err != nil {
		return fmt.Errorf("configure command coordination: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		return fmt.Errorf("coordinate command identity: %w", err)
	}
	return work()
}

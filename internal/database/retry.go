package database

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetryTransaction replays a database-only unit after PostgreSQL rolls back a
// serialization failure or deadlock. Callers must not perform external effects
// or accumulate results across attempts. Other errors are never replayed.
func RetryTransaction(ctx context.Context, db DB, work func(Tx) error) error {
	const attempts = 8
	for attempt := 0; ; attempt++ {
		err := InTransaction(ctx, db, nil, work)
		var state interface{ SQLState() string }
		if err == nil || attempt+1 == attempts || !errors.As(err, &state) ||
			(state.SQLState() != "40001" && state.SQLState() != "40P01") {
			return err
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("retry transaction: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

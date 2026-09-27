package blobrefs

import (
	"context"
	"fmt"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/blobgc"
	application "retrom/internal/service/payloadrelease"
)

// Commit records reference changes and their GC work in the same transaction.
func Commit(ctx context.Context, tx dbapi.Executor, before, after Snapshot) error {
	delta, err := Difference(before, after)
	if err != nil {
		return err
	}
	changes, err := Apply(ctx, tx, delta)
	if err != nil {
		return err
	}
	for _, id := range changes.Protected {
		if err := cancelCandidate(ctx, tx, id); err != nil {
			return err
		}
	}
	if len(changes.Zero) == 0 {
		return nil
	}
	transaction, ok := tx.(dbapi.Tx)
	if !ok {
		return ErrCount
	}
	scheduler, err := application.NewGCScheduler(
		nil,
		application.GCOptions{Now: func() time.Time { return time.UnixMilli(transaction.NowMS()) }},
	)
	if err != nil {
		return fmt.Errorf("prepare reference GC: %w", err)
	}
	if err := scheduler.StageInScope(ctx, blobgc.BindGC(tx, application.WorkerScope{}), changes.Zero); err != nil {
		return fmt.Errorf("stage dereferenced Blobs: %w", err)
	}
	return nil
}

func cancelCandidate(ctx context.Context, tx dbapi.Executor, id string) error {
	var expected int64
	if err := dbapi.QueryRowContext(
		ctx,
		tx,
		`SELECT count(*) FROM blob_gc_candidates WHERE blob_id=?`,
		id,
	).Scan(
		&expected,
	); err != nil {
		return fmt.Errorf("read restored Blob candidate: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM blob_gc_candidates WHERE blob_id=?`, id)
	if err != nil {
		return fmt.Errorf("invalidate restored Blob candidate: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm restored Blob candidate: %w", err)
	}
	if changed != expected {
		return ErrCount
	}
	return nil
}

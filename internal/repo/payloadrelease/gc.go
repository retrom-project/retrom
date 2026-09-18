package payloadrelease

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"
)

type GC struct{ database *sql.DB }

func NewGC(database *sql.DB) *GC { return &GC{database: database} }

func (repository *GC) LoadGCPage(ctx context.Context, cursor string, limit int) ([]application.GCBlob, error) {
	return (gcRecords{executor: repository.database}).Page(ctx, cursor, limit)
}

func (repository *GC) LoadGCCandidates(ctx context.Context) ([]application.GCBlob, error) {
	return (gcRecords{executor: repository.database}).Candidates(ctx)
}

func (repository *GC) LoadGCSelected(ctx context.Context, ids []string) ([]application.GCBlob, error) {
	return (gcRecords{executor: repository.database}).Selected(ctx, ids)
}

func (repository *GC) commitGCTx(ctx context.Context, label string, work func(application.GCScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s: %w", label, err)
	}
	defer dbexec.Rollback(tx)
	if err := work(BindGC(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", label, err)
	}
	return nil
}

func (repository *GC) CommitGCSchedule(ctx context.Context, batch application.GCScheduleBatch) error {
	return repository.commitGCTx(ctx, "GC schedule", func(scope application.GCScope) error {
		if err := scope.Write.Fence(ctx, batch.Selected); err != nil {
			return fmt.Errorf("fence GC schedule: %w", err)
		}
		for _, q := range batch.Queued {
			if err := scope.Write.Queue(ctx, q); err != nil {
				return fmt.Errorf("queue GC schedule: %w", err)
			}
		}
		return nil
	})
}

func (repository *GC) CommitImmediateGC(ctx context.Context, commit application.GCImmediateCommit) error {
	return repository.commitGCTx(ctx, "immediate GC", func(scope application.GCScope) error {
		if err := scope.Write.Fence(ctx, commit.Selected); err != nil {
			return fmt.Errorf("fence immediate GC: %w", err)
		}
		for _, change := range commit.Changes {
			if err := scope.Write.Advance(ctx, change); err != nil {
				return fmt.Errorf("advance immediate GC: %w", err)
			}
		}
		if err := scope.Write.Audit(ctx, commit.Audit); err != nil {
			return fmt.Errorf("audit immediate GC: %w", err)
		}
		return nil
	})
}

func (repository *GC) CommitGCCancellation(ctx context.Context, batch application.GCCancellationBatch) error {
	return repository.commitGCTx(ctx, "GC cancellation", func(scope application.GCScope) error {
		if err := scope.Write.Fence(ctx, batch.Selected); err != nil {
			return fmt.Errorf("fence GC cancellation: %w", err)
		}
		for _, cancel := range batch.Cancellations {
			if err := scope.Write.Cancel(ctx, cancel); err != nil {
				return fmt.Errorf("cancel GC entry: %w", err)
			}
		}
		return nil
	})
}

type gcRecords struct{ executor dbexec.Executor }

func BindGC(executor dbexec.Executor) application.GCScope {
	records := gcRecords{executor: executor}
	return application.GCScope{Read: records, Write: records}
}

func gcWrite(result sql.Result, err error) error {
	return gcWriteCount(result, err, 1)
}

func gcWriteCount(result sql.Result, err error, expected int64) error {
	if err != nil {
		return fmt.Errorf("write GC record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count GC writes: %w", err)
	}
	if count != expected {
		return application.ErrGCSnapshotChanged
	}
	return nil
}

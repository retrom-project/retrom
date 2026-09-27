package filedeletion

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/cleanupjobs"
)

type DeletionQueue struct {
	database   dbapi.DB
	bindWorker func(dbapi.Executor) application.WorkerScope
}

func NewQueue(database dbapi.DB, bindWorker func(dbapi.Executor) application.WorkerScope) *DeletionQueue {
	return &DeletionQueue{database: database, bindWorker: bindWorker}
}

func (repository *DeletionQueue) WithDeletion(ctx context.Context, run func(application.DeletionScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin file deletion transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := run(BindQueue(tx, repository.bindWorker(tx))); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit file deletion transaction: %w", err)
	}
	return nil
}

type deletionRecords struct {
	executor dbapi.Executor
	worker   application.WorkerScope
}

func BindQueue(executor dbapi.Executor, worker application.WorkerScope) application.DeletionScope {
	records := deletionRecords{executor: executor, worker: worker}
	return application.DeletionScope{Read: records, Write: records}
}

func deletionWrite(result sql.Result, err error) error {
	return deletionWriteCount(result, err, 1)
}

func deletionWriteCount(result sql.Result, err error, expected int64) error {
	if err != nil {
		return fmt.Errorf("write file deletion record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count file deletion writes: %w", err)
	}
	if count != expected {
		return application.ErrDeletionSnapshotChanged
	}
	return nil
}

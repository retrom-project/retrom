package blobgc

import (
	"context"
	"database/sql"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/payloadrelease"
)

type GC struct {
	database   dbapi.DB
	bindWorker func(dbapi.Executor) application.WorkerScope
}

func NewGC(database dbapi.DB, bindWorker func(dbapi.Executor) application.WorkerScope) *GC {
	return &GC{database: database, bindWorker: bindWorker}
}

func (repository *GC) WithGC(ctx context.Context, run func(application.GCScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin GC transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := run(BindGC(tx, repository.bindWorker(tx))); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit GC transaction: %w", err)
	}
	return nil
}

type gcRecords struct {
	executor dbapi.Executor
	worker   application.WorkerScope
}

func BindGC(executor dbapi.Executor, worker application.WorkerScope) application.GCScope {
	records := gcRecords{executor: executor, worker: worker}
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

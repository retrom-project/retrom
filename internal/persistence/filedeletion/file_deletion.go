package filedeletion

import (
	"context"
	"fmt"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/cleanupjobs"
)

type FileDeletion struct {
	database   dbapi.DB
	bindWorker func(dbapi.Executor) application.WorkerScope
}

func NewFileDeletion(database dbapi.DB, bindWorker func(dbapi.Executor) application.WorkerScope) *FileDeletion {
	return &FileDeletion{database: database, bindWorker: bindWorker}
}

func (repository *FileDeletion) WithFileDeletion(
	ctx context.Context,
	run func(application.FileDeletionScope) error,
) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin file deletion transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := fileDeletionRecords{executor: tx, worker: repository.bindWorker(tx)}
	if err := run(application.FileDeletionScope{Read: records, Write: records, Worker: records.worker}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit file deletion transaction: %w", err)
	}
	return nil
}

type fileDeletionRecords struct {
	executor dbapi.Executor
	worker   application.WorkerScope
}

func (records fileDeletionRecords) Facts(
	ctx context.Context,
	id string,
	_ string,
) (application.FileDeletionFacts, error) {
	var facts application.FileDeletionFacts
	blobs, err := deletionRecords(records).Selected(ctx, []string{id})
	if err != nil {
		return facts, fmt.Errorf("read file deletion Blob: %w", err)
	}
	if len(blobs) == 1 {
		facts.Found = true
		facts.Blob = blobs[0]
		if err := dbapi.QueryRowContext(
			ctx, records.executor, `SELECT count(*) FROM archive_entries WHERE archive_blob_id=?`, id).
			Scan(&facts.ArchiveEntries); err != nil {
			return application.FileDeletionFacts{}, fmt.Errorf("read file deletion archive size: %w", err)
		}
	}
	return facts, nil
}

func (records fileDeletionRecords) cancelCandidate(ctx context.Context, blob application.DeletionFile) error {
	if !blob.HasCandidate {
		return nil
	}
	result, err := records.executor.ExecContext(ctx, `DELETE FROM file_deletions`+deletionCandidateFence,
		deletionCandidateArguments(blob)...)
	if err := deletionWrite(result, err); err != nil {
		return fmt.Errorf("remove file deletion candidate: %w", err)
	}
	return nil
}

func (records fileDeletionRecords) Remove(ctx context.Context, facts application.FileDeletionFacts) error {
	if err := deletionRecords(records).Fence(ctx, []application.DeletionFile{facts.Blob}); err != nil {
		return fmt.Errorf("fence file deletion before deletion: %w", err)
	}
	result, err := records.executor.ExecContext(
		ctx,
		`DELETE FROM archive_entries WHERE archive_blob_id=?`,
		facts.Blob.ID,
	)
	if err := deletionWriteCount(result, err, facts.ArchiveEntries); err != nil {
		return fmt.Errorf("remove file deletion archive index: %w", err)
	}
	if err := records.cancelCandidate(ctx, facts.Blob); err != nil {
		return err
	}
	result, err = records.executor.ExecContext(ctx, `DELETE FROM stored_files WHERE id=? AND sha256=? AND size_bytes=?`,
		facts.Blob.ID, facts.Blob.Digest, facts.Blob.SizeBytes)
	if err := deletionWrite(result, err); err != nil {
		return fmt.Errorf("remove file deletion Blob: %w", err)
	}
	return nil
}

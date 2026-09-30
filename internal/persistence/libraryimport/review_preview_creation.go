package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	libraryservice "retrom/internal/service/libraryimport"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

type ReviewPreviewCreation struct{ database dbapi.DB }

func NewReviewPreviewCreation(database dbapi.DB) *ReviewPreviewCreation {
	return &ReviewPreviewCreation{database: database}
}

type reviewPreviewRecords struct{ executor dbapi.Executor }

func (repository *ReviewPreviewCreation) Replay(
	ctx context.Context,
	actor, key string,
) (application.PreviewReceipt, bool, error) {
	return (reviewPreviewRecords{executor: repository.database}).Replay(ctx, actor, key)
}

func (records reviewPreviewRecords) Replay(
	ctx context.Context,
	actor, key string,
) (application.PreviewReceipt, bool, error) {
	var receipt application.PreviewReceipt
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT preview.id,binding.import_item_id,preview.restore_from_preview_id
FROM runtime_preview_sessions preview JOIN review_preview_bindings binding ON binding.preview_session_id=preview.id
WHERE preview.actor_user_id=? AND preview.idempotency_key=?`, actor, key).
		Scan(&receipt.ID, &receipt.ImportItemID, &receipt.RestoreFromPreviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PreviewReceipt{}, false, nil
	}
	if err != nil {
		return application.PreviewReceipt{}, false, fmt.Errorf("query preview replay: %w", err)
	}
	return receipt, true, nil
}

func (repository *ReviewPreviewCreation) Snapshot(
	ctx context.Context,
	itemID string,
) (application.PreviewSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("begin preview snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
	snapshot, found, err := readReviewPreviewInput(ctx, tx, itemID)
	if err != nil || !found {
		return application.PreviewSnapshot{}, found, err
	}
	if err := tx.Commit(); err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("commit preview snapshot: %w", err)
	}
	return snapshot, true, nil
}

func readReviewPreviewInput(
	ctx context.Context, executor dbapi.Executor, itemID string,
) (application.PreviewSnapshot, bool, error) {
	source, found, err := previewCreationSource(ctx, executor, itemID)
	if err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("read preview input: %w", err)
	}
	if !found {
		return application.PreviewSnapshot{}, false, nil
	}
	sourceFiles, err := previewInputFiles(ctx, executor, source.SourceSnapshotID)
	if err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("read preview input: %w", err)
	}
	runtimeFiles, err := ReadReviewRuntimeFiles(ctx, executor, itemID)
	if err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("read preview input: %w", err)
	}
	validationFiles := make([]application.PreviewFile, 0, len(runtimeFiles))
	for _, file := range runtimeFiles {
		validationFiles = append(validationFiles, application.PreviewFile{
			Role: file.Role, LogicalName: file.LogicalName, FileRecord: file.FileRecord, SortOrder: file.SortOrder,
		})
	}
	deviceBIOS, err := previewMAMEDeviceBIOS(ctx, executor, source.TargetID)
	if err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("read preview input: %w", err)
	}
	validationFiles = append(validationFiles, deviceBIOS...)
	return application.PreviewSnapshot{
		Source: source, SourceFiles: sourceFiles, ValidationFiles: validationFiles,
	}, true, nil
}

func (repository *ReviewPreviewCreation) WithCreation(
	ctx context.Context,
	work func(libraryservice.ReviewPreviewScope) error,
) error {
	// The application supplies the shared single-writer database handle.
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin preview creation: %w", err)
	}
	defer dbapi.Rollback(tx)
	if err := work(reviewPreviewRecords{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit preview creation: %w", err)
	}
	return nil
}

func (repository *ReviewPreviewCreation) Restore(
	ctx context.Context, id string,
) (application.PreviewRestore, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &dbapi.TxOptions{ReadOnly: true})
	if err != nil {
		return application.PreviewRestore{}, false, fmt.Errorf("begin restore snapshot: %w", err)
	}
	defer dbapi.Rollback(tx)
	restore, found, err := (reviewPreviewRecords{executor: tx}).Restore(ctx, id)
	if err != nil || !found {
		return application.PreviewRestore{}, found, err
	}
	if err := tx.Commit(); err != nil {
		return application.PreviewRestore{}, false, fmt.Errorf("commit restore snapshot: %w", err)
	}
	return restore, true, nil
}

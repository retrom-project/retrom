package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/launch"
)

type PreviewCreation struct{ database *sql.DB }

func NewPreviewCreation(database *sql.DB) *PreviewCreation {
	return &PreviewCreation{database: database}
}

type previewCreationRecords struct{ executor dbexec.Executor }

func (repository *PreviewCreation) Replay(
	ctx context.Context,
	actor, key string,
) (application.PreviewReceipt, bool, error) {
	return (previewCreationRecords{executor: repository.database}).Replay(ctx, actor, key)
}

func (records previewCreationRecords) Replay(
	ctx context.Context,
	actor, key string,
) (application.PreviewReceipt, bool, error) {
	var receipt application.PreviewReceipt
	err := records.executor.QueryRowContext(ctx, `SELECT id,import_item_id,restore_from_preview_id
FROM review_preview_sessions WHERE actor_user_id=? AND idempotency_key=?`, actor, key).
		Scan(&receipt.ID, &receipt.ImportItemID, &receipt.RestoreFromPreviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PreviewReceipt{}, false, nil
	}
	if err != nil {
		return application.PreviewReceipt{}, false, fmt.Errorf("query preview replay: %w", err)
	}
	return receipt, true, nil
}

func (repository *PreviewCreation) Snapshot(
	ctx context.Context,
	itemID string,
) (application.PreviewSnapshot, bool, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("begin preview snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	source, found, err := previewCreationSource(ctx, tx, itemID)
	if err != nil || !found {
		return application.PreviewSnapshot{}, false, err
	}
	sourceFiles, err := previewInputFiles(ctx, tx, source.SourceSnapshotID, false)
	if err != nil {
		return application.PreviewSnapshot{}, false, err
	}
	validationFiles, err := previewInputFiles(ctx, tx, source.ValidationID, true)
	if err != nil {
		return application.PreviewSnapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return application.PreviewSnapshot{}, false, fmt.Errorf("commit preview snapshot: %w", err)
	}
	return application.PreviewSnapshot{
		Source: source, SourceFiles: sourceFiles, ValidationFiles: validationFiles,
	}, true, nil
}

func (repository *PreviewCreation) WithCreation(
	ctx context.Context,
	work func(application.PreviewCreationScope) error,
) error {
	// The application supplies the shared single-writer database handle.
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin preview creation: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(previewCreationRecords{executor: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit preview creation: %w", err)
	}
	return nil
}

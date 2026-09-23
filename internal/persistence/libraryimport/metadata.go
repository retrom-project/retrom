package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/libraryimport"
)

type Metadata struct{ database *sql.DB }

func NewMetadata(database *sql.DB) *Metadata { return &Metadata{database: database} }

func (repository *Metadata) WithMetadata(ctx context.Context, work func(application.MetadataScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin server review metadata: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(BindMetadata(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit server review metadata: %w", err)
	}
	return nil
}

type metadataRecords struct{ executor dbexec.Executor }

// BindMetadata joins a caller-owned transaction; the caller commits or rolls
// back the complete business operation, including any source handoff.
func BindMetadata(executor dbexec.Executor) application.MetadataScope {
	return metadataRecords{executor: executor}
}

func (records metadataRecords) CurrentMetadata(ctx context.Context, itemID string) (application.MetadataDraft, error) {
	var result application.MetadataDraft
	err := records.executor.QueryRowContext(ctx, `
SELECT draft.metadata_json,draft.review_version FROM import_items draft
JOIN import_items item ON item.id=draft.id
WHERE draft.id=? AND item.state='REVIEW_PENDING'`, itemID).
		Scan(&result.MetadataJSON, &result.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return application.MetadataDraft{}, application.ErrInvalid
	}
	if err != nil {
		return application.MetadataDraft{}, fmt.Errorf("read review metadata draft: %w", err)
	}
	return result, nil
}

func (records metadataRecords) SaveMetadata(ctx context.Context, change application.MetadataChange) error {
	result, err := recordstore.UpdateReviewItems(ctx, records.executor, recordstore.Update{
		Set: `metadata_json=?,review_version=review_version+1,review_updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND review_version=? AND metadata_json=? AND EXISTS(
SELECT 1 FROM import_items item WHERE item.id=import_items.id AND item.state='REVIEW_PENDING')`,
			Args: []any{change.ItemID, change.Before.Version, change.Before.MetadataJSON},
		},
		Values: []any{change.MetadataJSON, change.NowMS},
	})
	if err := requireMetadataChange(result, err); err != nil {
		return err
	}
	result, err = recordstore.UpdateImportItems(ctx, records.executor, recordstore.Update{
		Set: `search_text=?`, Scope: recordstore.Scope{
			Where: `id=? AND state='REVIEW_PENDING'`, Args: []any{change.ItemID},
		}, Values: []any{change.SearchText},
	})
	if err := requireMetadataChange(result, err); err != nil {
		return err
	}
	return nil
}

func requireMetadataChange(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write server review metadata: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count server review metadata changes: %w", err)
	}
	if affected != 1 {
		return application.ErrVersionConflict
	}
	return nil
}

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
SELECT draft.metadata_json,draft.version FROM review_drafts draft
JOIN import_items item ON item.id=draft.import_item_id
WHERE draft.import_item_id=? AND item.state='REVIEW_PENDING'`, itemID).
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
	result, err := recordstore.UpdateReviewDrafts(ctx, records.executor, recordstore.Update{
		Set: `metadata_json=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `import_item_id=? AND version=? AND metadata_json=? AND EXISTS(
SELECT 1 FROM import_items item WHERE item.id=review_drafts.import_item_id AND item.state='REVIEW_PENDING')`,
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
	return records.appendMetadataEvent(ctx, change)
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

func (records metadataRecords) appendMetadataEvent(ctx context.Context, change application.MetadataChange) error {
	const emptyEvidence = `{"schemaVersion":2}`
	const diff = `{"metadataChanged":true,"schemaVersion":2}`
	_, err := recordstore.CreateReviewEvents(ctx, records.executor, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,before_json,
after_json,diff_json,config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'DRAFT_SAVED',?,?,?,?,?,?,?,?,?,?)`,
		change.Audit.ID, change.ItemID, change.Audit.ActorKind, change.Audit.ActorUserID, change.Audit.ActorLabel,
		change.Audit.BeforeJSON, change.Audit.AfterJSON, diff, emptyEvidence, emptyEvidence, emptyEvidence, change.NowMS)
	if err != nil {
		return fmt.Errorf("append server review metadata audit: %w", err)
	}
	return nil
}

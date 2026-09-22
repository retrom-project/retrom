package sourceimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/sourceimport"
)

type Companions struct{ database *sql.DB }

func NewCompanions(database *sql.DB) *Companions { return &Companions{database: database} }
func (repository *Companions) WithCompanions(ctx context.Context, work func(application.CompanionScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Source companion transaction: %w", err)
	}
	defer dbexec.Rollback(tx)
	records := companionRecords{tx: tx}
	if err := work(application.CompanionScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Source companion transaction: %w", err)
	}
	return nil
}

type companionRecords struct{ tx *sql.Tx }

func (records companionRecords) Owner(ctx context.Context, itemID string) (application.OwnedItem, error) {
	before, err := itemWorkRecords(records).Current(ctx, itemID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	if err := records.tx.QueryRowContext(ctx, `SELECT collection.target_platform_instance_id,collection.target_platform_id,
COALESCE(collection.target_dat_version_id,'') FROM source_import_items item
JOIN source_import_collections collection ON collection.id=item.collection_id
WHERE item.id=? AND collection.mapping_action='IMPORT'`, itemID).Scan(
		&before.Item.TargetPlatformID, &before.Item.TargetPlatformKind, &before.Item.TargetDATVersionID); err != nil {
		return application.OwnedItem{}, fmt.Errorf("read Source companion target: %w", err)
	}
	before.Item.Files, err = itemWorkRecords(records).files(ctx, itemID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	return before, nil
}

func (records companionRecords) Register(
	ctx context.Context,
	change application.CompanionRegistration,
) (string, error) {
	// Fence the worker and selected source immediately before catalog insertion; no host IO runs in this scope.
	var valid bool
	if err := records.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_import_items item WHERE
 item.id=? AND item.import_id=? AND item.version=? AND item.execution_state=? AND item.execution_state='COPYING'`+
		itemExecutionFence+`)`, itemFenceArgs(change.Before, change.NowMS)...).Scan(&valid); err != nil {
		return "", fmt.Errorf("check Source companion owner: %w", err)
	}
	if !valid {
		return "", application.ErrVersionConflict
	}
	candidates, err := records.Candidates(ctx, change.Before.Item)
	if err != nil {
		return "", err
	}
	valid = false
	for _, candidate := range candidates {
		if candidate == change.Candidate {
			valid = true
			break
		}
	}
	if !valid {
		return "", application.ErrVersionConflict
	}
	return registerVerifiedMaterial(ctx, records.tx, change.Blob, "application/zip", change.NowMS)
}

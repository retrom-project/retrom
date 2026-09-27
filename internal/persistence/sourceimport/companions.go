package sourceimport

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

type Companions struct{ database dbapi.DB }

func NewCompanions(database dbapi.DB) *Companions { return &Companions{database: database} }
func (repository *Companions) WithCompanions(ctx context.Context, work func(application.CompanionScope) error) error {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Source companion transaction: %w", err)
	}
	defer dbapi.Rollback(tx)
	records := companionRecords{tx: tx}
	if err := work(application.CompanionScope{Read: records, Write: records}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit Source companion transaction: %w", err)
	}
	return nil
}

type companionRecords struct{ tx dbapi.Tx }

func (records companionRecords) Owner(ctx context.Context, itemID string) (application.OwnedItem, error) {
	before, err := itemWorkRecords(records).Current(ctx, itemID)
	if err != nil {
		return application.OwnedItem{}, err
	}
	if err := dbapi.QueryRowContext(
		ctx, records.tx, `SELECT collection.target_platform_instance_id,collection.target_platform_id,
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
	if err := dbapi.QueryRowContext(ctx, records.tx, `SELECT EXISTS(SELECT 1 FROM source_import_items item WHERE
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
	id, err := registerVerifiedMaterial(ctx, records.tx, change.Blob, "application/zip", change.NowMS)
	if err != nil {
		return "", err
	}
	owner, candidate := change.Before.Item.ID, change.Candidate.ItemID
	if _, err := recordstore.CreateReferences(ctx, records.tx, "source_import_item_companions", `
 INSERT INTO source_import_item_companions(item_id,candidate_item_id,blob_id,created_at_ms)
 VALUES(?,?,?,?) ON CONFLICT(item_id,candidate_item_id) DO NOTHING`, owner, candidate, id, change.NowMS); err != nil {
		return "", fmt.Errorf("protect Source companion: %w", err)
	}
	var current string
	if err := dbapi.QueryRowContext(ctx, records.tx, `SELECT blob_id FROM source_import_item_companions
 WHERE item_id=? AND candidate_item_id=?`, owner, candidate).Scan(&current); err != nil {
		return "", fmt.Errorf("verify Source companion: %w", err)
	}
	if current != id {
		return "", application.ErrVersionConflict
	}
	return id, nil
}

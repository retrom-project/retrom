package sourceimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/fileownership"

	"retrom/internal/persistence/recordstore"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

type Companions struct{ database dbapi.DB }

func NewCompanions(database dbapi.DB) *Companions { return &Companions{database: database} }

func (repository *Companions) WithCompanions(
	ctx context.Context,
	work func(application.CompanionScope) error,
) error {
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
	owner, candidate := change.Before.Item.ID, change.Candidate.ItemID
	var existing, digest string
	var size int64
	err = dbapi.QueryRowContext(ctx, records.tx, `SELECT file.id,file.sha256,file.size_bytes
 FROM source_import_item_companions companion JOIN stored_files file ON file.id=companion.blob_id
 WHERE companion.item_id=? AND companion.candidate_item_id=? AND file.owner_kind='SOURCE_IMPORT_ITEM'
 AND file.owner_id=? AND file.retired_at_ms IS NULL`, owner, candidate, owner).Scan(&existing, &digest, &size)
	if err == nil {
		if digest != change.Blob.SHA256 || size != change.Blob.Size {
			return "", application.ErrVersionConflict
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read existing Source companion: %w", err)
	}
	id, err := registerVerifiedMaterial(ctx, records.tx, change.Blob, "application/zip", change.NowMS)
	if err != nil {
		return "", err
	}
	if err := fileownership.Adopt(
		ctx,
		records.tx,
		id,
		fileownership.Owner{Kind: "SOURCE_IMPORT_ITEM", ID: owner},
	); err != nil {
		return "", fmt.Errorf("companions: %w", err)
	}
	if _, err := recordstore.InsertRows(ctx, records.tx, "source_import_item_companions", `
 INSERT INTO source_import_item_companions(item_id,candidate_item_id,blob_id,created_at_ms)
 VALUES(?,?,?,?)`, owner, candidate, id, change.NowMS); err != nil {
		return "", fmt.Errorf("record Source companion: %w", err)
	}
	return id, nil
}

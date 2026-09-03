package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/tagging"
)

func (service *Service) UpdateMappings(
	ctx context.Context,
	importID string,
	expectedVersion int64,
	mappings []Mapping,
) (Summary, error) {
	if len(mappings) < 1 || len(mappings) > 100 {
		return Summary{}, ErrInvalid
	}
	if !mappingTagArraysPresent(mappings) {
		return Summary{}, ErrInvalid
	}
	seen := map[string]struct{}{}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/mapping transaction: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var state, actorUserID string
	var version int64
	if err := transaction.QueryRowContext(
		ctx, `SELECT state,version,created_by_user_id FROM pegasus_imports WHERE id=?`, importID,
	).Scan(&state, &version, &actorUserID); err != nil {
		return Summary{}, ErrNotFound
	}
	if state != "AWAITING_MAPPING" {
		return Summary{}, ErrMapping
	}
	if version != expectedVersion {
		return Summary{}, ErrVersionConflict
	}
	if principal, ok := authn.PrincipalFromContext(ctx); ok && principal.UserID != "" {
		actorUserID = principal.UserID
	}
	now := service.now().UnixMilli()
	if err := service.applyMappings(ctx, transaction, importID, actorUserID, now, mappings, seen); err != nil {
		return Summary{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports SET
mapped_collection_count=(SELECT count(*) FROM pegasus_import_collections WHERE import_id=? AND mapping_action='IMPORT'),
skipped_collection_count=(SELECT count(*) FROM pegasus_import_collections WHERE import_id=? AND mapping_action='SKIP'),
mapping_version=mapping_version+1,version=version+1,updated_at_ms=?
WHERE id=?`, importID, importID, now, importID); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/update mapping counts: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Summary{}, fmt.Errorf("pegasusimport/commit mappings: %w", err)
	}
	return service.Get(ctx, importID)
}

func (service *Service) applyMappings(
	ctx context.Context,
	transaction *sql.Tx,
	importID, actorUserID string,
	now int64,
	mappings []Mapping,
	seen map[string]struct{},
) error {
	for _, mapping := range mappings {
		if mapping.CollectionID == "" || mappingAlreadySeen(seen, mapping.CollectionID) {
			return ErrInvalid
		}
		seen[mapping.CollectionID] = struct{}{}
		if err := service.applyMapping(ctx, transaction, importID, mapping, actorUserID, now); err != nil {
			return mappingUpdateError(err)
		}
	}
	return nil
}

func mappingTagArraysPresent(mappings []Mapping) bool {
	for _, mapping := range mappings {
		if mapping.TagIDs == nil {
			return false
		}
	}
	return true
}

func mappingUpdateError(err error) error {
	if errors.Is(err, tagging.ErrReferenceInvalid) || errors.Is(err, tagging.ErrAssignmentLimitExceeded) {
		return fmt.Errorf("pegasusimport/update mapping tags: %w", err)
	}
	return ErrInvalid
}

func mappingAlreadySeen(seen map[string]struct{}, collectionID string) bool {
	_, exists := seen[collectionID]
	return exists
}

func (service *Service) applyMapping(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	mapping Mapping,
	actorUserID string,
	now int64,
) error {
	switch mapping.Action {
	case "SKIP":
		return service.skipCollection(ctx, transaction, importID, mapping, actorUserID, now)
	case "IMPORT":
		return service.importCollection(ctx, transaction, importID, mapping, actorUserID, now)
	default:
		return ErrInvalid
	}
}

func (service *Service) skipCollection(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	mapping Mapping,
	actorUserID string,
	now int64,
) error {
	if mapping.PlatformInstanceID != "" || len(mapping.TagIDs) != 0 {
		return ErrInvalid
	}
	if _, err := service.tags.ReplacePegasusCollectionTags(
		ctx, transaction, mapping.CollectionID, mapping.TagIDs, actorUserID, now,
	); err != nil {
		return fmt.Errorf("pegasusimport/clear skipped collection tags: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_collections
SET mapping_action='SKIP',
	tag_snapshot_json='[]',
target_platform_instance_id=NULL,
target_platform_instance_version=NULL,
target_platform_id=NULL,
target_default_core_id=NULL,
target_provider_id=NULL,
target_id=NULL,
target_contract_sha256=NULL,
target_dat_version_id=NULL,
updated_at_ms=?
WHERE id=?
AND import_id=?`, now, mapping.CollectionID, importID)
	if err != nil || rowsAffected(result) != 1 {
		return ErrInvalid
	}
	return nil
}

func (service *Service) importCollection(
	ctx context.Context,
	transaction *sql.Tx,
	importID string,
	mapping Mapping,
	actorUserID string,
	now int64,
) error {
	var instanceVersion int64
	var platformID, coreID, providerID, targetID, targetContract string
	var datID sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT instance.version,
instance.platform_id,
instance.default_core_id,
target.provider_id,
target.target_id,
target.target_contract_sha256,
(SELECT id FROM dat_versions WHERE provider_id=target.provider_id AND target_id=target.target_id AND is_active=1)
FROM platform_instances instance
JOIN platforms platform ON platform.id=instance.platform_id AND platform.enabled=1
JOIN cores core ON core.id=instance.default_core_id AND core.enabled=1
JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=instance.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE instance.id=?
AND instance.enabled=1
AND instance.deleted_at_ms IS NULL`, mapping.PlatformInstanceID).
		Scan(&instanceVersion, &platformID, &coreID, &providerID, &targetID, &targetContract, &datID)
	if err != nil {
		return ErrInvalid
	}
	references, err := service.tags.ReplacePegasusCollectionTags(
		ctx, transaction, mapping.CollectionID, mapping.TagIDs, actorUserID, now,
	)
	if err != nil {
		return fmt.Errorf("pegasusimport/replace collection tags: %w", err)
	}
	tagSnapshot, err := json.Marshal(references)
	if err != nil {
		return fmt.Errorf("pegasusimport/encode tag snapshot: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_collections
SET mapping_action='IMPORT',
	tag_snapshot_json=?,
target_platform_instance_id=?,
target_platform_instance_version=?,
target_platform_id=?,
target_default_core_id=?,
target_provider_id=?,
target_id=?,
target_contract_sha256=?,
target_dat_version_id=?,
updated_at_ms=?
WHERE id=?
AND import_id=?`, string(tagSnapshot), mapping.PlatformInstanceID, instanceVersion, platformID, coreID, providerID,
		targetID, targetContract, nullable(datID), now, mapping.CollectionID, importID)
	if err != nil || rowsAffected(result) != 1 {
		return ErrInvalid
	}
	return nil
}

func rowsAffected(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	value, _ := result.RowsAffected()
	return value
}

func nullable(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

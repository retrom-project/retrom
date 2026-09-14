package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/emulationstationimport"
)

func (records companionRecords) Owner(ctx context.Context, id string) (application.CompanionOwner, error) {
	owned, err := itemWorkRecords(records).Item(ctx, id)
	if err != nil {
		return application.CompanionOwner{}, err
	}
	if err := itemWorkRecords(records).materials(ctx, &owned.Item); err != nil {
		return application.CompanionOwner{}, err
	}
	result := application.CompanionOwner{Before: owned}
	target := &result.Mapping
	err = records.executor.QueryRowContext(ctx, `SELECT collection.id,plan.mapping_version,
collection.target_platform_instance_id,collection.target_platform_instance_version,collection.target_platform_id,
collection.target_default_core_id,collection.target_provider_id,collection.target_id,collection.target_dat_version_id
FROM emulationstation_import_items item
JOIN emulationstation_import_collections collection ON collection.id=item.collection_id
 AND collection.import_id=item.import_id
JOIN emulationstation_imports plan ON plan.id=item.import_id
WHERE item.id=? AND collection.mapping_action='IMPORT'`, id).Scan(
		&result.CollectionID,
		&result.MappingVersion,

		&target.InstanceID,
		&target.InstanceVersion,
		&target.PlatformID,
		&target.CoreID,
		&target.ProviderID,
		&target.TargetID,
		&target.DATVersionID,
	)
	if err != nil {
		return application.CompanionOwner{}, fmt.Errorf("read EmulationStation companion mapping: %w", err)
	}
	return result, nil
}

func (records companionRecords) Target(ctx context.Context, id string) (application.MappingTarget, bool, error) {
	return (mappingRecords{executor: records.executor}).EligibleTarget(ctx, id)
}

func (records companionRecords) Candidates(
	ctx context.Context,
	owner application.CompanionOwner,
) ([]application.CompanionFile, error) {
	item := owner.Before.Item
	rows, err := records.executor.QueryContext(ctx, `SELECT candidate.id,collection.id,file.ordinal,
file.relative_path,file.size_bytes,file.source_facts_digest
FROM emulationstation_import_items candidate
JOIN emulationstation_import_collections collection ON collection.id=candidate.collection_id
JOIN emulationstation_import_item_files file ON file.item_id=candidate.id
WHERE candidate.import_id=? AND candidate.id<>? AND candidate.discovery_state='READY'
AND collection.mapping_action='IMPORT' AND collection.target_platform_instance_id=?
AND collection.target_dat_version_id=?
AND (SELECT count(*) FROM emulationstation_import_item_files own WHERE own.item_id=candidate.id)=1
ORDER BY file.relative_path,candidate.id,file.ordinal`,
		item.ImportID, item.ID, item.TargetPlatformID, item.TargetDATVersionID)
	if err != nil {
		return nil, fmt.Errorf("query EmulationStation companion files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []application.CompanionFile{}
	for rows.Next() {
		var file application.CompanionFile
		if err := rows.Scan(
			&file.ItemID, &file.CollectionID, &file.Ordinal, &file.Path, &file.Size, &file.Facts,
		); err != nil {
			return nil, fmt.Errorf("read EmulationStation companion file: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate EmulationStation companion files: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close EmulationStation companion files: %w", err)
	}
	return result, nil
}

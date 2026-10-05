package sourceimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/sourceimport"
)

func (records companionRecords) Candidates(
	ctx context.Context,
	item application.ExecutionItem,
) ([]application.CompanionCandidate, error) {
	rows, err := records.tx.QueryContext(ctx, `
SELECT candidate.id,file.ordinal,file.relative_path,file.size_bytes,file.source_facts_digest
FROM source_import_items candidate
JOIN source_import_collections collection ON collection.id=candidate.collection_id
JOIN source_import_item_files file ON file.item_id=candidate.id
WHERE candidate.import_id=? AND candidate.id<>? AND candidate.discovery_state='READY'
AND collection.mapping_action='IMPORT' AND collection.target_platform_instance_id=?
AND collection.target_dat_version_id=?
AND (SELECT count(*) FROM source_import_item_files own WHERE own.item_id=candidate.id)=1
ORDER BY file.relative_path`, item.ImportID, item.ID, item.TargetPlatformID, item.TargetDATVersionID)
	if err != nil {
		return nil, fmt.Errorf("sourceimport/query arcade companions: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []application.CompanionCandidate{}
	for rows.Next() {
		var candidate application.CompanionCandidate
		file := &candidate.File
		if err := rows.Scan(&candidate.ItemID, &file.Ordinal, &file.Path, &file.Size, &file.Facts); err != nil {
			return nil, fmt.Errorf("sourceimport/scan arcade companion: %w", err)
		}

		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sourceimport/iterate arcade companions: %w", err)
	}
	return result, nil
}

func (records companionRecords) Dependencies(
	ctx context.Context,
	datVersionID, machine string,
) ([]string, error) {
	rows, err := records.tx.QueryContext(ctx, `
WITH RECURSIVE relation(parent,machine) AS (
 SELECT machine_name,cloneof FROM dat_machines WHERE dat_version_id=$1 AND cloneof IS NOT NULL
 UNION
 SELECT machine_name,romof FROM dat_machines WHERE dat_version_id=$1 AND romof IS NOT NULL
), dependency(machine) AS (
 SELECT machine FROM relation WHERE parent=$2
 UNION
 SELECT relation.machine FROM relation JOIN dependency ON relation.parent=dependency.machine
)
SELECT machine FROM dependency WHERE machine<>$2 ORDER BY machine`, datVersionID, machine,
	)
	if err != nil {
		return nil, fmt.Errorf("sourceimport/query arcade dependency closure: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []string{}
	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err != nil {
			return nil, fmt.Errorf("sourceimport/scan arcade dependency closure: %w", err)
		}
		result = append(result, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sourceimport/iterate arcade dependency closure: %w", err)
	}
	return result, nil
}

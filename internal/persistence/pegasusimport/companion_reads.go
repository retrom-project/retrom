package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/pegasusimport"
)

func (records companionRecords) Candidates(
	ctx context.Context,
	item application.ExecutionItem,
) ([]application.CompanionCandidate, error) {
	rows, err := records.tx.QueryContext(ctx, `
SELECT candidate.id,file.ordinal,file.relative_path,file.size_bytes,file.source_facts_digest
FROM pegasus_import_items candidate
JOIN pegasus_import_collections collection ON collection.id=candidate.collection_id
JOIN pegasus_import_item_files file ON file.item_id=candidate.id
WHERE candidate.import_id=? AND candidate.id<>? AND candidate.discovery_state='READY'
AND collection.mapping_action='IMPORT' AND collection.target_platform_instance_id=?
AND collection.target_dat_version_id=?
AND (SELECT count(*) FROM pegasus_import_item_files own WHERE own.item_id=candidate.id)=1
ORDER BY file.relative_path`, item.ImportID, item.ID, item.TargetPlatformID, item.TargetDATVersionID)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/query arcade companions: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []application.CompanionCandidate{}
	for rows.Next() {
		var candidate application.CompanionCandidate
		file := &candidate.File
		if err := rows.Scan(&candidate.ItemID, &file.Ordinal, &file.Path, &file.Size, &file.Facts); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan arcade companion: %w", err)
		}

		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate arcade companions: %w", err)
	}
	return result, nil
}

func (records companionRecords) Dependencies(
	ctx context.Context,
	datVersionID, machine string,
) ([]string, error) {
	rows, err := records.tx.QueryContext(ctx, `
WITH RECURSIVE dependency(machine) AS (
 SELECT cloneof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND cloneof IS NOT NULL
 UNION
 SELECT romof FROM dat_machines
 WHERE dat_version_id=? AND machine_name=? AND romof IS NOT NULL
 UNION
 SELECT relation.cloneof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.cloneof IS NOT NULL
 UNION
 SELECT relation.romof FROM dat_machines relation
 JOIN dependency current ON relation.machine_name=current.machine
 WHERE relation.dat_version_id=? AND relation.romof IS NOT NULL
)
SELECT machine FROM dependency WHERE machine<>? ORDER BY machine`,
		datVersionID, machine, datVersionID, machine, datVersionID, datVersionID, machine,
	)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/query arcade dependency closure: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []string{}
	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err != nil {
			return nil, fmt.Errorf("pegasusimport/scan arcade dependency closure: %w", err)
		}
		result = append(result, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pegasusimport/iterate arcade dependency closure: %w", err)
	}
	return result, nil
}

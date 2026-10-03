package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ContentDuplicates struct{ executor dbapi.Executor }

func BindContentDuplicates(executor dbapi.Executor) *ContentDuplicates {
	return &ContentDuplicates{executor: executor}
}

func (records *ContentDuplicates) Snapshot(
	ctx context.Context,
	itemID string,
) (libraryservice.ContentSnapshot, error) {
	var result libraryservice.ContentSnapshot
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT snapshot.id,snapshot.content_kind FROM import_item_source_snapshots snapshot
WHERE snapshot.id=COALESCE(
 (SELECT draft.effective_source_snapshot_id FROM import_items draft WHERE draft.id=?),
 (SELECT initial.id FROM import_item_source_snapshots initial
 WHERE initial.import_item_id=? AND initial.created_by='IDENTIFICATION')
)`, itemID, itemID).Scan(&result.ID, &result.Kind)
	if err != nil {
		return libraryservice.ContentSnapshot{}, fmt.Errorf("read duplicate snapshot: %w", err)
	}
	return result, nil
}

func (records *ContentDuplicates) ReviewPlatform(ctx context.Context, itemID string) (string, error) {
	var result string
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT instance.platform_id FROM import_items item
JOIN import_items draft ON draft.id=item.id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
WHERE item.id=? AND item.state='REVIEW_PENDING'`, itemID).Scan(&result)
	if err != nil {
		return "", fmt.Errorf("read review duplicate platform: %w", err)
	}
	return result, nil
}

func (records *ContentDuplicates) IdentityParts(
	ctx context.Context,
	snapshotID string,
) ([]libraryservice.ContentIdentityPart, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT source.role,json_extract(blob.value, '$.sha256'),count(*) FROM import_item_source_snapshot_files source
JOIN json_each(json_array(source.file_record)) blob ON blob.value IS NOT NULL WHERE
source.source_snapshot_id=?
GROUP BY source.role,json_extract(blob.value, '$.sha256') ORDER BY source.role,json_extract(blob.value,
'$.sha256')`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query content identity parts: %w", err)
	}
	defer func() { cleanup.Error("close identity parts", rows.Close()) }()
	result := make([]libraryservice.ContentIdentityPart, 0)
	for rows.Next() {
		var part libraryservice.ContentIdentityPart
		if err := rows.Scan(&part.Role, &part.SHA256, &part.Count); err != nil {
			return nil, fmt.Errorf("scan content identity part: %w", err)
		}
		result = append(result, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate content identity parts: %w", err)
	}
	return result, nil
}

func (records *ContentDuplicates) OrderedDiscs(
	ctx context.Context,
	snapshotID string,
) ([]libraryservice.ContentIdentityDisc, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT entry.state,COALESCE(json_extract(blob.value, '$.sha256'),'') FROM import_item_multidisc_entries entry
LEFT JOIN json_each(json_array(entry.file_record)) blob ON blob.value IS NOT NULL WHERE
entry.source_snapshot_id=?
ORDER BY entry.ordinal`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query ordered content identity: %w", err)
	}
	defer func() { cleanup.Error("close ordered identity", rows.Close()) }()
	result, err := collectRows(
		rows,
		scanContentIdentityDisc,
		"scan ordered content identity",
		"iterate ordered content identity",
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func scanContentIdentityDisc(rows dbapi.Rows) (libraryservice.ContentIdentityDisc, error) {
	var disc libraryservice.ContentIdentityDisc
	if err := rows.Scan(&disc.State, &disc.SHA256); err != nil {
		return libraryservice.ContentIdentityDisc{}, fmt.Errorf("scan content identity row: %w", err)
	}
	return disc, nil
}

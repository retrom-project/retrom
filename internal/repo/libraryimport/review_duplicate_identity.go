package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/dbexec"
)

type ContentDuplicates struct{ executor dbexec.Executor }

func BindContentDuplicates(executor dbexec.Executor) *ContentDuplicates {
	return &ContentDuplicates{executor: executor}
}

func (records *ContentDuplicates) Snapshot(ctx context.Context, itemID string) (application.ContentSnapshot, error) {
	var result application.ContentSnapshot
	err := records.executor.QueryRowContext(ctx, `
SELECT snapshot.id,snapshot.content_kind FROM import_item_source_snapshots snapshot
WHERE snapshot.id=COALESCE(
 (SELECT draft.effective_source_snapshot_id FROM review_drafts draft WHERE draft.import_item_id=?),
 (SELECT initial.id FROM import_item_source_snapshots initial
 WHERE initial.import_item_id=? AND initial.created_by='IDENTIFICATION')
)`, itemID, itemID).Scan(&result.ID, &result.Kind)
	if err != nil {
		return application.ContentSnapshot{}, fmt.Errorf("read duplicate snapshot: %w", err)
	}
	return result, nil
}

func (records *ContentDuplicates) ReviewPlatform(ctx context.Context, itemID string) (string, error) {
	var result string
	err := records.executor.QueryRowContext(ctx, `
SELECT instance.platform_id FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
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
) ([]application.ContentIdentityPart, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT source.role,blob.sha256,count(*) FROM import_item_source_snapshot_files source
JOIN blobs blob ON blob.id=source.blob_id WHERE source.source_snapshot_id=?
GROUP BY source.role,blob.sha256 ORDER BY source.role,blob.sha256`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query content identity parts: %w", err)
	}
	defer func() { cleanup.Error("close identity parts", rows.Close()) }()
	result := make([]application.ContentIdentityPart, 0)
	for rows.Next() {
		var part application.ContentIdentityPart
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
) ([]application.ContentIdentityDisc, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT entry.state,COALESCE(blob.sha256,'') FROM import_item_multidisc_entries entry
LEFT JOIN blobs blob ON blob.id=entry.blob_id WHERE entry.source_snapshot_id=? ORDER BY entry.ordinal`, snapshotID)
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

func scanContentIdentityDisc(rows *sql.Rows) (application.ContentIdentityDisc, error) {
	var disc application.ContentIdentityDisc
	if err := rows.Scan(&disc.State, &disc.SHA256); err != nil {
		return application.ContentIdentityDisc{}, fmt.Errorf("scan content identity row: %w", err)
	}
	return disc, nil
}

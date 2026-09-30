package libraryimport

import (
	"context"
	"fmt"

	arcaderecords "retrom/internal/persistence/arcade"

	"retrom/internal/persistence/contentquery"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type ReviewDependencies struct {
	*arcaderecords.Catalog
	executor dbapi.Executor
}

func BindReviewDependencies(executor dbapi.Executor) *ReviewDependencies {
	return &ReviewDependencies{Catalog: arcaderecords.New(executor), executor: executor}
}

func (records *ReviewDependencies) Head(
	ctx context.Context,
	itemID string,
) (libraryservice.ReviewDependencyHead, error) {
	var result libraryservice.ReviewDependencyHead
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT draft.effective_source_snapshot_id,snapshot.content_kind,platform.platform_id,
validation.status,validation.compatibility_code,validation.dependency_snapshot_json
FROM import_items item JOIN import_items draft ON draft.id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
LEFT JOIN (`+contentquery.CurrentContentSQL+`) validation ON validation.import_item_id=item.id
WHERE item.id=? AND item.state='REVIEW_PENDING'`,
		itemID).Scan(&result.SnapshotID,
		&result.ContentKind,
		&result.PlatformID,
		&result.ValidationStatus,
		&result.CompatibilityCode,
		&result.DependencyJSON)
	if err != nil {
		return libraryservice.ReviewDependencyHead{}, fmt.Errorf("read review dependency head: %w", err)
	}
	runtime, err := ReadReviewRuntime(ctx, records.executor, itemID)
	if err != nil {
		return libraryservice.ReviewDependencyHead{}, err
	}
	result.ValidationStatus, result.CompatibilityCode = &runtime.Status, &runtime.Code
	result.DependencyJSON = &runtime.DependencyJSON
	return result, nil
}

func (records *ReviewDependencies) ArcadeAttachments(
	ctx context.Context,
	itemID string,
) ([]libraryservice.ArcadeAttachment, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT attachment.id,dependency_machine,expected_logical_name,original_filename,
CASE WHEN attachment.state<>'PENDING' THEN attachment.state
WHEN job.state='FAILED' AND job.error_retryable=1 THEN 'FAILED_RETRYABLE'
WHEN job.state='CANCEL_REQUESTED' THEN 'RUNNING' ELSE job.state END,
COALESCE(attachment.error_code,job.error_code),job_id,observed_size_bytes,observed_sha256,diagnostics_json,
attachment.created_at_ms,MAX(attachment.updated_at_ms,job.updated_at_ms),
COALESCE(attachment.finished_at_ms,job.finished_at_ms)
FROM review_arcade_parent_attachments attachment JOIN jobs job ON job.id=attachment.job_id
WHERE import_item_id=? ORDER BY attachment.created_at_ms DESC,attachment.id DESC`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review arcade attachments: %w", err)
	}
	defer func() { cleanup.Error("close arcade attachments", rows.Close()) }()
	result := make([]libraryservice.ArcadeAttachment, 0)
	for rows.Next() {
		var row libraryservice.ArcadeAttachment
		var diagnostics []byte
		if err := rows.Scan(&row.ID,
			&row.Machine,
			&row.ExpectedLogicalName,
			&row.OriginalFilename,
			&row.State,
			&row.ErrorCode,
			&row.JobID,
			&row.ObservedSizeBytes,
			&row.ObservedSHA256,
			&diagnostics,
			&row.CreatedAtMS,
			&row.UpdatedAtMS,
			&row.FinishedAtMS); err != nil {
			return nil, fmt.Errorf("scan arcade attachment: %w", err)
		}
		row.Diagnostics = diagnostics
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate arcade attachments: %w", err)
	}
	return result, nil
}

package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/persistence/dbexec"
	application "retrom/internal/service/libraryimport"
)

type ReviewDependencies struct {
	*ArcadeRelations
	executor dbexec.Executor
}

func BindReviewDependencies(executor dbexec.Executor) *ReviewDependencies {
	return &ReviewDependencies{ArcadeRelations: BindArcadeRelations(executor), executor: executor}
}

func (records *ReviewDependencies) Head(
	ctx context.Context,
	itemID string,
) (application.ReviewDependencyHead, error) {
	var result application.ReviewDependencyHead
	err := records.executor.QueryRowContext(ctx, `
SELECT draft.effective_source_snapshot_id,snapshot.content_kind,platform.platform_id,
validation.status,validation.compatibility_code,validation.dependency_snapshot_json
FROM import_items item JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
LEFT JOIN import_item_core_validations validation ON validation.id=COALESCE(draft.selected_validation_id,
 (SELECT candidate.id FROM import_item_core_validations candidate WHERE candidate.import_item_id=item.id
 AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
 AND candidate.target_platform_instance_id=draft.target_platform_instance_id
 ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1))
WHERE item.id=? AND item.state='REVIEW_PENDING'`,
		itemID).Scan(&result.SnapshotID,
		&result.ContentKind,
		&result.PlatformID,
		&result.ValidationStatus,
		&result.CompatibilityCode,
		&result.DependencyJSON)
	if err != nil {
		return application.ReviewDependencyHead{}, fmt.Errorf("read review dependency head: %w", err)
	}
	return result, nil
}

func (records *ReviewDependencies) ArcadeAttachments(
	ctx context.Context,
	itemID string,
) ([]application.ArcadeAttachment, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT id,dependency_machine,expected_logical_name,original_filename,state,error_code,
job_id,observed_size_bytes,observed_sha256,diagnostics_json,created_at_ms,updated_at_ms,finished_at_ms
FROM review_arcade_parent_attachments WHERE import_item_id=? ORDER BY created_at_ms DESC,id DESC`, itemID)
	if err != nil {
		return nil, fmt.Errorf("query review arcade attachments: %w", err)
	}
	defer func() { cleanup.Error("close arcade attachments", rows.Close()) }()
	result := make([]application.ArcadeAttachment, 0)
	for rows.Next() {
		var row application.ArcadeAttachment
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

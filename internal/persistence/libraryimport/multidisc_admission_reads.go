package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/persistence/contentquery"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records multidiscAdmissionRecords) Admission(
	ctx context.Context, itemID string,
) (libraryservice.MultiDiscAttachmentAdmission, bool, error) {
	var admission libraryservice.MultiDiscAttachmentAdmission
	err := dbapi.QueryRowContext(ctx, records.executor, `SELECT draft.id,item.state,draft.review_version,
draft.effective_source_snapshot_id,
platform.platform_id,platform.id,platform.version,platform.default_core_id,
target.provider_id,target.target_id,
`+contentquery.BindingPolicySQL+`,
validation.status,validation.compatibility_code
FROM import_items item
JOIN import_items draft ON draft.id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
AND snapshot.content_kind='MULTI_DISC'
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN runtime_target_bindings binding ON binding.core_id=platform.default_core_id
  AND binding.launch_policy<>'DISABLED'
JOIN runtime_binding_platforms platform_binding ON platform_binding.binding_id=binding.binding_id
  AND platform_binding.platform_id=platform.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id
  AND target.target_id=binding.target_id
JOIN (`+contentquery.CurrentContentSQL+`) validation ON validation.import_item_id=item.id
AND validation.source_snapshot_id=snapshot.id
AND validation.target_platform_instance_id=platform.id
WHERE item.id=?
`, itemID).Scan(
		&admission.DraftID, &admission.ItemState, &admission.DraftVersion,
		&admission.SnapshotID, &admission.PlatformID, &admission.PlatformInstanceID,
		&admission.PlatformVersion, &admission.CoreID, &admission.ProviderID, &admission.TargetID,
		contentquery.ScanPolicy(&admission.Policy),
		&admission.ValidationStatus, &admission.CompatibilityCode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return admission, false, nil
	}
	if err != nil {
		return admission, false, fmt.Errorf("read multi-disc admission: %w", err)
	}
	runtime, err := ReadReviewRuntime(ctx, records.executor, itemID)
	if err != nil {
		return admission, false, err
	}
	admission.ValidationStatus, admission.CompatibilityCode = runtime.Status, runtime.Code
	return admission, true, nil
}

func (records multidiscAdmissionRecords) Head(
	ctx context.Context, itemID string,
) (libraryservice.MultiDiscAttachmentHead, bool, error) {
	var head libraryservice.MultiDiscAttachmentHead
	err := dbapi.QueryRowContext(ctx, records.executor, `SELECT item.state,snapshot.content_kind
FROM import_items item JOIN import_items draft ON draft.id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
WHERE item.id=?`, itemID).Scan(&head.State, &head.ContentKind)
	if errors.Is(err, sql.ErrNoRows) {
		return head, false, nil
	}
	if err != nil {
		return head, false, fmt.Errorf("read multi-disc item headline: %w", err)
	}
	return head, true, nil
}

func (records multidiscAdmissionRecords) Upload(
	ctx context.Context, id string,
) (libraryservice.MultiDiscAttachmentUpload, bool, error) {
	var upload libraryservice.MultiDiscAttachmentUpload
	err := dbapi.QueryRowContext(ctx, records.executor, `SELECT session.state,session.source_type,EXISTS(
SELECT 1 FROM upload_consumptions consumption WHERE consumption.upload_session_id=session.id
AND consumption.upload_file_id IS NULL) FROM upload_sessions session WHERE session.id=?`, id).
		Scan(&upload.State, &upload.SourceType, &upload.Consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return upload, false, nil
	}
	if err != nil {
		return upload, false, fmt.Errorf("read multi-disc upload: %w", err)
	}
	return upload, true, nil
}

func (records multidiscAdmissionRecords) Activity(
	ctx context.Context, id string,
) (libraryservice.MultiDiscAttachmentActivity, error) {
	var result libraryservice.MultiDiscAttachmentActivity
	err := dbapi.QueryRowContext(ctx, records.executor, `SELECT
COALESCE(sum(CASE WHEN attachment.state='PENDING'
AND job.state IN ('QUEUED','RUNNING','CANCEL_REQUESTED') THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN attachment.state='PENDING' AND job.state='FAILED' AND job.error_retryable=1 THEN 1 ELSE 0 END),0)
FROM review_multidisc_attachments attachment JOIN jobs job ON job.id=attachment.job_id
WHERE attachment.import_item_id=?`, id).Scan(&result.Active, &result.Retryable)
	if err != nil {
		return result, fmt.Errorf("read multi-disc attachment activity: %w", err)
	}
	return result, nil
}

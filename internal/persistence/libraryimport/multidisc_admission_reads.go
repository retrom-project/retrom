package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/contentquery"
	application "retrom/internal/service/libraryimport"
)

func (records multidiscAdmissionRecords) Admission(
	ctx context.Context, itemID string,
) (application.MultiDiscAttachmentAdmission, bool, error) {
	var admission application.MultiDiscAttachmentAdmission
	err := records.executor.QueryRowContext(ctx, `SELECT draft.id,item.state,draft.version,
draft.effective_source_snapshot_id,
platform.platform_id,platform.id,platform.version,platform.default_core_id,
target.provider_id,target.target_id,
`+contentquery.BindingPolicySQL+`,
validation.id,validation.status,validation.compatibility_code,
validation.platform_instance_version,validation.core_id,validation.provider_id,validation.target_id
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
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
JOIN import_item_core_validations validation ON validation.import_item_id=item.id
AND validation.source_snapshot_id=snapshot.id
AND validation.target_platform_instance_id=platform.id
WHERE item.id=?
ORDER BY validation.created_at_ms DESC,validation.id DESC LIMIT 1
`, itemID).Scan(
		&admission.DraftID, &admission.ItemState, &admission.DraftVersion,
		&admission.SnapshotID, &admission.PlatformID, &admission.PlatformInstanceID,
		&admission.PlatformVersion, &admission.CoreID, &admission.ProviderID, &admission.TargetID,
		contentquery.ScanPolicy(&admission.Policy),
		&admission.ValidationID,
		&admission.ValidationStatus, &admission.CompatibilityCode,
		&admission.ValidationPlatformVersion, &admission.ValidationCoreID,
		&admission.ValidationProviderID, &admission.ValidationTargetID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return admission, false, nil
	}
	if err != nil {
		return admission, false, fmt.Errorf("read multi-disc admission: %w", err)
	}
	return admission, true, nil
}

func (records multidiscAdmissionRecords) Head(
	ctx context.Context, itemID string,
) (application.MultiDiscAttachmentHead, bool, error) {
	var head application.MultiDiscAttachmentHead
	err := records.executor.QueryRowContext(ctx, `SELECT item.state,snapshot.content_kind
FROM import_items item JOIN review_drafts draft ON draft.import_item_id=item.id
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
) (application.MultiDiscAttachmentUpload, bool, error) {
	var upload application.MultiDiscAttachmentUpload
	err := records.executor.QueryRowContext(ctx, `SELECT session.state,session.source_type,EXISTS(
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
) (application.MultiDiscAttachmentActivity, error) {
	var result application.MultiDiscAttachmentActivity
	err := records.executor.QueryRowContext(ctx, `SELECT
COALESCE(sum(CASE WHEN state IN ('QUEUED','RUNNING') THEN 1 ELSE 0 END),0),
COALESCE(sum(CASE WHEN state='FAILED_RETRYABLE' THEN 1 ELSE 0 END),0)
FROM review_multidisc_attachments WHERE import_item_id=?`, id).Scan(&result.Active, &result.Retryable)
	if err != nil {
		return result, fmt.Errorf("read multi-disc attachment activity: %w", err)
	}
	return result, nil
}

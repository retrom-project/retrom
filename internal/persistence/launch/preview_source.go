package launch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/dbexec"
	application "retrom/internal/service/launch"
)

const previewCreationSourceSQL = `
SELECT draft.effective_source_snapshot_id,draft.target_platform_instance_id,instance.name,platform.id,
	validation.provider_id,validation.target_id,provider.bundle_sha256,validation.core_id,binding.delivery_profile,
COALESCE(json_extract(draft.metadata_json,'$.title'),''),snapshot.content_kind,
validation.id,validation.status,validation.dependency_snapshot_json,draft.default_dos_entry,
draft.selected_validation_id,validation.dat_version_id
FROM import_items item
JOIN import_items draft ON draft.id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
 AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
JOIN platforms platform ON platform.id=instance.platform_id
JOIN import_item_core_validations validation ON validation.id=(
 SELECT candidate.id FROM import_item_core_validations candidate
 WHERE candidate.import_item_id=item.id
 AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
 AND candidate.target_platform_instance_id=draft.target_platform_instance_id
 AND candidate.core_id=instance.default_core_id
 ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
JOIN runtime_targets target ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
WHERE item.id=? AND item.state='REVIEW_PENDING' AND item.payload_state='RETAINED'
`

func previewCreationSource(
	ctx context.Context,
	executor dbexec.Executor,
	itemID string,
) (application.PreviewSource, bool, error) {
	var value application.PreviewSource
	err := executor.QueryRowContext(ctx, previewCreationSourceSQL, itemID).Scan(
		&value.SourceSnapshotID, &value.PlatformInstanceID, &value.PlatformName, &value.PlatformKey,
		&value.ProviderID, &value.TargetID, &value.BundleSHA256, &value.CoreID, &value.DeliveryProfile,
		&value.Title, &value.ContentKind, &value.ValidationID, &value.ValidationStatus, &value.DependencySnapshot,
		&value.DefaultDOSEntry, &value.SelectedValidationID, &value.DATVersionID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PreviewSource{}, false, nil
	}
	if err != nil {
		return application.PreviewSource{}, false, fmt.Errorf("query preview source: %w", err)
	}
	return value, true, nil
}

func (records previewCreationRecords) Current(
	ctx context.Context,
	request application.ReviewPreviewRequest,
) (application.PreviewSource, string, bool, error) {
	source, found, err := previewCreationSource(ctx, records.executor, request.ImportItemID)
	if err != nil || !found {
		return application.PreviewSource{}, "", found, err
	}
	var profile string
	err = records.executor.QueryRowContext(ctx, `SELECT profile_id FROM users WHERE id=?`, request.ActorUserID).
		Scan(&profile)
	if errors.Is(err, sql.ErrNoRows) {
		return source, "", true, nil
	}
	if err != nil {
		return application.PreviewSource{}, "", false, fmt.Errorf("query preview actor: %w", err)
	}
	return source, profile, true, nil
}

package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/persistence/contentquery"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/launch"
)

const previewCreationSourceSQL = `
SELECT draft.effective_source_snapshot_id,draft.target_platform_instance_id,instance.name,platform.id,
	validation.provider_id,validation.target_id,provider.bundle_sha256,validation.core_id,binding.delivery_profile,
COALESCE(json_extract(draft.metadata_json,'$.title'),''),snapshot.content_kind,
validation.status,validation.dependency_snapshot_json,draft.default_dos_entry,
validation.dat_version_id
FROM import_items item
JOIN import_items draft ON draft.id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
 AND instance.enabled=1 AND instance.deleted_at_ms IS NULL
JOIN platforms platform ON platform.id=instance.platform_id
JOIN (` + contentquery.CurrentContentSQL + `) validation ON validation.import_item_id=item.id
JOIN runtime_targets target ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
JOIN runtime_providers provider ON provider.provider_id=target.provider_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
WHERE item.id=? AND item.state='REVIEW_PENDING' AND item.payload_state='RETAINED'
`

func previewCreationSource(
	ctx context.Context,
	executor dbapi.Executor,
	itemID string,
) (application.PreviewSource, bool, error) {
	var value application.PreviewSource
	err := dbapi.QueryRowContext(ctx, executor, previewCreationSourceSQL, itemID).Scan(
		&value.SourceSnapshotID, &value.PlatformInstanceID, &value.PlatformName, &value.PlatformKey,
		&value.ProviderID, &value.TargetID, &value.BundleSHA256, &value.CoreID, &value.DeliveryProfile,
		&value.Title, &value.ContentKind, &value.ValidationStatus, &value.DependencySnapshot,
		&value.DefaultDOSEntry, &value.DATVersionID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.PreviewSource{}, false, nil
	}
	if err != nil {
		return application.PreviewSource{}, false, fmt.Errorf("query preview source: %w", err)
	}
	runtime, err := ReadReviewRuntime(ctx, executor, itemID)
	if err != nil {
		return application.PreviewSource{}, false, fmt.Errorf("read preview facts: %w", err)
	}
	value.ValidationStatus, value.DependencySnapshot = runtime.Status, runtime.DependencyJSON
	return value, true, nil
}

func (records reviewPreviewRecords) Current(
	ctx context.Context,
	request application.ReviewPreviewRequest,
) (application.PreviewSnapshot, string, bool, error) {
	source, found, err := readReviewPreviewInput(ctx, records.executor, request.ImportItemID)
	if err != nil || !found {
		return application.PreviewSnapshot{}, "", found, err
	}
	var profile string
	err = dbapi.QueryRowContext(ctx, records.executor, `SELECT profile_id FROM users WHERE id=?`, request.ActorUserID).
		Scan(&profile)
	if errors.Is(err, sql.ErrNoRows) {
		return source, "", true, nil
	}
	if err != nil {
		return application.PreviewSnapshot{}, "", false, fmt.Errorf("query preview actor: %w", err)
	}
	return source, profile, true, nil
}

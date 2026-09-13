package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/repo/contentquery"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/libraryimport"
)

type ReviewValidation struct{ executor dbexec.Executor }

func BindReviewValidation(executor dbexec.Executor) *ReviewValidation {
	return &ReviewValidation{executor: executor}
}

func (records *ReviewValidation) Evidence(
	ctx context.Context,
	validationID string,
) (application.ReviewValidationEvidence, error) {
	var value application.ReviewValidationEvidence
	err := records.executor.QueryRowContext(ctx, `
SELECT validation.platform_instance_version,
validation.source_snapshot_id,
validation.target_platform_instance_id,validation.core_id,validation.provider_id,validation.target_id,
validation.source_manifest_digest,validation.prepublish_input_digest,validation.Status,
validation.compatibility_code,validation.dependency_snapshot_json,validation.dat_version_id,
validation.default_dos_entry,draft.default_dos_entry,
(SELECT active.id FROM dat_versions active
 WHERE active.provider_id=validation.provider_id AND active.target_id=validation.target_id AND active.is_active=1),
draft.effective_source_snapshot_id,draft.target_platform_instance_id,
snapshot.source_manifest_digest,snapshot.content_kind,platform.default_core_id,platform.version,
`+contentquery.BindingPolicySQL+`,draft.id
FROM import_item_core_validations validation
JOIN import_items item ON item.id=validation.import_item_id AND item.state='REVIEW_PENDING'
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
LEFT JOIN rpgmaker_review_profiles rpg_profile ON rpg_profile.review_draft_id=draft.id
JOIN runtime_targets target ON target.provider_id=validation.provider_id AND target.target_id=validation.target_id
JOIN runtime_target_bindings binding ON binding.provider_id=target.provider_id AND binding.target_id=target.target_id
 AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=platform.platform_id
WHERE validation.id=?
`, validationID).Scan(
		&value.PlatformVersion,
		&value.SourceSnapshotID, &value.PlatformInstanceID, &value.CoreID,
		&value.ProviderID, &value.TargetID,
		&value.ManifestDigest, &value.InputDigest, &value.Status, &value.CompatibilityCode,
		&value.DependencyJSON, &value.ValidationDAT, &value.ValidationDOS, &value.DraftDOS,
		&value.ActiveDAT, &value.DraftSnapshotID, &value.DraftPlatformInstanceID,
		&value.SnapshotManifestDigest, &value.ContentKind, &value.CurrentCoreID,
		&value.CurrentPlatformVersion, contentquery.ScanPolicy(&value.ContentPolicy), &value.DraftID,
	)
	if err != nil {
		return application.ReviewValidationEvidence{}, fmt.Errorf("libraryimport/review validation evidence: %w", err)
	}
	return value, nil
}

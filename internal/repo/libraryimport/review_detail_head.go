package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/repo/contentquery"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/libraryimport"
)

const reviewDetailQuery = `
SELECT i.id,
i.import_job_id,
d.metadata_json,
d.version,
d.updated_at_ms,
pi.id,
pi.name,
` + contentquery.BindingPolicySQL + `,
v.id,
v.status,
v.compatibility_code,
v.dependency_snapshot_json,
d.selected_validation_id,
source_snapshot.id,
source_snapshot.source_manifest_json,
source_snapshot.content_kind,
d.selected_candidate_id,
d.cover_candidate_asset_id,
d.cover_uploaded_asset_id,
d.background_candidate_asset_id,
d.default_dos_entry,d.id,pi.platform_id
FROM import_items i
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=d.effective_source_snapshot_id
JOIN platform_instances pi ON pi.id=d.target_platform_instance_id
LEFT JOIN rpgmaker_review_profiles rpg_profile ON rpg_profile.review_draft_id=d.id
JOIN runtime_targets current_target ON current_target.provider_id=COALESCE(rpg_profile.provider_id,(
 SELECT binding.provider_id FROM runtime_target_bindings binding
 JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
  AND binding_platform.platform_id=pi.platform_id AND binding_platform.core_id=pi.default_core_id
 JOIN runtime_binding_content_kinds binding_kind ON binding_kind.binding_id=binding.binding_id
  AND binding_kind.content_kind=source_snapshot.content_kind
 WHERE binding.core_id=pi.default_core_id AND binding.launch_policy<>'DISABLED' LIMIT 1
)) AND current_target.target_id=COALESCE(rpg_profile.target_id,(
 SELECT binding.target_id FROM runtime_target_bindings binding
 JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
  AND binding_platform.platform_id=pi.platform_id AND binding_platform.core_id=pi.default_core_id
 JOIN runtime_binding_content_kinds binding_kind ON binding_kind.binding_id=binding.binding_id
  AND binding_kind.content_kind=source_snapshot.content_kind
 WHERE binding.core_id=pi.default_core_id AND binding.launch_policy<>'DISABLED' LIMIT 1
))
JOIN runtime_target_bindings binding
 ON binding.provider_id=current_target.provider_id AND binding.target_id=current_target.target_id
 AND binding.launch_policy<>'DISABLED'
LEFT JOIN import_item_core_validations v ON v.id=COALESCE(d.selected_validation_id,(
  SELECT candidate.id
FROM import_item_core_validations candidate
WHERE candidate.import_item_id=i.id
AND candidate.source_snapshot_id=d.effective_source_snapshot_id
AND candidate.target_platform_instance_id=d.target_platform_instance_id
ORDER BY candidate.created_at_ms DESC,
candidate.id DESC LIMIT 1))
WHERE i.id=?
AND i.state='REVIEW_PENDING'
AND (i.review_handoff_kind='DIRECT' OR EXISTS(
  SELECT 1 FROM emulationstation_import_items reserved_source
  WHERE reserved_source.library_import_item_id=i.id
  AND reserved_source.execution_state='REVIEW_PENDING'
))
AND NOT EXISTS(
  SELECT 1 FROM pegasus_import_items pegasus
  WHERE pegasus.library_import_item_id=i.id AND pegasus.execution_state<>'REVIEW_PENDING'
)
AND NOT EXISTS(
  SELECT 1 FROM emulationstation_import_items emulationstation
  WHERE emulationstation.library_import_item_id=i.id
  AND emulationstation.execution_state<>'REVIEW_PENDING'
)
`

type ReviewDrafts struct{ executor dbexec.Executor }

func (records ReviewDrafts) Head(ctx context.Context, itemID string) (application.ReviewHead, error) {
	var result application.ReviewHead
	err := records.executor.QueryRowContext(ctx, reviewDetailQuery, itemID).Scan(
		&result.ItemID, &result.ImportJobID, &result.MetadataJSON, &result.Version, &result.UpdatedAtMS,
		&result.PlatformInstance.ID, &result.PlatformInstance.Name, contentquery.ScanPolicy(&result.Policy),
		&result.ValidationID, &result.ValidationStatus, &result.CompatibilityCode, &result.DependencyJSON,
		&result.SelectedValidationID, &result.SnapshotID, &result.SourceManifestJSON, &result.ContentKind,
		&result.SelectedCandidateID,
		&result.CoverID,
		&result.UploadedCoverID,
		&result.BackgroundID,
		&result.DefaultDOSEntry,

		&result.DraftID, &result.PlatformID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewHead{}, application.ErrReviewNotFound
	}
	if err != nil {
		return application.ReviewHead{}, fmt.Errorf("query review headline: %w", err)
	}
	return result, nil
}

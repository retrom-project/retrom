package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/contentquery"
	"retrom/internal/repo/storequery"
)

func (records reviewApprovalRecords) Head(
	ctx context.Context, itemID string,
) (application.ReviewApprovalHead, bool, error) {
	var h application.ReviewApprovalHead
	err := records.transaction.QueryRowContext(ctx, approvalHeadQuery, itemID).Scan(
		&h.DraftID, &h.State, &h.ImportID, &h.PlatformID, &h.PlatformInstanceID, &h.ValidationID,
		&h.ValidationStatus, &h.MetadataJSON,
		&h.SourceSnapshotID, &h.SourceManifestJSON, &h.SourceManifestDigest, &h.ContentKind, &h.CoreID,
		&h.ProviderID, &h.TargetID,
		contentquery.ScanPolicy(&h.Policy), &h.DATID, &h.ValidationDOS, &h.DraftDOS, &h.DependencyJSON,
		&h.ScreenshotID,
		&h.DraftVersion, &h.CandidateID, &h.CoverID, &h.UploadedCoverID, &h.BackgroundID,
		&h.ParentVersion, &h.Progress.State, &h.Progress.Counts.Queued, &h.Progress.Counts.Running,
		&h.Progress.Counts.ReviewPending,
		&h.Progress.Counts.Failed, &h.Progress.Counts.Cancelled, &h.Progress.Counts.Rejected,
		&h.Progress.Counts.ResolvedRejected,
		&h.Progress.CancelRequestedAtMS, &h.Progress.CompletedAtMS, &h.SourceBusy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ReviewApprovalHead{}, false, nil
	}
	if err != nil {
		return application.ReviewApprovalHead{}, false, fmt.Errorf("query approval headline: %w", err)
	}
	return h, true, nil
}

const approvalHeadQuery = `
SELECT d.id,i.state,i.import_job_id,p.platform_id,
  d.target_platform_instance_id,v.id,v.status,d.metadata_json,source_snapshot.id,
  source_snapshot.source_manifest_json,source_snapshot.source_manifest_digest,
  source_snapshot.content_kind,v.core_id,v.provider_id,v.target_id,
  ` + contentquery.BindingPolicySQL + `,
  v.dat_version_id,v.default_dos_entry,d.default_dos_entry,
  v.dependency_snapshot_json,
  (SELECT screenshot.id FROM review_runtime_screenshots screenshot
   WHERE screenshot.import_item_id=i.id AND screenshot.validation_id=v.id
     AND screenshot.source_snapshot_id=d.effective_source_snapshot_id
     AND screenshot.provider_id=v.provider_id AND screenshot.target_id=v.target_id
   ORDER BY screenshot.captured_at_ms DESC,screenshot.id DESC LIMIT 1),
  d.version,d.selected_candidate_id,d.cover_candidate_asset_id,d.cover_uploaded_asset_id,
  d.background_candidate_asset_id,
  j.version,j.state,j.queued_item_count,j.running_item_count,j.review_pending_item_count,
  j.failed_item_count,j.cancelled_item_count,j.rejected_file_count,j.resolved_rejected_file_count,
  j.cancel_requested_at_ms,j.completed_at_ms,
  (EXISTS(SELECT 1 FROM pegasus_import_items owner
    WHERE owner.library_import_item_id=i.id AND owner.execution_state<>'REVIEW_PENDING') OR
   EXISTS(SELECT 1 FROM emulationstation_import_items owner
    WHERE owner.library_import_item_id=i.id AND owner.execution_state<>'REVIEW_PENDING'))
FROM import_items i
JOIN import_jobs j ON j.id=i.import_job_id
JOIN review_drafts d ON d.import_item_id=i.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=d.effective_source_snapshot_id
JOIN platform_instances p ON p.id=d.target_platform_instance_id
  AND p.enabled=1 AND p.deleted_at_ms IS NULL
JOIN import_item_core_validations v ON v.id=(
  SELECT candidate.id FROM import_item_core_validations candidate
  WHERE candidate.import_item_id=i.id
    AND candidate.source_snapshot_id=d.effective_source_snapshot_id
    AND candidate.target_platform_instance_id=d.target_platform_instance_id
  ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1
)
AND v.source_snapshot_id=d.effective_source_snapshot_id
AND v.target_platform_instance_id=d.target_platform_instance_id
AND p.default_core_id=v.core_id
JOIN runtime_targets target ON target.provider_id=v.provider_id AND target.target_id=v.target_id
JOIN runtime_target_bindings binding ON binding.provider_id=v.provider_id AND binding.target_id=v.target_id
 AND binding.core_id=v.core_id AND binding.launch_policy!='DISABLED'
JOIN runtime_binding_platforms binding_platform ON binding_platform.binding_id=binding.binding_id
 AND binding_platform.platform_id=p.platform_id
WHERE i.id=?
AND NOT EXISTS(SELECT 1 FROM (` + storequery.DiscardedImportJobs + `) WHERE import_id=i.import_job_id)
AND (i.review_handoff_kind='DIRECT' OR EXISTS(
  SELECT 1 FROM emulationstation_import_items reserved_source
  WHERE reserved_source.library_import_item_id=i.id
  AND reserved_source.execution_state='REVIEW_PENDING'
))
AND (v.status='READY' OR EXISTS(
  SELECT 1 FROM review_runtime_screenshots screenshot
  WHERE screenshot.import_item_id=i.id AND screenshot.validation_id=v.id
    AND screenshot.source_snapshot_id=d.effective_source_snapshot_id
    AND screenshot.provider_id=v.provider_id AND screenshot.target_id=v.target_id
))
AND v.default_dos_entry IS d.default_dos_entry
AND v.dat_version_id IS (
  SELECT active.id FROM dat_versions active
  WHERE active.provider_id=v.provider_id AND active.target_id=v.target_id AND active.is_active=1
)
`

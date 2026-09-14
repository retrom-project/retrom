package libraryimport

import (
	"context"

	application "retrom/internal/model/libraryimport"
	"retrom/internal/repo/recordstore"
)

func (records creationRecords) Validation(ctx context.Context, change application.CreationValidation) error {
	target := change.Target
	result, err := recordstore.CreateImportItemCoreValidations(
		ctx,
		records.transaction,
		`
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
 platform_instance_version,core_id,
provider_id,target_id,dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,
 prepublish_input_digest,
status,compatibility_code,dependency_snapshot_json,created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,
 ?,?)`,
		change.ID,
		change.ItemID,
		target.ID,
		target.Version,
		target.CoreID,
		target.ProviderID,
		target.TargetID,
		creationNullable(change.DATID),
		creationNullable(change.DefaultDOS),
		change.ManifestDigest,
		change.SnapshotID,
		change.InputDigest,
		change.Status,
		change.Code,
		change.DependencyJSON,
		change.NowMS,
	)
	if err := creationMutation(result, err, "insert creation validation", 1); err != nil {
		return err
	}
	for _, entry := range change.DOSEntries {
		result, err = records.transaction.ExecContext(
			ctx,
			`
INSERT INTO import_item_dos_entries(import_item_id,normalized_path,original_relative_path,kind,rank,
 enabled,direct_launch_safe,created_at_ms)
VALUES(?,?,?,?,?,1,?,?)`,
			change.ItemID,
			entry.Path,
			entry.Path,
			entry.Kind,
			entry.Rank,
			entry.Safe,
			change.NowMS,
		)
		if err := creationMutation(result, err, "insert creation DOS entry", 1); err != nil {
			return err
		}
	}
	for _, file := range change.Files {
		result, err = records.transaction.ExecContext(
			ctx,
			`
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,
 sort_order,created_at_ms)
VALUES(?,?,?,?,?,?)`,
			change.ID,
			file.Role,
			file.LogicalName,
			file.BlobID,
			file.SortOrder,
			change.NowMS,
		)
		if err := creationMutation(result, err, "insert creation validation file", 1); err != nil {
			return err
		}
	}
	return nil
}

func (records creationRecords) Draft(ctx context.Context, change application.CreationDraft) error {
	result, err := recordstore.UpdateImportItems(
		ctx,
		records.transaction,
		recordstore.Update{
			Set:    `search_text=?`,
			Scope:  recordstore.Scope{Where: `id=? AND version=1`, Args: []any{change.ItemID}},
			Values: []any{change.SearchText},
		},
	)
	if err := creationMutation(result, err, "project creation search", 1); err != nil {
		return err
	}
	result, err = recordstore.CreateReviewDrafts(
		ctx,
		records.transaction,
		`
INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,selected_validation_id,
 effective_source_snapshot_id,
 default_dos_entry,metadata_json,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,1,?,?)`,
		change.ID,
		change.ItemID,
		change.TargetID,
		change.SelectedValidationID,
		change.SnapshotID,
		change.DefaultDOS,
		change.MetadataJSON,
		change.NowMS,
		change.NowMS,
	)
	return creationMutation(result, err, "insert creation draft", 1)
}

func (records creationRecords) RPG(ctx context.Context, change application.CreationRPGProfile) error {
	result, err := recordstore.CreateRpgmakerReviewProfiles(
		ctx,
		records.transaction,
		`
INSERT INTO rpgmaker_review_profiles(review_draft_id,generation,evidence_family,evidence_generation,
 evidence_confidence,
engine_version,entry_html_path,file_count,total_bytes,project_fingerprint,requirements_sha256,
 analysis_json,
 self_contained_override,provider_id,target_id,dependency_snapshot_sha256,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?,?)`,
		change.DraftID,
		change.Generation,
		change.EvidenceFamily,
		change.EvidenceGeneration,
		change.EvidenceConfidence,
		change.EngineVersion,
		change.EntryHTML,
		change.FileCount,
		change.TotalBytes,
		change.FilesDigest,
		change.RequirementsDigest,
		change.AnalysisJSON,
		change.ProviderID,
		change.TargetID,
		change.DependencyDigest,
		change.NowMS,
		change.NowMS,
	)
	return creationMutation(result, err, "insert creation RPG profile", 1)
}

func (records creationRecords) Events(ctx context.Context, events []application.CreationEvent) error {
	for _, event := range events {
		result, err := records.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES(?,?,?,?,?,?)`,
			event.JobID, event.ScopeType, event.ScopeID, event.Kind, event.DataJSON, event.NowMS)
		if err := creationMutation(result, err, "insert creation event", 1); err != nil {
			return err
		}
	}
	return nil
}

package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/profilemodel"
	application "retrom/internal/service/libraryimport"
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
	if change.ID != change.ItemID {
		return application.ErrInvalid
	}
	result, err := recordstore.UpdateReviewItems(ctx, records.transaction, recordstore.Update{
		Set: `search_text=?,target_platform_instance_id=?,selected_validation_id=?,
effective_source_snapshot_id=?,default_dos_entry=?,metadata_json=?,review_version=1,
review_created_at_ms=?,review_updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND review_version=0`, Args: []any{change.ItemID}},
		Values: []any{
			change.SearchText, change.TargetID, change.SelectedValidationID,
			change.SnapshotID, change.DefaultDOS, change.MetadataJSON, change.NowMS, change.NowMS,
		},
	})
	return creationMutation(result, err, "initialize creation review", 1)
}

func (records creationRecords) RPG(ctx context.Context, change application.CreationRPGProfile) error {
	profile, err := profilemodel.Encode(profilemodel.Review, profilemodel.RPGMakerProject, &profilemodel.RPGReview{
		Generation: change.Generation, EvidenceFamily: change.EvidenceFamily,
		EvidenceGeneration: change.EvidenceGeneration, EvidenceConfidence: change.EvidenceConfidence,
		EngineVersion: change.EngineVersion, EntryHTMLPath: change.EntryHTML,
		FileCount: change.FileCount, TotalBytes: change.TotalBytes,
		ProjectFingerprint: change.FilesDigest, RequirementsSHA256: change.RequirementsDigest,
		Analysis: json.RawMessage(change.AnalysisJSON), ProviderID: change.ProviderID, TargetID: change.TargetID,
		DependencySnapshotSHA256: change.DependencyDigest,
	})
	if err != nil {
		return fmt.Errorf("encode creation RPG profile: %w", err)
	}
	result, err := records.transaction.ExecContext(ctx, `
UPDATE import_items SET review_profile_json=?
WHERE id=? AND review_profile_json IS NULL AND EXISTS(
 SELECT 1 FROM runtime_targets WHERE provider_id=? AND target_id=?)`,
		profile, change.DraftID, change.ProviderID, change.TargetID)
	return creationMutation(result, err, "write creation RPG profile", 1)
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

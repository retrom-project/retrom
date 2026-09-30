package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/core/scummvm"
	"retrom/internal/persistence/recordstore"
	"retrom/internal/profilemodel"
	libraryservice "retrom/internal/service/libraryimport"
)

func (records creationRecords) Validation(ctx context.Context, change libraryservice.CreationValidation) error {
	if snapshot, err := scummvm.ParseSnapshot(change.DependencyJSON); err == nil {
		profile, err := profilemodel.Encode(profilemodel.Review, profilemodel.ScummVMProject, &snapshot)
		if err != nil {
			return fmt.Errorf("save creation observations: %w", err)
		}
		_, err = records.transaction.ExecContext(ctx, `
UPDATE import_items SET review_profile_json=? WHERE id=?`, profile, change.ItemID)
		if err != nil {
			return fmt.Errorf("save creation observations: %w", err)
		}
		return nil
	}
	analysis, err := libraryservice.ContentAnalysisJSON(change.Status, change.Code, change.DependencyJSON)
	if err != nil {
		return fmt.Errorf("save creation observations: %w", err)
	}
	result, err := records.transaction.ExecContext(ctx, `
UPDATE import_items SET content_analysis_json=? WHERE id=?`, analysis, change.ItemID)
	if err := creationMutation(result, err, "save creation content analysis", 1); err != nil {
		return fmt.Errorf("save creation observations: %w", err)
	}
	for _, entry := range change.DOSEntries {
		result, err = records.transaction.ExecContext(
			ctx,
			`
INSERT INTO import_item_dos_entries(import_item_id,normalized_path,original_relative_path,
kind,rank,
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
			return fmt.Errorf("save creation observations: %w", err)
		}
	}
	for _, file := range change.Files {
		if file.Role == "BIOS_BUNDLE" || file.Role == "PARENT" {
			continue
		}
		result, err = recordstore.InsertRows(ctx, records.transaction, "import_item_runtime_files", `
INSERT INTO import_item_runtime_files(import_item_id,role,logical_name,
file_record,
 sort_order,created_at_ms)
VALUES(?,?,?,?,?,?)`, change.ItemID, file.Role, file.LogicalName, file.FileRecord, file.SortOrder, change.NowMS)
		if err := creationMutation(result, err, "insert creation validation file", 1); err != nil {
			return fmt.Errorf("save creation observations: %w", err)
		}
	}
	return nil
}

func (records creationRecords) Draft(ctx context.Context, change libraryservice.CreationDraft) error {
	if change.ID != change.ItemID {
		return libraryservice.ErrInvalid
	}
	result, err := recordstore.UpdateReviewItems(ctx, records.transaction, recordstore.Update{
		Set: `search_text=?,target_platform_instance_id=?,
effective_source_snapshot_id=?,default_dos_entry=?,metadata_json=?,review_version=1,
review_created_at_ms=?,review_updated_at_ms=?`,
		Scope: recordstore.Scope{Where: `id=? AND review_version=0`, Args: []any{change.ItemID}},
		Values: []any{
			change.SearchText, change.TargetID,
			change.SnapshotID, change.DefaultDOS, change.MetadataJSON, change.NowMS, change.NowMS,
		},
	})
	return creationMutation(result, err, "initialize creation review", 1)
}

func (records creationRecords) RPG(ctx context.Context, change libraryservice.CreationRPGProfile) error {
	profile, err := profilemodel.Encode(
		profilemodel.Review,
		profilemodel.RPGMakerProject,
		&profilemodel.RPGReview{
			Generation:               change.Generation,
			EvidenceFamily:           change.EvidenceFamily,
			EvidenceGeneration:       change.EvidenceGeneration,
			EvidenceConfidence:       change.EvidenceConfidence,
			EngineVersion:            change.EngineVersion,
			EntryHTMLPath:            change.EntryHTML,
			FileCount:                change.FileCount,
			TotalBytes:               change.TotalBytes,
			ProjectFingerprint:       change.FilesDigest,
			RequirementsSHA256:       change.RequirementsDigest,
			Analysis:                 json.RawMessage(change.AnalysisJSON),
			ProviderID:               change.ProviderID,
			TargetID:                 change.TargetID,
			DependencySnapshotSHA256: change.DependencyDigest,
		},
	)
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

func (records creationRecords) Events(ctx context.Context, events []libraryservice.CreationEvent) error {
	for _, event := range events {
		result, err := records.transaction.ExecContext(
			ctx,
			`
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,?,?,?,?,?)`,
			event.JobID,
			event.ScopeType,
			event.ScopeID,
			event.Kind,
			event.DataJSON,
			event.NowMS,
		)
		if err := creationMutation(result, err, "insert creation event", 1); err != nil {
			return fmt.Errorf("save creation observations: %w", err)
		}
	}
	return nil
}

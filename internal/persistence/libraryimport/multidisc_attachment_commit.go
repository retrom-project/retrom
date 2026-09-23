package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/contentcapability"
	"retrom/internal/dbexec"
	"retrom/internal/multidisc"
	"retrom/internal/persistence/blobcatalog"
	"retrom/internal/persistence/contentquery"
	validationpersistence "retrom/internal/persistence/corevalidation"
	"retrom/internal/persistence/recordstore"
	validationservice "retrom/internal/service/corevalidation"
	application "retrom/internal/service/libraryimport"
)

type MultiDiscAttachmentFinalization struct{ database *sql.DB }

var _ application.MultiDiscAttachmentCommitRepository = (*MultiDiscAttachmentFinalization)(nil)

func NewMultiDiscAttachmentFinalization(database *sql.DB) *MultiDiscAttachmentFinalization {
	return &MultiDiscAttachmentFinalization{database: database}
}

type multiDiscAttachmentCommitScope struct{ transaction *sql.Tx }

var _ application.MultiDiscAttachmentCommitScope = multiDiscAttachmentCommitScope{}

func (repository *MultiDiscAttachmentFinalization) WithCommit(
	ctx context.Context, work func(application.MultiDiscAttachmentCommitScope) error,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin multi-disc attachment commit: %w", err)
	}
	defer dbexec.Rollback(transaction)
	if err := work(multiDiscAttachmentCommitScope{transaction: transaction}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit multi-disc attachment commit: %w", err)
	}
	return nil
}

func (scope multiDiscAttachmentCommitScope) BIOS(
	ctx context.Context, providerID, targetID string,
) ([]validationservice.BIOSRecord, error) {
	result, err := validationpersistence.New(scope.transaction).BIOS(ctx, providerID, targetID)
	if err != nil {
		return nil, fmt.Errorf("read multi-disc BIOS records: %w", err)
	}
	return result, nil
}

func (scope multiDiscAttachmentCommitScope) CommitAccepted(
	ctx context.Context, write application.MultiDiscAttachmentCommitWrite,
) error {
	if err := scope.validateCurrentInput(ctx, write.Input); err != nil {
		return err
	}
	if err := scope.validateOwnership(ctx, write); err != nil {
		return err
	}
	if err := scope.insertSourceSnapshot(ctx, write); err != nil {
		return err
	}
	if err := scope.insertValidation(ctx, write); err != nil {
		return err
	}
	selectedValidation := any(nil)
	if write.Validation.Status == "READY" {
		selectedValidation = write.ValidationID
	}
	result, err := recordstore.UpdateReviewItems(ctx, scope.transaction, recordstore.Update{
		Set: `effective_source_snapshot_id=?,selected_validation_id=?,review_version=review_version+1,review_updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND effective_source_snapshot_id=?`,
			Args:  []any{write.Input.ReviewDraftID, write.Input.BaseSourceSnapshotID},
		},
		Values: []any{write.SourceSnapshotID, selectedValidation, write.NowMS},
	})
	if err := requireMultiDiscChange(result, err, "advance review source"); err != nil {
		return err
	}
	if err := scope.recordDuplicateEvidence(ctx, write); err != nil {
		return err
	}
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "discCount": len(write.ResultEntries),
		"attachedFileCount": len(write.BaseFiles), "validationStatus": write.Validation.Status,
		"durationMs": multiDiscAttachmentDurationMS(write.ExecutionStartedAtMS, write.NowMS),
	})
	result, err = recordstore.UpdateReviewMultidiscAttachments(ctx, scope.transaction, recordstore.Update{
		Set: `state='ACCEPTED',result_source_snapshot_id=?,result_validation_id=?,diagnostics_json=?,
error_code=NULL,finished_at_ms=?,version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='RUNNING'`, Args: []any{write.Input.AttachmentID}},
		Values: []any{write.SourceSnapshotID, write.ValidationID, string(diagnostics), write.NowMS, write.NowMS},
	})
	if err := requireMultiDiscChange(result, err, "accept attachment"); err != nil {
		return err
	}
	if _, err := recordstore.CreateUploadConsumptions(ctx, scope.transaction, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,NULL,'REVIEW_MULTI_DISC',?,?)
`, write.ConsumptionID, write.Input.UploadSessionID, write.Input.AttachmentID, write.NowMS); err != nil {
		return fmt.Errorf("consume multi-disc upload: %w", err)
	}
	result, err = recordstore.UpdateImportItems(ctx, scope.transaction, recordstore.Update{
		Set:    `version=version+1,updated_at_ms=?`,
		Scope:  recordstore.Scope{Where: `id=? AND state='REVIEW_PENDING'`, Args: []any{write.Input.ImportItemID}},
		Values: []any{write.NowMS},
	})
	if err := requireMultiDiscChange(result, err, "advance import item"); err != nil {
		return err
	}
	if err := scope.recordAcceptedJobEvents(ctx, write); err != nil {
		return err
	}
	result, err = scope.transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, write.NowMS, write.NowMS, write.JobID, write.WorkerID)
	if err := requireMultiDiscChange(result, err, "complete job"); err != nil {
		return err
	}
	return nil
}

func (scope multiDiscAttachmentCommitScope) validateCurrentInput(
	ctx context.Context, input application.MultiDiscAttachmentInput,
) error {
	var state, snapshotID, platformID, platformInstanceID, coreID string
	var providerID, targetID string
	var platformVersion int64
	var policy contentcapability.Policy
	err := scope.transaction.QueryRowContext(ctx, `
SELECT item.state,draft.effective_source_snapshot_id,platform.platform_id,platform.id,
platform.version,platform.default_core_id,target.provider_id,target.target_id,
`+contentquery.BindingPolicySQL+`
FROM import_items item
JOIN import_items draft ON draft.id=? AND draft.id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN runtime_target_bindings binding ON binding.core_id=platform.default_core_id
  AND binding.launch_policy<>'DISABLED'
JOIN runtime_binding_platforms platform_binding ON platform_binding.binding_id=binding.binding_id
  AND platform_binding.platform_id=platform.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id
  AND target.target_id=binding.target_id
WHERE item.id=?
`, input.ReviewDraftID, input.ImportItemID).Scan(
		&state, &snapshotID, &platformID, &platformInstanceID, &platformVersion,
		&coreID, &providerID, &targetID, contentquery.ScanPolicy(&policy),
	)
	if err != nil {
		return fmt.Errorf("read current multi-disc attachment input: %w", err)
	}
	if state != "REVIEW_PENDING" || snapshotID != input.BaseSourceSnapshotID ||
		platformID != input.TargetPlatformID || platformInstanceID != input.PlatformInstanceID ||
		platformVersion != input.PlatformVersion || coreID != input.CoreID ||
		providerID != input.ProviderID || targetID != input.TargetID ||
		policy.DigestFor("MULTI_DISC") != input.ContentPolicyDigest {
		return application.ErrInvalid
	}
	capabilities := contentcapability.Resolve(platformID, true, true, policy)
	if capabilities.MultiDisc == nil || capabilities.MultiDisc.MaxDiscs != input.MaxDiscs ||
		capabilities.MultiDisc.MaxTotalBytes != input.MaxTotalBytes {
		return application.ErrInvalid
	}
	return nil
}

func (scope multiDiscAttachmentCommitScope) validateOwnership(
	ctx context.Context, write application.MultiDiscAttachmentCommitWrite,
) error {
	var state, workerID string
	if err := scope.transaction.QueryRowContext(ctx,
		`SELECT state,worker_id FROM jobs WHERE id=?`, write.JobID).Scan(&state, &workerID); err != nil ||
		state != "RUNNING" || workerID != write.WorkerID {
		return application.ErrInvalid
	}
	var consumed int
	if err := scope.transaction.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM upload_consumptions
  WHERE upload_session_id=? AND upload_file_id IS NULL
)
`, write.Input.UploadSessionID).Scan(&consumed); err != nil || consumed != 0 {
		return application.ErrInvalid
	}
	entries, err := BindMultiDiscAdmission(scope.transaction).Entries(ctx, write.Input.BaseSourceSnapshotID)
	if err != nil {
		return fmt.Errorf("read multi-disc source entries: %w", err)
	}
	digest, err := multidisc.ExpectedSetDigest(entries)
	if err != nil || digest != write.Input.ExpectedSetDigest {
		return application.ErrInvalid
	}
	return nil
}

func (scope multiDiscAttachmentCommitScope) insertSourceSnapshot(
	ctx context.Context, write application.MultiDiscAttachmentCommitWrite,
) error {
	if _, err := scope.transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshots(id,import_item_id,content_kind,
source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,'MULTI_DISC',?,?,'MULTI_DISC_ATTACHMENT',?)
`, write.SourceSnapshotID, write.Input.ImportItemID, write.ResultManifestJSON,
		write.ResultManifestDigest, write.NowMS); err != nil {
		return fmt.Errorf("insert multi-disc source snapshot: %w", err)
	}
	for _, file := range write.BaseFiles {
		if _, err := scope.transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,
upload_file_id,blob_id,source_archive_blob_id,source_archive_entry_ordinal,sort_order,created_at_ms)
VALUES(?,?,?,?,?,NULL,NULL,?,?)
`, write.SourceSnapshotID, file.Role, file.LogicalName, nullableStringValue(file.UploadFileID),
			file.BlobID, file.SortOrder, write.NowMS); err != nil {
			return fmt.Errorf("insert multi-disc source file: %w", err)
		}
	}
	for _, entry := range write.ResultEntries {
		if _, err := recordstore.CreateImportItemMultidiscEntries(ctx, scope.transaction, `
INSERT INTO import_item_multidisc_entries(source_snapshot_id,ordinal,source_reference,
normalized_reference,canonical_name,state,upload_file_id,blob_id,source_logical_name,created_at_ms)
VALUES(?,?,?,?,?,'PRESENT',?,?,?,?)
`, write.SourceSnapshotID, entry.Ordinal, entry.SourceReference, entry.NormalizedReference,
			entry.CanonicalName, nullableStringValue(entry.File.UploadFileID), nullableStringValue(entry.File.BlobID),
			entry.File.LogicalName, write.NowMS); err != nil {
			return fmt.Errorf("insert multi-disc entry: %w", err)
		}
	}
	return nil
}

func (scope multiDiscAttachmentCommitScope) insertValidation(
	ctx context.Context, write application.MultiDiscAttachmentCommitWrite,
) error {
	canonicalBlobID, err := blobcatalog.EnsureRecord(
		ctx, scope.transaction, write.CanonicalPlaylist, "application/vnd.retrom.m3u", write.NowMS,
	)
	if err != nil {
		return fmt.Errorf("register multi-disc canonical playlist: %w", err)
	}
	inputDigest := application.PrepublishDigest(application.PrepublishDigestInput{
		SchemaVersion: 1, SourceSnapshotID: write.SourceSnapshotID,
		SourceManifestDigest: write.ResultManifestDigest, ContentKind: multidisc.ContentKind,
		TargetPlatformInstanceID: write.Input.PlatformInstanceID, ProviderID: write.Input.ProviderID,
		TargetID: write.Input.TargetID, ContentPolicyDigest: write.Input.ContentPolicyDigest,
		DependencySnapshot: json.RawMessage(write.Validation.DependencySnapshotJSON),
		Status:             write.Validation.Status, CompatibilityCode: write.Validation.CompatibilityCode,
	})
	if inputDigest == "" {
		return application.ErrInvalid
	}
	if _, err := recordstore.CreateImportItemCoreValidations(ctx, scope.transaction, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
platform_instance_version,core_id,provider_id,target_id,
dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
status,compatibility_code,dependency_snapshot_json,created_at_ms)
VALUES(?,?,?,?,?,?,?,NULL,NULL,?,?,?,?,?,?,?)
`, write.ValidationID, write.Input.ImportItemID, write.Input.PlatformInstanceID,
		write.Input.PlatformVersion, write.Input.CoreID, write.Input.ProviderID, write.Input.TargetID,
		write.ResultManifestDigest, write.SourceSnapshotID, inputDigest, write.Validation.Status,
		write.Validation.CompatibilityCode, write.Validation.DependencySnapshotJSON, write.NowMS); err != nil {
		return fmt.Errorf("insert multi-disc validation: %w", err)
	}
	validationFiles := append([]application.PreparedValidationFile(nil), write.Validation.Files...)
	validationFiles = append(validationFiles, application.PreparedValidationFile{
		Role: "MULTI_DISC_PLAYLIST", LogicalName: "playlist.m3u", BlobID: canonicalBlobID, SortOrder: 0,
	})
	for _, file := range validationFiles {
		if _, err := scope.transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,
sort_order,created_at_ms) VALUES(?,?,?,?,?,?)
`, write.ValidationID, file.Role, file.LogicalName, file.BlobID, file.SortOrder, write.NowMS); err != nil {
			return fmt.Errorf("insert multi-disc validation file: %w", err)
		}
	}
	return nil
}

func (scope multiDiscAttachmentCommitScope) recordDuplicateEvidence(
	ctx context.Context, write application.MultiDiscAttachmentCommitWrite,
) error {
	duplicates := application.NewContentDuplicates(BindContentDuplicates(scope.transaction))
	identity, err := duplicates.Identity(ctx, write.Input.ImportItemID)
	if err != nil {
		return fmt.Errorf("read multi-disc content identity: %w", err)
	}
	if err := BindReviewApproval(scope.transaction).Decisions.ClaimIdentity(
		ctx, write.Input.TargetPlatformID, identity, write.NowMS,
	); err != nil {
		return fmt.Errorf("claim multi-disc content identity: %w", err)
	}
	games, err := duplicates.Matches(ctx, write.Input.ImportItemID, write.Input.TargetPlatformID)
	if err != nil {
		return fmt.Errorf("read multi-disc duplicate games: %w", err)
	}
	for _, game := range games {
		if _, err := scope.transaction.ExecContext(ctx, `
INSERT INTO import_item_duplicate_matches(
 import_item_id,existing_game_id,content_identity_digest,detected_stage,created_at_ms
) VALUES(?,?,?,'IDENTIFICATION',?) ON CONFLICT(import_item_id,existing_game_id) DO NOTHING
`, write.Input.ImportItemID, game.GameID, identity, write.NowMS); err != nil {
			return fmt.Errorf("insert multi-disc duplicate evidence: %w", err)
		}
	}
	return nil
}

func (scope multiDiscAttachmentCommitScope) recordAcceptedJobEvents(
	ctx context.Context, write application.MultiDiscAttachmentCommitWrite,
) error {
	parserData, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "parserResultCode": "MATCHED", "discCount": len(write.ResultEntries),
	})
	terminalData, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "state": "ACCEPTED", "validationStatus": write.Validation.Status,
		"durationMs": multiDiscAttachmentDurationMS(write.ExecutionStartedAtMS, write.NowMS),
	})
	if _, err := scope.transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'PLAYLIST_PARSED',?,?),
(?,'IMPORT_ITEM',?,'DISC_SET_MATCHED',?,?),
(?,'IMPORT_ITEM',?,'SOURCE_SNAPSHOT_CREATED',?,?),
(?,'IMPORT_ITEM',?,'CORE_VALIDATION_COMPLETED',?,?),
(?,'IMPORT_ITEM',?,'SUCCEEDED',?,?)
`, write.JobID, write.Input.ImportItemID, string(parserData), write.NowMS,
		write.JobID, write.Input.ImportItemID, string(parserData), write.NowMS,
		write.JobID, write.Input.ImportItemID,
		fmt.Sprintf(`{"sourceSnapshotId":%q}`, write.SourceSnapshotID), write.NowMS,
		write.JobID, write.Input.ImportItemID,
		fmt.Sprintf(`{"validationId":%q,"status":%q}`, write.ValidationID, write.Validation.Status), write.NowMS,
		write.JobID, write.Input.ImportItemID, string(terminalData), write.NowMS); err != nil {
		return fmt.Errorf("record multi-disc job events: %w", err)
	}
	return nil
}

func requireMultiDiscChange(result sql.Result, err error, action string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", action, err)
	}
	if changed != 1 {
		return application.ErrInvalid
	}
	return nil
}

func nullableStringValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func multiDiscAttachmentDurationMS(startedAtMS, nowMS int64) int64 {
	if startedAtMS <= 0 || nowMS <= startedAtMS {
		return 0
	}
	return nowMS - startedAtMS
}

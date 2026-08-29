package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
)

// Every accepted artifact and audit row must commit atomically.
func (service *Service) commitAcceptedParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
	entries []importing.ArchiveEntry,
	files []attachedSourceFile,
	manifestJSON, manifestDigest string,
	validation preparedGroup,
	diagnostics map[string]any,
) error {
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return parentStoreError("begin accepted commit", err)
	}
	defer cleanup.Rollback(transaction)
	target, err := loadParentCommitTarget(ctx, transaction, candidate)
	if err != nil {
		return err
	}
	if err := validateParentCommitJob(ctx, transaction, jobID, workerID); err != nil {
		return err
	}
	artifacts, err := service.insertParentCommitArtifacts(
		ctx, transaction, candidate, entries, files, manifestJSON, manifestDigest, validation, target,
	)
	if err != nil {
		return err
	}
	newSnapshotID := artifacts.snapshotID
	validationID := artifacts.validationID
	now := artifacts.now
	diagnosticsJSON, _ := json.Marshal(diagnostics)
	selectedValidation := selectedParentValidation(validation.validationStatus, validationID)
	consumptionID, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()
	evidence := marshalReviewEventV2(map[string]any{
		"attachmentKind": "ARCADE_PARENT", "machine": candidate.machine,
		"validationStatus": validation.validationStatus, "state": "ACCEPTED",
	})
	result, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='ACCEPTED',accepted_blob_id=?,
result_source_snapshot_id=?,observed_size_bytes=?,observed_sha256=?,diagnostics_json=?,error_code=NULL,
finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, candidate.blobID, newSnapshotID, candidate.blobSize, candidate.blobSHA,
		string(diagnosticsJSON), now, now, candidate.attachmentID)
	if err := requireParentCommitChange(result, err, "accept attachment"); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,?,'REVIEW_ARCADE_PARENT',?,?)
	`, consumptionID.String(), candidate.uploadSessionID, candidate.uploadFileID,
		candidate.attachmentID, now); err != nil {
		return parentStoreError("consume parent upload", err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE review_drafts SET effective_source_snapshot_id=?,selected_validation_id=?,
version=version+1,updated_at_ms=? WHERE id=? AND effective_source_snapshot_id=?
`, newSnapshotID, selectedValidation, now, candidate.draftID, candidate.baseSnapshotID)
	if err := requireParentCommitChange(result, err, "advance review source"); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items SET version=version+1,updated_at_ms=? WHERE id=? AND state='REVIEW_PENDING';
	`, now, candidate.itemID); err != nil {
		return parentStoreError("advance import item", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'PARENT_ATTACHMENT_ACCEPTED',?,?,?,?,?,?,?,?,?,?)
`, eventID.String(), candidate.itemID, reviewActor(ctx).Kind, reviewActor(ctx).UserID, reviewActor(ctx).Label,
		emptyReviewEventV2, evidence, evidence, emptyReviewEventV2,
		emptyReviewEventV2, emptyReviewEventV2, now); err != nil {
		return parentStoreError("record parent review event", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'ARCHIVE_SCANNED','{}',?),
(?,'IMPORT_ITEM',?,'PARENT_MATCHED','{}',?),
(?,'IMPORT_ITEM',?,'SOURCE_SNAPSHOT_CREATED',?,?),
(?,'IMPORT_ITEM',?,'CORE_VALIDATION_COMPLETED',?,?),
(?,'IMPORT_ITEM',?,'SUCCEEDED','{}',?)
`, jobID, candidate.itemID, now,
		jobID, candidate.itemID, now,
		jobID, candidate.itemID, fmt.Sprintf(`{"sourceSnapshotId":%q}`, newSnapshotID), now,
		jobID, candidate.itemID,
		fmt.Sprintf(`{"validationId":%q,"status":%q}`, validationID, validation.validationStatus), now,
		jobID, candidate.itemID, now); err != nil {
		return parentStoreError("record accepted job events", err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, now, now, jobID, workerID)
	if err := requireParentCommitChange(result, err, "complete parent job"); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return parentStoreError("commit accepted attachment", err)
	}
	return nil
}

func selectedParentValidation(status, validationID string) any {
	if status == "READY" {
		return validationID
	}
	return nil
}

func requireParentCommitChange(result sql.Result, err error, action string) error {
	if err != nil {
		return parentStoreError(action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return parentStoreError(action+" result", err)
	}
	if changed != 1 {
		return ErrInvalid
	}
	return nil
}

type parentCommitArtifacts struct {
	snapshotID   string
	validationID string
	now          int64
}

func (service *Service) insertParentCommitArtifacts(
	ctx context.Context,
	transaction *sql.Tx,
	candidate parentAttachmentCandidate,
	entries []importing.ArchiveEntry,
	files []attachedSourceFile,
	manifestJSON, manifestDigest string,
	validation preparedGroup,
	target parentCommitTarget,
) (parentCommitArtifacts, error) {
	snapshotUUID, _ := uuid.NewV7()
	validationUUID, _ := uuid.NewV7()
	snapshotID, validationID := snapshotUUID.String(), validationUUID.String()
	var revision int
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(revision_no),0)+1
FROM import_item_source_snapshots WHERE import_item_id=?
`, candidate.itemID).Scan(&revision); err != nil {
		return parentCommitArtifacts{}, parentStoreError("allocate source revision", err)
	}
	now := service.now().UnixMilli()
	_, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshots(
  id,import_item_id,revision_no,source_manifest_json,source_manifest_digest,
  content_kind,created_by,created_at_ms
) VALUES(?,?,?,?,?,?,'ARCADE_PARENT_ATTACHMENT',?)
`, snapshotID, candidate.itemID, revision, manifestJSON, manifestDigest, target.contentKind, now)
	if err != nil {
		return parentCommitArtifacts{}, parentStoreError("insert source snapshot", err)
	}
	if err := insertParentSnapshotFiles(ctx, transaction, snapshotID, files, now); err != nil {
		return parentCommitArtifacts{}, err
	}
	if err := insertParentArchiveEntries(ctx, transaction, candidate.blobID, entries, now); err != nil {
		return parentCommitArtifacts{}, err
	}
	if err := insertParentCoreValidation(
		ctx, transaction, candidate, snapshotID, validationID, manifestDigest, validation, target, now,
	); err != nil {
		return parentCommitArtifacts{}, err
	}
	return parentCommitArtifacts{snapshotID: snapshotID, validationID: validationID, now: now}, nil
}

func insertParentCoreValidation(
	ctx context.Context,
	transaction *sql.Tx,
	candidate parentAttachmentCandidate,
	snapshotID, validationID, manifestDigest string,
	validation preparedGroup,
	target parentCommitTarget,
	now int64,
) error {
	digest := prepublishDigest(prepublishDigestInput{
		SchemaVersion: 1, ValidatorVersion: validatorArcadeV4, SourceSnapshotID: snapshotID,
		SourceManifestDigest: manifestDigest, ContentKind: target.contentKind,
		TargetPlatformInstanceID: target.targetID, PlatformInstanceVersion: target.platformVersion,
		CoreArtifactID: target.artifactID, CoreArtifactVersion: target.artifactVersion,
		CompatibilityConfigDigest: compatibilityConfigDigest(target.compatibilityConfig),
		DATVersionID:              stringPointer(candidate.datID),
		DependencySnapshot:        json.RawMessage(validation.dependencySnapshot),
		Status:                    validation.validationStatus, CompatibilityCode: validation.compatibilityCode,
	})
	_, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_core_validations(
  id,import_item_id,target_platform_instance_id,platform_instance_version,core_id,
  core_artifact_id,dat_version_id,default_dos_entry,core_artifact_version,
  prepublish_generation,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
  status,compatibility_code,dependency_snapshot_json,created_at_ms
) VALUES(?,?,?,?,?,?,?,NULL,?,?,?,?,?,?,?,?,?)
`, validationID, candidate.itemID, target.targetID, target.platformVersion, target.coreID,
		target.artifactID, candidate.datID, target.artifactVersion, prepublishGeneration,
		manifestDigest, snapshotID, digest, validation.validationStatus,
		validation.compatibilityCode, validation.dependencySnapshot, now)
	if err != nil {
		return parentStoreError("insert source validation", err)
	}
	for _, file := range validation.validationFiles {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_validation_files(
  import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms
) VALUES(?,?,?,?,?,?)
`, validationID, file.role, file.logicalName, file.blobID, file.sortOrder, now)
		if err != nil {
			return parentStoreError("insert validation file", err)
		}
	}
	return nil
}

type parentCommitTarget struct {
	contentKind         string
	targetID            string
	coreID              string
	artifactID          string
	compatibilityConfig string
	platformVersion     int64
	artifactVersion     int64
}

func loadParentCommitTarget(
	ctx context.Context,
	transaction *sql.Tx,
	candidate parentAttachmentCandidate,
) (parentCommitTarget, error) {
	var target parentCommitTarget
	var itemState, currentSnapshotID string
	var activeDATID sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT item.state,draft.effective_source_snapshot_id,draft.target_platform_instance_id,
source_snapshot.content_kind,platform.version,platform.default_core_id,artifact.id,
artifact.version,artifact.compatibility_json,
(SELECT dat.id FROM dat_versions dat WHERE dat.core_artifact_id=artifact.id AND dat.is_active=1)
FROM import_items item
JOIN review_drafts draft ON draft.id=? AND draft.import_item_id=item.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN core_artifacts artifact ON artifact.core_id=platform.default_core_id AND artifact.selected_for_new_bindings=1
WHERE item.id=?
`, candidate.draftID, candidate.itemID).Scan(
		&itemState, &currentSnapshotID, &target.targetID, &target.contentKind,
		&target.platformVersion, &target.coreID, &target.artifactID,
		&target.artifactVersion, &target.compatibilityConfig, &activeDATID,
	)
	valid := err == nil && itemState == "REVIEW_PENDING" && currentSnapshotID == candidate.baseSnapshotID &&
		target.artifactID == candidate.artifactID && target.artifactVersion == candidate.artifactVersion &&
		compatibilityConfigDigest(target.compatibilityConfig) == candidate.compatibilityDigest &&
		activeDATID.Valid && activeDATID.String == candidate.datID
	if !valid {
		return parentCommitTarget{}, ErrInvalid
	}
	return target, nil
}

func validateParentCommitJob(
	ctx context.Context,
	transaction *sql.Tx,
	jobID, workerID string,
) error {
	var state, currentWorker string
	err := transaction.QueryRowContext(ctx, `SELECT state,worker_id FROM jobs WHERE id=?`, jobID).
		Scan(&state, &currentWorker)
	if err != nil || state != "RUNNING" || currentWorker != workerID {
		return ErrInvalid
	}
	return nil
}

func insertParentSnapshotFiles(
	ctx context.Context,
	transaction *sql.Tx,
	snapshotID string,
	files []attachedSourceFile,
	now int64,
) error {
	for _, file := range files {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshot_files(
  source_snapshot_id,role,logical_name,upload_file_id,blob_id,source_archive_blob_id,
  source_archive_entry_ordinal,sort_order,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`, snapshotID, file.role, file.logicalName, file.uploadFileID, file.blobID,
			nullable(file.archiveBlobID), nullableInt(file.archiveOrdinal), file.sortOrder, now)
		if err != nil {
			return parentStoreError("insert source snapshot file", err)
		}
	}
	return nil
}

func insertParentArchiveEntries(
	ctx context.Context,
	transaction *sql.Tx,
	archiveBlobID string,
	entries []importing.ArchiveEntry,
	now int64,
) error {
	for _, entry := range entries {
		_, err := transaction.ExecContext(ctx, `
INSERT OR IGNORE INTO archive_entries(
  archive_blob_id,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
  archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
  materialized_blob_id,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,?)
`, archiveBlobID, entry.Ordinal, entry.OriginalPath, entry.NormalizedPath,
			entry.ASCIICasefoldPath, entry.ArchiveFormat, entry.CompressionProfile, entry.Size,
			entry.CRC32, entry.MD5, entry.SHA1, entry.SHA256, now)
		if err != nil {
			return parentStoreError("insert source archive entry", err)
		}
	}
	return nil
}

func (service *Service) finishRejectedParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID, code, archiveCode string,
	missing, mismatched []string,
) {
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "archiveCode": archiveCode, "missingEntries": missing,
		"mismatchedEntries": mismatched,
	})
	now := service.now().UnixMilli()
	eventID, _ := uuid.NewV7()
	evidence := marshalReviewEventV2(map[string]any{
		"attachmentKind": "ARCADE_PARENT", "machine": candidate.machine,
		"state": "REJECTED", "errorCode": code,
	})
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='REJECTED',error_code=?,diagnostics_json=?,
observed_size_bytes=?,observed_sha256=?,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, code, string(diagnostics), candidate.blobSize, candidate.blobSHA, now, now, candidate.attachmentID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, jobID, workerID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'PARENT_REJECTED',?,?),(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, jobID, candidate.itemID, string(diagnostics), now, jobID, candidate.itemID,
		fmt.Sprintf(`{"errorCode":%q}`, code), now); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO review_events(id,import_item_id,event_type,actor_kind,actor_user_id,actor_label,
before_json,after_json,diff_json,
config_evidence_json,dat_evidence_json,provider_evidence_json,created_at_ms)
VALUES(?,?,'PARENT_ATTACHMENT_REJECTED',?,?,?,?,?,?,?,?,?,?)
`, eventID.String(), candidate.itemID, reviewActor(ctx).Kind, reviewActor(ctx).UserID, reviewActor(ctx).Label,
		emptyReviewEventV2, evidence, evidence, emptyReviewEventV2,
		emptyReviewEventV2, emptyReviewEventV2, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) finishRetryableParentAttachment(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID, code string,
) {
	now := service.now().UnixMilli()
	diagnostics := fmt.Sprintf(`{"errorCode":%q,"schemaVersion":1}`, code)
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	if _, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='FAILED_RETRYABLE',error_code=?,
diagnostics_json=?,observed_size_bytes=?,observed_sha256=?,finished_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING'
`, code, diagnostics, candidate.blobSize, candidate.blobSHA, now, now, candidate.attachmentID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=1,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, code, now, now, jobID, workerID); err != nil {
		return
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, jobID, candidate.itemID, diagnostics, now); err != nil {
		return
	}
	_ = transaction.Commit()
}

func (service *Service) SyncParentAttachmentCancellation(ctx context.Context, jobID string) {
	now := service.now().UnixMilli()
	_, _ = service.database.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','RUNNING','FAILED_RETRYABLE')
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='CANCELLED')
`, now, now, jobID, jobID)
}

func (service *Service) finishParentAttachmentCancellation(
	ctx context.Context,
	candidate parentAttachmentCandidate,
	jobID, workerID string,
) bool {
	var state string
	if err := service.database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND worker_id=?`, jobID, workerID).
		Scan(&state); err != nil || state != "CANCEL_REQUESTED" {
		return false
	}
	now := service.now().UnixMilli()
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer cleanup.Rollback(transaction)
	result, err := transaction.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments SET state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING'
`, now, now, candidate.attachmentID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='CANCEL_REQUESTED' AND worker_id=?
`, now, now, jobID, workerID)
	if err != nil {
		return false
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return false
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'CANCELLED','{}',?)
`, jobID, candidate.itemID, now); err != nil {
		return false
	}
	return transaction.Commit() == nil
}

func (service *Service) ReviewArcadeDependencies(ctx context.Context, itemID string) (any, bool, error) {
	var platformID string
	var validationStatus, compatibilityCode, dependencyJSON sql.NullString
	if err := service.database.QueryRowContext(ctx, `
SELECT platform.platform_id,validation.status,validation.compatibility_code,
validation.dependency_snapshot_json
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
LEFT JOIN import_item_core_validations validation ON validation.id=COALESCE(
  draft.selected_validation_id,
  (SELECT candidate.id FROM import_item_core_validations candidate
   WHERE candidate.import_item_id=item.id
   AND candidate.source_snapshot_id=draft.effective_source_snapshot_id
   AND candidate.target_platform_instance_id=draft.target_platform_instance_id
   ORDER BY candidate.created_at_ms DESC,candidate.id DESC LIMIT 1)
)
	WHERE item.id=? AND item.state='REVIEW_PENDING'
	`, itemID).Scan(&platformID, &validationStatus, &compatibilityCode, &dependencyJSON); err != nil {
		return nil, false, parentStoreError("read review dependency snapshot", err)
	}
	if platformID != "arcade" || !dependencyJSON.Valid {
		return nil, false, nil
	}
	snapshot, err := service.canonicalArcadeSnapshot(ctx, dependencyJSON.String)
	if err != nil {
		return nil, false, err
	}
	attachments, active, err := service.reviewParentAttachments(ctx, itemID)
	if err != nil {
		return nil, false, err
	}
	unsupported := compatibilityCode.String == "UNSUPPORTED_MERGED_ROMSET" ||
		compatibilityCode.String == "UNSUPPORTED_CHD" ||
		compatibilityCode.String == "ARCADE_DEPENDENCY_CYCLE" ||
		compatibilityCode.String == "ARCADE_DAT_UNAVAILABLE"
	nodes := make([]map[string]any, 0, len(snapshot.Dependencies))
	for _, dependency := range snapshot.Dependencies {
		latest := attachments[dependency.Machine]
		canAttach := dependency.Kind == "PARENT" &&
			(dependency.State == "MISSING" || dependency.State == "MISMATCH") && active == nil && !unsupported
		node := map[string]any{
			"kind": dependency.Kind, "machine": dependency.Machine, "requiredBy": dependency.RequiredBy,
			"depth": dependency.Depth, "expectedLogicalName": dependency.ExpectedLogicalName,
			"state": dependency.State, "requiredEntryCount": dependency.RequiredEntryCount,
			"requiredEntries": dependency.RequiredEntries, "canAttach": canAttach, "attachment": latest,
		}
		if dependency.Kind == "BIOS_OR_BASE" {
			node["managementUrl"] = "/admin/bios"
		}
		nodes = append(nodes, node)
	}
	return map[string]any{
		"machine": snapshot.Machine, "status": validationStatus.String,
		"compatibilityCode": compatibilityCode.String, "nodes": nodes, "activeAttachment": active,
	}, true, nil
}

func (service *Service) reviewParentAttachments(
	ctx context.Context,
	itemID string,
) (map[string]any, any, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,dependency_machine,expected_logical_name,original_filename,state,error_code,
job_id,observed_size_bytes,observed_sha256,diagnostics_json,created_at_ms,updated_at_ms,finished_at_ms
FROM review_arcade_parent_attachments
WHERE import_item_id=?
ORDER BY created_at_ms DESC,id DESC
	`, itemID)
	if err != nil {
		return nil, nil, parentStoreError("read review parent attachments", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	byMachine := make(map[string]any)
	var active any
	for rows.Next() {
		var id, machine, logicalName, originalName, state, jobID, diagnosticsJSON string
		var errorCode, observedSHA sql.NullString
		var observedSize, finishedAt sql.NullInt64
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&id, &machine, &logicalName, &originalName, &state, &errorCode, &jobID, &observedSize,
			&observedSHA, &diagnosticsJSON, &createdAt, &updatedAt, &finishedAt,
		); err != nil {
			return nil, nil, parentStoreError("scan review parent attachment", err)
		}
		var diagnostics any
		_ = json.Unmarshal([]byte(diagnosticsJSON), &diagnostics)
		value := map[string]any{
			"attachmentId": id, "machine": machine, "expectedLogicalName": logicalName,
			"originalFilename": originalName, "state": state, "errorCode": nullable(errorCode),
			"jobId": jobID, "observedSizeBytes": nullableInt(observedSize),
			"observedSha256": nullable(observedSHA), "diagnostics": diagnostics,
			"createdAtMs": createdAt, "updatedAtMs": updatedAt, "finishedAtMs": nullableInt(finishedAt),
		}
		if _, exists := byMachine[machine]; !exists {
			byMachine[machine] = value
		}
		if active == nil && (state == "QUEUED" || state == "RUNNING") {
			active = value
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, parentStoreError("iterate review parent attachments", err)
	}
	return byMachine, active, nil
}

package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	contentcapability "retrom/internal/content/capability"
	dbapi "retrom/internal/database"
	"retrom/internal/importing"
	"retrom/internal/persistence/contentquery"
	"retrom/internal/persistence/recordstore"
	libraryservice "retrom/internal/service/libraryimport"

	"github.com/google/uuid"
)

// ArcadeParentCommitRepository persists the accepted and terminal states of
// an arcade parent attachment. Each public operation owns its transaction so
// the application worker can keep validation and storage orchestration
// separate while retaining atomic state transitions.
type ArcadeParentCommitRepository struct{ database dbapi.DB }

var _ libraryservice.ArcadeParentCommitRepository = (*ArcadeParentCommitRepository)(nil)

func NewArcadeParentCommitRepository(database dbapi.DB) *ArcadeParentCommitRepository {
	return &ArcadeParentCommitRepository{database: database}
}

func (repository *ArcadeParentCommitRepository) CommitAccepted(
	ctx context.Context,
	request libraryservice.ArcadeParentAcceptedCommit,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return arcadeParentCommitStoreError("begin accepted commit", err)
	}
	defer dbapi.Rollback(transaction)
	if err := requireAttachmentWorker(
		ctx, transaction, request.JobID, request.WorkerID, request.NowMS, true,
	); err != nil {
		return err
	}
	target, err := loadArcadeParentCommitTarget(ctx, transaction, request.Candidate)
	if err != nil {
		return err
	}

	for source, destination := range request.FileCopies {
		if err := recordstore.CopyArchiveFacts(ctx, transaction, source, destination); err != nil {
			return arcadeParentCommitStoreError("copy parent snapshot archive indexes", err)
		}
	}
	artifacts, err := insertArcadeParentCommitArtifacts(
		ctx, transaction, request.Candidate, request.Entries, request.Files,
		request.ManifestJSON, request.ManifestDigest, request.Validation, target, request.NowMS,
	)
	if err != nil {
		return err
	}
	diagnosticsJSON := request.DiagnosticsJSON
	consumptionID, _ := uuid.NewV7()
	result, err := recordstore.UpdateReviewArcadeParentAttachments(ctx, transaction, recordstore.Update{
		Set: `
state='ACCEPTED',accepted_file_record=?,
result_source_snapshot_id=?,observed_size_bytes=?,observed_sha256=?,diagnostics_json=?,error_code=NULL,
finished_at_ms=?,version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='PENDING'`,
			Args:  []any{request.Candidate.AttachmentID},
		},
		Values: []any{
			request.Candidate.FileRecord,
			artifacts.snapshotID,
			request.Candidate.BlobSize,
			request.Candidate.BlobSHA,
			diagnosticsJSON,
			request.NowMS,
			request.NowMS,
		},
	})
	if err := requireArcadeParentCommitChange(result, err, "accept attachment"); err != nil {
		return err
	}
	if _, err := recordstore.CreateUploadConsumptions(
		ctx,
		transaction,
		`
INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
VALUES(?,?,?,'REVIEW_ARCADE_PARENT',?,?)
	`,
		consumptionID.String(),
		request.Candidate.UploadSessionID,
		request.Candidate.UploadFileID,

		request.Candidate.AttachmentID,
		request.NowMS,
	); err != nil {
		return arcadeParentCommitStoreError("consume parent upload", err)
	}
	result, err = recordstore.UpdateReviewItems(ctx, transaction, recordstore.Update{
		Set: `
effective_source_snapshot_id=?,
review_version=review_version+1,review_updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND effective_source_snapshot_id=?`,
			Args:  []any{request.Candidate.DraftID, request.Candidate.BaseSnapshotID},
		},
		Values: []any{artifacts.snapshotID, request.NowMS},
	})
	if err := requireArcadeParentCommitChange(result, err, "advance review source"); err != nil {
		return err
	}
	if _, err := recordstore.UpdateImportItems(ctx, transaction, recordstore.Update{
		Set: `version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='REVIEW_PENDING'`,
			Args:  []any{request.Candidate.ItemID},
		},
		Values: []any{request.NowMS},
	}); err != nil {
		return arcadeParentCommitStoreError("advance import item", err)
	}

	if _, err := transaction.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms) VALUES
(?,'IMPORT_ITEM',?,'ARCHIVE_SCANNED','{}',?),
(?,'IMPORT_ITEM',?,'PARENT_MATCHED','{}',?),
(?,'IMPORT_ITEM',?,'SOURCE_SNAPSHOT_CREATED',?,?),
(?,'IMPORT_ITEM',?,'CORE_VALIDATION_COMPLETED',?,?),
(?,'IMPORT_ITEM',?,'SUCCEEDED','{}',?)
`,
		request.JobID,
		request.Candidate.ItemID,
		request.NowMS,

		request.JobID,
		request.Candidate.ItemID,
		request.NowMS,

		request.JobID,
		request.Candidate.ItemID,
		fmt.Sprintf(`{"sourceSnapshotId":%q}`, artifacts.snapshotID),
		request.NowMS,

		request.JobID,
		request.Candidate.ItemID,

		fmt.Sprintf(`{"status":%q}`, request.Validation.Status),
		request.NowMS,

		request.JobID,
		request.Candidate.ItemID,
		request.NowMS,
	); err != nil {
		return arcadeParentCommitStoreError("record accepted job events", err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='SUCCEEDED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='RUNNING' AND worker_id=?
`, request.NowMS, request.NowMS, request.JobID, request.WorkerID)
	if err := requireArcadeParentCommitChange(result, err, "complete parent job"); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return arcadeParentCommitStoreError("commit accepted attachment", err)
	}
	return nil
}

func (repository *ArcadeParentCommitRepository) FinishRejected(
	ctx context.Context,
	request libraryservice.ArcadeParentRejectedCommit,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return arcadeParentCommitStoreError("begin rejected attachment", err)
	}
	defer dbapi.Rollback(transaction)
	if err := requireAttachmentWorker(
		ctx, transaction, request.JobID, request.WorkerID, request.NowMS, false,
	); err != nil {
		return err
	}
	if _, err := recordstore.UpdateReviewArcadeParentAttachments(
		ctx,
		transaction,
		recordstore.Update{
			Set: `
state='REJECTED',error_code=?,diagnostics_json=?,
observed_size_bytes=?,observed_sha256=?,finished_at_ms=?,version=version+1,updated_at_ms=?
`,

			Scope: recordstore.Scope{
				Where: `id=? AND state='PENDING'`,
				Args:  []any{request.AttachmentID},
			},

			Values: []any{
				request.Code,
				request.DiagnosticsJSON,
				request.BlobSize,
				request.BlobSHA,
				request.NowMS,
				request.NowMS,
			},
		},
	); err != nil {
		return arcadeParentCommitStoreError("reject attachment", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=0,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, request.Code, request.NowMS, request.NowMS, request.JobID, request.WorkerID); err != nil {
		return arcadeParentCommitStoreError("fail rejected parent job", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'PARENT_REJECTED',?,?),(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, request.JobID, request.ItemID, request.DiagnosticsJSON, request.NowMS,
		request.JobID, request.ItemID, fmt.Sprintf(`{"errorCode":%q}`, request.Code), request.NowMS); err != nil {
		return arcadeParentCommitStoreError("record rejected parent events", err)
	}

	if err := transaction.Commit(); err != nil {
		return arcadeParentCommitStoreError("commit rejected attachment", err)
	}
	return nil
}

func (repository *ArcadeParentCommitRepository) FinishRetryable(
	ctx context.Context,
	request libraryservice.ArcadeParentRetryableCommit,
) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return arcadeParentCommitStoreError("begin retryable attachment", err)
	}
	defer dbapi.Rollback(transaction)
	if err := requireAttachmentWorker(
		ctx, transaction, request.JobID, request.WorkerID, request.NowMS, false,
	); err != nil {
		return err
	}
	if _, err := recordstore.UpdateReviewArcadeParentAttachments(
		ctx,
		transaction,
		recordstore.Update{
			Set: `
diagnostics_json=?,observed_size_bytes=?,observed_sha256=?,version=version+1,
updated_at_ms=?
`,

			Scope: recordstore.Scope{
				Where: `id=? AND state='PENDING'`,
				Args:  []any{request.AttachmentID},
			},

			Values: []any{
				request.DiagnosticsJSON,
				request.BlobSize,
				request.BlobSHA,
				request.NowMS,
			},
		},
	); err != nil {
		return arcadeParentCommitStoreError("mark retryable attachment", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code=?,error_retryable=1,finished_at_ms=?,leased_until_ms=NULL,
heartbeat_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE id=? AND state='RUNNING' AND worker_id=?
`, request.Code, request.NowMS, request.NowMS, request.JobID, request.WorkerID); err != nil {
		return arcadeParentCommitStoreError("fail retryable parent job", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'FAILED',?,?)
`, request.JobID, request.ItemID, request.DiagnosticsJSON, request.NowMS); err != nil {
		return arcadeParentCommitStoreError("record retryable parent event", err)
	}
	if err := transaction.Commit(); err != nil {
		return arcadeParentCommitStoreError("commit retryable attachment", err)
	}
	return nil
}

func (repository *ArcadeParentCommitRepository) SyncCancellation(
	ctx context.Context,
	request libraryservice.ArcadeParentCancellationSync,
) error {
	_, err := recordstore.UpdateReviewArcadeParentAttachments(ctx, repository.database, recordstore.Update{
		Set: `
state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `
job_id=? AND state='PENDING'
AND EXISTS(SELECT 1 FROM jobs WHERE id=? AND state='CANCELLED')
`,
			Args: []any{request.JobID, request.JobID},
		},
		Values: []any{request.NowMS, request.NowMS},
	})
	if err != nil {
		return arcadeParentCommitStoreError("sync attachment cancellation", err)
	}
	return nil
}

func (repository *ArcadeParentCommitRepository) FinishCancellation(
	ctx context.Context,
	request libraryservice.ArcadeParentAttachmentCancellation,
) (bool, error) {
	var state string
	if err := dbapi.QueryRowContext(ctx, repository.database,
		`SELECT state FROM jobs WHERE id=? AND worker_id=?`, request.JobID, request.WorkerID).
		Scan(&state); err != nil {
		return false, arcadeParentCommitStoreError("read cancellation job", err)
	}
	if state != "CANCEL_REQUESTED" {
		return false, nil
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return false, arcadeParentCommitStoreError("begin cancellation", err)
	}
	defer dbapi.Rollback(transaction)
	result, err := recordstore.UpdateReviewArcadeParentAttachments(ctx, transaction, recordstore.Update{
		Set: `
state='CANCELLED',error_code='CANCELLED',
diagnostics_json='{"errorCode":"CANCELLED","schemaVersion":1}',finished_at_ms=?,
version=version+1,updated_at_ms=?
`,
		Scope: recordstore.Scope{
			Where: `id=? AND state='PENDING'`,
			Args:  []any{request.AttachmentID},
		},
		Values: []any{request.NowMS, request.NowMS},
	})
	if err != nil {
		return false, arcadeParentCommitStoreError("cancel attachment", err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return false, arcadeParentCommitStoreError("cancel attachment result", err)
	} else if changed != 1 {
		return false, nil
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE jobs SET state='CANCELLED',finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,
version=version+1,updated_at_ms=? WHERE id=? AND state='CANCEL_REQUESTED' AND worker_id=?
`, request.NowMS, request.NowMS, request.JobID, request.WorkerID)
	if err != nil {
		return false, arcadeParentCommitStoreError("cancel parent job", err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return false, arcadeParentCommitStoreError("cancel parent job result", err)
	} else if changed != 1 {
		return false, nil
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'IMPORT_ITEM',?,'CANCELLED','{}',?)
`, request.JobID, request.ItemID, request.NowMS); err != nil {
		return false, arcadeParentCommitStoreError("record cancellation event", err)
	}
	if err := transaction.Commit(); err != nil {
		return false, arcadeParentCommitStoreError("commit cancellation", err)
	}
	return true, nil
}

type arcadeParentCommitArtifacts struct {
	snapshotID string
}

type arcadeParentCommitTarget struct {
	contentKind     string
	targetID        string
	coreID          string
	providerID      string
	runtimeTargetID string
	contentPolicy   contentcapability.Policy
	platformVersion int64
}

func insertArcadeParentCommitArtifacts(
	ctx context.Context,
	transaction dbapi.Tx,
	candidate libraryservice.ArcadeParentCommitCandidate,
	entries []importing.ArchiveEntry,
	files []libraryservice.ArcadeParentSourceFile,
	manifestJSON, manifestDigest string,
	validation libraryservice.ArcadeParentValidation,
	target arcadeParentCommitTarget,
	now int64,
) (arcadeParentCommitArtifacts, error) {
	snapshotUUID, _ := uuid.NewV7()
	artifacts := arcadeParentCommitArtifacts{snapshotID: snapshotUUID.String()}
	_, err := transaction.ExecContext(ctx, `
INSERT INTO import_item_source_snapshots(
  id,import_item_id,source_manifest_json,source_manifest_digest,
  content_kind,created_by,created_at_ms
) VALUES(?,?,?,?,?,'ARCADE_PARENT_ATTACHMENT',?)
`, artifacts.snapshotID, candidate.ItemID, manifestJSON, manifestDigest, target.contentKind, now)
	if err != nil {
		return arcadeParentCommitArtifacts{}, arcadeParentCommitStoreError("insert source snapshot", err)
	}
	if err := insertArcadeParentSnapshotFiles(ctx, transaction, artifacts.snapshotID, files, now); err != nil {
		return arcadeParentCommitArtifacts{}, err
	}
	if err := insertArcadeParentArchiveEntries(ctx, transaction, candidate.FileRecord, entries, now); err != nil {
		return arcadeParentCommitArtifacts{}, err
	}
	if err := saveArcadeParentAnalysis(
		ctx, transaction, candidate, validation, now,
	); err != nil {
		return arcadeParentCommitArtifacts{}, err
	}
	return artifacts, nil
}

func saveArcadeParentAnalysis(
	ctx context.Context,
	transaction dbapi.Tx,
	candidate libraryservice.ArcadeParentCommitCandidate,
	validation libraryservice.ArcadeParentValidation,
	now int64,
) error {
	analysis,
		err := libraryservice.ContentAnalysisJSON(
		validation.Status, validation.CompatibilityCode, validation.DependencySnapshot,
	)
	if err != nil {
		return fmt.Errorf("save parent content analysis: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE import_items SET content_analysis_json=? WHERE id=?`, analysis, candidate.ItemID); err != nil {
		return arcadeParentCommitStoreError("save source content analysis", err)
	}
	for _, file := range validation.Files {
		if file.Role == "BIOS_BUNDLE" || file.Role == "PARENT" {
			continue
		}
		if _, err := recordstore.InsertRows(ctx, transaction, "import_item_runtime_files", `
INSERT INTO import_item_runtime_files(
  import_item_id,role,logical_name,file_record,sort_order,created_at_ms
) VALUES(?,?,?,?,?,?) ON CONFLICT(import_item_id,role,logical_name) DO UPDATE
SET file_record=excluded.file_record,sort_order=excluded.sort_order
`, candidate.ItemID, file.Role, file.LogicalName, file.FileRecord, file.SortOrder, now); err != nil {
			return arcadeParentCommitStoreError("insert validation file", err)
		}
	}
	return nil
}

func loadArcadeParentCommitTarget(
	ctx context.Context,
	transaction dbapi.Tx,
	candidate libraryservice.ArcadeParentCommitCandidate,
) (arcadeParentCommitTarget, error) {
	var target arcadeParentCommitTarget
	var itemState, currentSnapshotID string
	var activeDATID sql.NullString
	err := dbapi.QueryRowContext(
		ctx,
		transaction,
		`
SELECT item.state,draft.effective_source_snapshot_id,draft.target_platform_instance_id,
source_snapshot.content_kind,platform.version,platform.default_core_id,
target.provider_id,target.target_id,
`+contentquery.BindingPolicySQL+`,
(SELECT dat.id FROM dat_versions dat WHERE dat.provider_id=target.provider_id
 AND dat.target_id=target.target_id AND dat.is_active=1)
FROM import_items item
JOIN import_items draft ON draft.id=? AND draft.id=item.id
JOIN import_item_source_snapshots source_snapshot ON source_snapshot.id=draft.effective_source_snapshot_id
JOIN platform_instances platform ON platform.id=draft.target_platform_instance_id
AND platform.enabled=1 AND platform.deleted_at_ms IS NULL
JOIN runtime_target_bindings binding ON binding.core_id=platform.default_core_id
  AND binding.launch_policy<>'DISABLED'
JOIN runtime_binding_platforms platform_binding ON platform_binding.binding_id=binding.binding_id
  AND platform_binding.platform_id=platform.platform_id
JOIN runtime_targets target ON target.provider_id=binding.provider_id
  AND target.target_id=binding.target_id
WHERE item.id=?
`,
		candidate.DraftID,
		candidate.ItemID,
	).Scan(

		&itemState,
		&currentSnapshotID,
		&target.targetID,
		&target.contentKind,

		&target.platformVersion,
		&target.coreID,
		&target.providerID,
		&target.runtimeTargetID,

		contentquery.ScanPolicy(&target.contentPolicy),
		&activeDATID,
	)
	valid := err == nil && itemState == "REVIEW_PENDING" && currentSnapshotID == candidate.BaseSnapshotID &&
		target.providerID == candidate.ProviderID && target.runtimeTargetID == candidate.TargetID &&
		target.contentPolicy.DigestFor(target.contentKind) == candidate.ContentPolicyDigest &&
		activeDATID.Valid && activeDATID.String == candidate.DATID
	if !valid {
		return arcadeParentCommitTarget{}, libraryservice.ErrInvalid
	}
	return target, nil
}

func insertArcadeParentSnapshotFiles(
	ctx context.Context,
	transaction dbapi.Tx,
	snapshotID string,
	files []libraryservice.ArcadeParentSourceFile,
	now int64,
) error {
	for _, file := range files {
		var archiveFileRecord, archiveOrdinal any
		if file.ArchiveFileRecord != nil {
			archiveFileRecord = *file.ArchiveFileRecord
		}
		if file.ArchiveOrdinal != nil {
			archiveOrdinal = *file.ArchiveOrdinal
		}
		if _, err := recordstore.InsertRows(
			ctx,
			transaction,
			"import_item_source_snapshot_files",
			`
INSERT INTO import_item_source_snapshot_files(
  source_snapshot_id,role,logical_name,upload_file_id,file_record,source_archive_file_record,
  source_archive_entry_ordinal,sort_order,created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?)
`,
			snapshotID,
			file.Role,
			file.LogicalName,
			file.UploadFileID,
			file.FileRecord,
			archiveFileRecord,
			archiveOrdinal,
			file.SortOrder,
			now,
		); err != nil {
			return arcadeParentCommitStoreError("insert source snapshot file", err)
		}
	}
	return nil
}

func insertArcadeParentArchiveEntries(
	ctx context.Context,
	transaction dbapi.Tx,
	archiveFileRecord string,
	entries []importing.ArchiveEntry,
	now int64,
) error {
	for _, entry := range entries {
		if _, err := recordstore.InsertRows(
			ctx,
			transaction,
			"archive_entries",
			`
INSERT OR IGNORE INTO archive_entries(
  archive_file_record,ordinal,original_relative_path,normalized_path,ascii_casefold_path,
  archive_format,compression_profile,uncompressed_size_bytes,crc32,md5,sha1,sha256,
  created_at_ms
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
			archiveFileRecord,
			entry.Ordinal,
			entry.OriginalPath,
			entry.NormalizedPath,
			entry.ASCIICasefoldPath,
			entry.ArchiveFormat,
			entry.CompressionProfile,
			entry.Size,
			entry.CRC32,
			entry.MD5,
			entry.SHA1,
			entry.SHA256,
			now,
		); err != nil {
			return arcadeParentCommitStoreError("insert source archive entry", err)
		}
	}
	return nil
}

func requireArcadeParentCommitChange(result sql.Result, err error, action string) error {
	if err != nil {
		return arcadeParentCommitStoreError(action, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return arcadeParentCommitStoreError(action+" result", err)
	}
	if changed != 1 {
		return libraryservice.ErrInvalid
	}
	return nil
}

func arcadeParentCommitStoreError(operation string, err error) error {
	return fmt.Errorf("persistence/libraryimport/arcade parent %s: %w", operation, err)
}

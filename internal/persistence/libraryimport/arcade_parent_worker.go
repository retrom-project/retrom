package libraryimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	application "retrom/internal/service/libraryimport"
)

var errArcadeParentSourceArchiveBlobMissing = errors.New("arcade parent source archive blob is missing")

// ArcadeParentAttachmentWorker owns the short claim transaction and the
// worker read models used while validating an uploaded arcade parent. Archive
// parsing and source-manifest construction remain in the application facade.
type ArcadeParentAttachmentWorker struct{ database *sql.DB }

var _ application.ArcadeParentAttachmentWorkerRepository = (*ArcadeParentAttachmentWorker)(nil)

func NewArcadeParentAttachmentWorker(database *sql.DB) *ArcadeParentAttachmentWorker {
	return &ArcadeParentAttachmentWorker{database: database}
}

func (repository *ArcadeParentAttachmentWorker) Claim(
	ctx context.Context, jobID, workerID string, now int64,
) (application.ArcadeParentAttachmentWorkerClaim, error) {
	return runWorkerClaim(
		ctx, repository.database, jobID, workerID, now, "arcade parent",
		claimArcadeParentAttachmentRecords, readClaimedArcadeParentAttachment,
	)
}

func claimArcadeParentAttachmentRecords(
	ctx context.Context, tx *sql.Tx, jobID, workerID string, now int64,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE jobs SET state='RUNNING',attempt_count=attempt_count+1,worker_id=?,
execution_started_at_ms=COALESCE(execution_started_at_ms,?),execution_deadline_at_ms=?,
leased_until_ms=?,heartbeat_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND kind='REVIEW_ARCADE_PARENT_VALIDATE' AND state='QUEUED' AND available_at_ms<=?
`, workerID, now, now+application.ArcadeParentAttachmentDeadline.Milliseconds(),
		now+application.ArcadeParentAttachmentDeadline.Milliseconds(), now, now, jobID, now)
	if err != nil {
		return fmt.Errorf("claim arcade parent job: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrInvalid
	}
	result, err = tx.ExecContext(ctx, `
UPDATE review_arcade_parent_attachments
SET state='RUNNING',error_code=NULL,finished_at_ms=NULL,version=version+1,updated_at_ms=?
WHERE job_id=? AND state IN ('QUEUED','FAILED_RETRYABLE')
`, now, jobID)
	if err != nil {
		return fmt.Errorf("mark arcade parent attachment running: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return application.ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
SELECT id,scope_type,scope_id,'STARTED','{}',? FROM jobs WHERE id=?
`, now, jobID); err != nil {
		return fmt.Errorf("record arcade parent start event: %w", err)
	}
	return nil
}

func readClaimedArcadeParentAttachment(
	ctx context.Context, tx *sql.Tx, jobID, workerID string,
) (application.ArcadeParentAttachmentWorkerClaim, error) {
	var result application.ArcadeParentAttachmentWorkerClaim
	result.JobID, result.WorkerID = jobID, workerID
	var inputJSON string
	if err := tx.QueryRowContext(ctx, `
SELECT input.input_json,job.execution_started_at_ms
FROM job_input_snapshots input
JOIN jobs job ON job.id=input.job_id AND job.execution_no=input.execution_no
WHERE input.job_id=?
`, jobID).Scan(&inputJSON, &result.ExecutionStartedAtMS); err != nil {
		return application.ArcadeParentAttachmentWorkerClaim{}, fmt.Errorf("read arcade parent input: %w", err)
	}
	if err := json.Unmarshal([]byte(inputJSON), &result.Input); err != nil ||
		!application.ValidArcadeParentAttachmentInput(result.Input) {
		return application.ArcadeParentAttachmentWorkerClaim{}, application.ErrInvalid
	}
	var candidate application.ArcadeParentAttachmentCandidate
	if err := tx.QueryRowContext(ctx, `
SELECT attachment.id,attachment.import_item_id,attachment.review_draft_id,
attachment.base_source_snapshot_id,attachment.dependency_machine,attachment.required_by_machine,
attachment.depth,attachment.provider_id,attachment.target_id,
attachment.dat_version_id,attachment.upload_file_id,
file.upload_session_id,attachment.original_filename,file.blob_id,blob.sha256,blob.size_bytes
FROM review_arcade_parent_attachments attachment
JOIN import_files file ON file.id=attachment.upload_file_id
JOIN blobs blob ON blob.id=file.blob_id
WHERE attachment.job_id=? AND attachment.state='RUNNING'
`, jobID).Scan(
		&candidate.AttachmentID, &candidate.ItemID, &candidate.DraftID, &candidate.BaseSnapshotID,
		&candidate.Machine, &candidate.RequiredBy, &candidate.Depth, &candidate.ProviderID, &candidate.TargetID,
		&candidate.DATID, &candidate.UploadFileID, &candidate.UploadSessionID, &candidate.OriginalName,
		&candidate.BlobID, &candidate.BlobSHA, &candidate.BlobSize,
	); err != nil {
		return application.ArcadeParentAttachmentWorkerClaim{}, fmt.Errorf("read claimed arcade parent attachment: %w", err)
	}
	if candidate.AttachmentID != result.Input.AttachmentID || candidate.ItemID != result.Input.ImportItemID ||
		candidate.DraftID != result.Input.ReviewDraftID || candidate.BaseSnapshotID != result.Input.BaseSourceSnapshotID ||
		candidate.Machine != result.Input.DependencyMachine || candidate.ProviderID != result.Input.ProviderID ||
		candidate.TargetID != result.Input.TargetID || candidate.DATID != result.Input.DATVersionID ||
		candidate.UploadFileID != result.Input.UploadFileID {
		return application.ArcadeParentAttachmentWorkerClaim{}, application.ErrInvalid
	}
	candidate.ContentPolicyDigest = result.Input.ContentPolicyDigest
	result.Candidate = candidate
	return result, nil
}

func (repository *ArcadeParentAttachmentWorker) RootValidation(
	ctx context.Context, candidate application.ArcadeParentAttachmentCandidate,
) (string, error) {
	var raw string
	if err := repository.database.QueryRowContext(ctx, `
SELECT dependency_snapshot_json
FROM import_item_core_validations
WHERE import_item_id=? AND source_snapshot_id=? AND provider_id=? AND target_id=? AND dat_version_id=?
ORDER BY created_at_ms DESC,id DESC LIMIT 1
`, candidate.ItemID, candidate.BaseSnapshotID, candidate.ProviderID, candidate.TargetID,
		candidate.DATID).Scan(&raw); err != nil {
		return "", fmt.Errorf("read arcade parent root validation: %w", err)
	}
	return raw, nil
}

func (repository *ArcadeParentAttachmentWorker) SourceSnapshot(
	ctx context.Context, snapshotID string,
) ([]application.ArcadeParentSourceSnapshotFile, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT file.role,file.logical_name,file.upload_file_id,file.blob_id,blob.sha256,blob.size_bytes,
file.source_archive_blob_id,file.source_archive_entry_ordinal,COALESCE(archive.sha256,'')
FROM import_item_source_snapshot_files file
JOIN blobs blob ON blob.id=file.blob_id
LEFT JOIN blobs archive ON archive.id=file.source_archive_blob_id
WHERE file.source_snapshot_id=?
ORDER BY file.role,file.logical_name
`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read arcade parent source snapshot: %w", err)
	}
	defer func() { cleanup.Error("close arcade parent source snapshot", rows.Close()) }()
	files := make([]application.ArcadeParentSourceSnapshotFile, 0)
	for rows.Next() {
		var file application.ArcadeParentSourceSnapshotFile
		var archiveID sql.NullString
		var archiveOrdinal sql.NullInt64
		if err := rows.Scan(
			&file.Role, &file.LogicalName, &file.UploadFileID, &file.BlobID, &file.BlobSHA, &file.BlobSize,
			&archiveID, &archiveOrdinal, &file.SourceArchiveSHA,
		); err != nil {
			return nil, fmt.Errorf("scan arcade parent source snapshot: %w", err)
		}
		if archiveID.Valid {
			file.SourceArchiveBlobID = archiveID.String
			if archiveOrdinal.Valid {
				ordinal := int(archiveOrdinal.Int64)
				file.SourceArchiveEntryOrdinal = &ordinal
			}
			if file.SourceArchiveSHA == "" {
				return nil, errArcadeParentSourceArchiveBlobMissing
			}
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate arcade parent source snapshot: %w", err)
	}
	return files, nil
}
